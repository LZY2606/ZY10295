// Package ledger evaluates imported NTP exchanges into an offline era
// ledger: per-exchange delay/offset/dispersion/root-distance, era
// candidates for the 32-bit seconds field, and per-peer filter state.
//
// Era resolution never consults the wall clock of the machine running
// the evaluation. Eras are derived either from the capture record (when
// the client clock is fully known) or from a user-supplied trusted
// anchor. Without an anchor, all plausible era candidates are retained.
package ledger

import (
	"fmt"
	"sort"

	"ntp-ledger/internal/ntp"
)

// phi is the dispersion growth rate: 15e-6 s/s as in RFC 5905.
const phiNum, phiDen = 15, 1_000_000

// Exchange is one imported request/response pair.
type Exchange struct {
	ID           int64
	PeerID       int64
	Seq          int // request sequence number
	RecvOrder    int // arrival order of the response at the capture point
	CaptureClock string
	ClientKnown  bool // capture includes full client wall time
	T1UnixNs     int64
	T4UnixNs     int64
	T1           ntp.Timestamp // client request transmit (wire copy)
	T4           ntp.Timestamp // client response receive (wire copy)
	Resp         ntp.Packet    // server response
	Raw          []byte        // raw 48-byte response packet
}

// EraCandidate is one plausible era assignment for an exchange.
type EraCandidate struct {
	ClientEra uint32
	ServerEra uint32
	Dist      ntp.Fixed // distance to the anchor, when an anchor is set
}

// FilterVerdict records the outcome of one filter for one exchange.
type FilterVerdict struct {
	Name   string
	Reject bool
	Detail string
}

// Eval is the full evaluation of one exchange.
type Eval struct {
	Ex         *Exchange
	Delay      ntp.Fixed
	Offset     ntp.Fixed
	Dispersion ntp.Fixed
	RootDist   ntp.Fixed
	EraCands   []EraCandidate
	Selected   *EraCandidate // nil when ambiguous (no anchor, many candidates)
	Verdicts   []FilterVerdict
	Accepted   bool
	Reason     string // primary rejection reason, empty when accepted
}

// LeapPolicy decides how announced leap seconds (LI=1/2) affect
// selection. No policy ever shifts the timestamps themselves.
type LeapPolicy int

const (
	// LeapStrict rejects samples taken while a leap second is announced.
	LeapStrict LeapPolicy = iota
	// LeapPermissive accepts but annotates leap-announced samples.
	LeapPermissive
	// LeapIgnore ignores the leap indicator entirely (LI=3 still rejects).
	LeapIgnore
)

func (p LeapPolicy) String() string {
	switch p {
	case LeapStrict:
		return "strict"
	case LeapPermissive:
		return "permissive"
	case LeapIgnore:
		return "ignore"
	}
	return "unknown"
}

// ParseLeapPolicy parses a policy name.
func ParseLeapPolicy(s string) (LeapPolicy, error) {
	switch s {
	case "strict":
		return LeapStrict, nil
	case "permissive":
		return LeapPermissive, nil
	case "ignore":
		return LeapIgnore, nil
	}
	return LeapStrict, fmt.Errorf("unknown leap policy %q", s)
}

// Options control one evaluation pass.
type Options struct {
	Anchor *ntp.Abs // trusted time anchor; nil keeps ambiguous candidates
	// MaxEra bounds the era search for captures without a known client
	// clock. Eras 0..MaxEra are considered.
	MaxEra uint32
	Leap   LeapPolicy
}

// PeerInput groups the exchanges of one peer.
type PeerInput struct {
	ID        int64
	Name      string
	Addr      string
	Exchanges []*Exchange
}

// Sample is one accepted exchange in the peer filter.
type Sample struct {
	Seq      int
	Offset   ntp.Fixed
	Delay    ntp.Fixed
	RootDist ntp.Fixed
	LeapFlag bool // a leap second was announced when the sample was taken
}

// Peer is the evaluated per-peer ledger state.
type Peer struct {
	ID        int64
	Name      string
	Addr      string
	Reach     uint8 // shift register, one bit per exchange in recv order
	Samples   []Sample
	Stratum   uint8
	RefID     string
	Precision int8
	EraNote   string
	Evals     []*Eval
}

// ReachOctal renders the reach register in octal, as ntpq does.
func (p *Peer) ReachOctal() string {
	return fmt.Sprintf("%03o", p.Reach)
}

// Evaluate runs the full ledger over all peers. The result is a pure
// function of the inputs and opts: same input, same output.
func Evaluate(inputs []PeerInput, opts Options) []*Peer {
	peers := make([]*Peer, 0, len(inputs))
	for _, in := range inputs {
		p := &Peer{ID: in.ID, Name: in.Name, Addr: in.Addr}
		st := &peerFilter{seenTx: map[ntp.Timestamp]bool{}}
		exs := append([]*Exchange(nil), in.Exchanges...)
		sort.SliceStable(exs, func(i, j int) bool { return exs[i].RecvOrder < exs[j].RecvOrder })
		for _, ex := range exs {
			ev := evalExchange(ex, opts, st)
			p.Evals = append(p.Evals, ev)
			if ev.Accepted {
				p.Reach = p.Reach<<1 | 1
				p.Samples = append(p.Samples, Sample{
					Seq:      ex.Seq,
					Offset:   ev.Offset,
					Delay:    ev.Delay,
					RootDist: ev.RootDist,
					LeapFlag: ex.Resp.LI == ntp.LILast61 || ex.Resp.LI == ntp.LILast59,
				})
				if len(p.Samples) > 8 {
					p.Samples = p.Samples[len(p.Samples)-8:]
				}
				p.Stratum = ex.Resp.Stratum
				p.RefID = ex.Resp.RefIDString()
				p.Precision = ex.Resp.Precision
			} else {
				p.Reach = p.Reach << 1
			}
		}
		p.EraNote = eraNote(p)
		peers = append(peers, p)
	}
	return peers
}

func eraNote(p *Peer) string {
	ambiguous, resolved := 0, 0
	eras := map[[2]uint32]bool{}
	for _, ev := range p.Evals {
		if ev.Selected != nil {
			resolved++
			eras[[2]uint32{ev.Selected.ClientEra, ev.Selected.ServerEra}] = true
		} else if len(ev.EraCands) > 1 {
			ambiguous++
		}
	}
	switch {
	case ambiguous > 0:
		return fmt.Sprintf("%d resolved, %d ambiguous (%d candidates kept)", resolved, ambiguous, len(p.Evals))
	case len(eras) > 1:
		return fmt.Sprintf("resolved across %d era assignments", len(eras))
	default:
		return "resolved"
	}
}

// evalExchange computes the era-independent metrics first, then resolves
// era candidates, then runs the filter pipeline in fixed order.
func evalExchange(ex *Exchange, opts Options, st *peerFilter) *Eval {
	ev := &Eval{Ex: ex}

	// Era-independent fixed-point metrics. UnwrapDelta corrects a wrap
	// of the 32-bit seconds field between the two timestamps of a pair.
	clientRT := ntp.UnwrapDelta(ex.T4, ex.T1)
	serverProc := ntp.UnwrapDelta(ex.Resp.Tx, ex.Resp.Rx)
	ev.Delay = ntp.SubFixed(clientRT, serverProc)
	ev.Offset = ntp.Half(ntp.Add(
		ntp.UnwrapDelta(ex.Resp.Rx, ex.T1),
		ntp.UnwrapDelta(ex.Resp.Tx, ex.T4),
	))
	// Dispersion grows while the sample ages in flight; root distance
	// follows RFC 5905: lambda = (delay + rootDelay)/2 + epsilon.
	flight := clientRT
	if flight.Sign() < 0 {
		flight = ntp.Fixed{}
	}
	ev.Dispersion = ntp.Add(ex.Resp.RootDisp.ToFixed(), ntp.Scale(flight, phiNum, phiDen))
	ev.RootDist = ntp.Add(
		ntp.Half(ntp.Add(ev.Delay, ex.Resp.RootDelay.ToFixed())),
		ev.Dispersion,
	)

	ev.EraCands = eraCandidates(ex, opts)
	if scored, sel, ok := selectEra(ex, ev.EraCands, opts); ok {
		ev.EraCands = scored
		s := sel
		ev.Selected = &s
	}

	ev.Verdicts = runFilters(ex, ev, opts, st)
	ev.Accepted = true
	for _, v := range ev.Verdicts {
		if v.Reject {
			ev.Accepted = false
			if ev.Reason == "" {
				ev.Reason = v.Name
			}
		}
	}
	return ev
}
