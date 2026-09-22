package analysis

import (
	"eraledger/internal/ntp"
	"fmt"
	"math/big"
)

// PHINum is the RFC 5905 peer-jitter accrual rate 15 ppm = 15/1e6.
const PHINum = int64(15)
const PHIDen = int64(1_000_000)

// Analyzer is a stateless configuration holder; every Analyze call is
// deterministic for its inputs.
type Analyzer struct {
	// ClientPrecision is the local client precision exponent (default -24).
	ClientPrecision int8
}

// leapBoundsOf extracts the imported leap table from the capture via closure.

// DefaultSettings returns the deterministic default settings:
// no anchor, raw leap policy, 50 ms local-clock tolerance, 8 filter slots.
func DefaultSettings() Settings {
	return Settings{
		LeapPolicy:  LeapRaw,
		LocalTolNS:  50_000_000,
		MaxOffsetNS: 86_400_000_000_000,
		MaxFilter:   8,
	}
}

// Analyze runs a full offline analysis pass.
func (a *Analyzer) Analyze(cap_ *Capture, s Settings) *Result {
	if s.LeapPolicy == "" {
		s.LeapPolicy = LeapRaw
	}
	if s.LocalTolNS == 0 {
		s.LocalTolNS = 50_000_000
	}
	if s.MaxFilter == 0 {
		s.MaxFilter = 8
	}
	if s.MaxOffsetNS == 0 {
		s.MaxOffsetNS = 86_400_000_000_000
	}
	if a.ClientPrecision == 0 {
		a.ClientPrecision = -24
	}

	leapTable = map[uint32]uint8{}
	for _, l := range cap_.Leaps {
		leapTable[l.NTPSeconds] = l.Kind
	}
	exchanges, evidence := a.pairAll(cap_)
	srcTrusted := map[string]bool{}
	for _, src := range cap_.Sources {
		srcTrusted[src.ID] = src.Trusted
	}

	clientPrec, err := ntp.PrecisionTick64(a.ClientPrecision)
	if err != nil {
		clientPrec = 0
	}

	// Effective anchor: explicit settings win; otherwise a trusted clock
	// source with a paired observation gives an implicit anchor.
	for _, ex := range exchanges {
		if !s.HasAnchor && ex.Request != nil && srcTrusted[ex.LocalClock] && ex.RecvNS != 0 {
			s.HasAnchor = true
			s.AnchorNS = ex.RecvNS
			s.AnchorLocalNS = ex.RecvNS
			s.AnchorClock = ex.LocalClock
		}
	}

	var baseEra uint32
	if s.HasAnchor {
		baseEra = ntp.FromUnixNano(s.AnchorNS).Era
	}

	for _, ex := range exchanges {
		if ex.Rejected || ex.Request == nil || ex.Response == nil {
			continue
		}
		a.classifyResponse(ex)
		if ex.Rejected {
			evidence = append(evidence, Evidence{
				ID: len(evidence) + 1, Scope: "exchange", Ref: ex.ID,
				Code: ex.RejectCode, Detail: ex.RejectNote,
			})
			continue
		}
		a.enumerateCandidates(ex, baseEra, s)
		if len(ex.Candidates) == 0 {
			ex.Rejected = true
			ex.RejectCode = RejectEraAmbiguous
			ex.RejectNote = "no era assignment satisfies ordering and the local clock"
			evidence = append(evidence, Evidence{
				ID: len(evidence) + 1, Scope: "exchange", Ref: ex.ID,
				Code: ex.RejectCode, Detail: ex.RejectNote,
			})
			continue
		}
		a.evaluateChosen(ex, s, clientPrec)
		if !ex.Rejected && ex.RecvNS-ex.SendNS > 100_000_000_000 {
			ex.Late = true
			evidence = append(evidence, Evidence{
				ID: len(evidence) + 1, Scope: "exchange", Ref: ex.ID,
				Code: "late_response",
				Detail: fmt.Sprintf("response arrived %d s after the request",
					(ex.RecvNS-ex.SendNS)/1_000_000_000),
			})
		}
	}

	peers := a.buildPeers(exchanges, s, &evidence)
	selected := rankPeers(peers)

	return &Result{
		Settings:  s,
		Exchanges: exchanges,
		Peers:     peers,
		Evidence:  evidence,
		Selected:  selected,
	}
}

func (a *Analyzer) classifyResponse(ex *Exchange) {
	p := ex.Response
	if _, code := ntp.PrecisionTick64(p.Precision); code != nil {
		ex.Rejected = true
		ex.RejectCode = RejectBadPrecision
		ex.RejectNote = fmt.Sprintf("precision exponent %d is non-negative", p.Precision)
		return
	}
	if code, ok := p.KissCode(); ok {
		ex.Rejected = true
		ex.RejectCode = RejectKissODeath
		ex.RejectNote = fmt.Sprintf("kiss-o'-death packet, code %q", code)
		return
	}
	if p.Stratum == 0 {
		ex.Rejected = true
		ex.RejectCode = RejectKissODeath
		ex.RejectNote = "stratum 0 response without a valid ASCII kiss code"
		return
	}
	if p.LI == ntp.LIAlarm {
		ex.Rejected = true
		ex.RejectCode = RejectLI3
		ex.RejectNote = "LI=3 alarm: server clock is not synchronized"
		return
	}
}

func (a *Analyzer) evaluateChosen(ex *Exchange, s Settings, clientPrec uint64) {
	p := ex.Response
	best := ex.Candidates[ex.ChosenCandidate]
	er := best.Eras

	x1 := ntp.ExtendStamp(ex.Request.Transmit, er[0])
	x2 := ntp.ExtendStamp(p.Receive, er[1])
	x3 := ntp.ExtendStamp(p.Transmit, er[2])
	t4 := ntp.FromUnixNano(ex.RecvNS)
	x4 := ntp.ExtendStamp(t4.Stamp, er[3])

	// Leap policy applies to the *server* timestamps only; marking LI never
	// adds one second to the client timestamps.
	inside, adjusted := applyLeapPolicy(x2, x3, p.LI, s.LeapPolicy, leapTable)
	if inside {
		ex.Rejected = true
		ex.RejectCode = RejectLeapInsideSecond
		ex.RejectNote = "avoid policy: receive/transmit straddle the inserted leap second"
		return
	}
	ex.LeapInside = false
	ex.LeapAdjusted = adjusted
	if adjusted {
		x2 = ntp.AddTick32(x2, big.NewInt(-int64(1<<32)))
		x3 = ntp.AddTick32(x3, big.NewInt(-int64(1<<32)))
	}

	delay := new(big.Int).Add(ntp.Sub(x4, x1), ntp.Sub(x2, x3))
	offset := new(big.Int).Add(ntp.Sub(x2, x1), ntp.Sub(x3, x4))
	offset.Rsh(offset, 1)
	if delay.Sign() < 0 {
		ex.Rejected = true
		ex.RejectCode = RejectNegativeDelay
		ex.RejectNote = "computed round-trip delay is negative"
		return
	}
	ex.DelayTick32 = delay.Int64()
	ex.OffsetTick32 = offset.Int64()
	ex.DelayNS = ntp.NsFromTick32(ex.DelayTick32)
	ex.OffsetNS = ntp.NsFromTick32(ex.OffsetTick32)

	serverPrec, _ := ntp.PrecisionTick64(p.Precision)
	age := ntp.Ticks32OfDuration(ex.RecvNS - ex.SendNS)
	ex.AgeTick32 = age
	ex.ServerPrecTick64 = serverPrec
	ex.ClientPrecTick64 = clientPrec
	ex.RootDelayTick64 = p.RootDelay.Tick64()
	ex.RootDispTick64 = p.RootDisp.Tick64()

	// phi * age, kept exact: phi = 15/1e6 s, age in tick32.
	ageBig := big.NewInt(age)
	ageTick64 := new(big.Int).Lsh(ageBig, 32)
	phiN := new(big.Int).Mul(ageTick64, big.NewInt(PHINum))
	phiD := big.NewInt(PHIDen)
	phi := ratFrom(phiN, phiD)
	ex.PhiTick64Num = phiN.String()
	ex.PhiTick64Den = PHIDenStr()

	// rho = root_disp/2 + root_delay/2 + serverprec + clientprec + phi*age
	rho := newRat(int64(p.RootDisp.Tick64() / 2))
	rho = rho.Add(newRat(int64(p.RootDelay.Tick64() / 2)))
	rho = rho.Add(newRat(int64(serverPrec)))
	rho = rho.Add(newRat(int64(clientPrec)))
	rho = rho.Add(phi)
	rhoF := rho.Floor()
	ex.RhoTick64 = rhoF.Uint64()

	lambda := new(big.Int).Add(
		big.NewInt(int64(ex.RootDelayTick64/2)),
		big.NewInt(int64(ex.RootDispTick64/2)))
	lambda.Add(lambda, rhoF)
	ex.RootDistanceTick64 = lambda.Uint64()

	ex.ReferenceRaw = p.Reference.Raw64()
	ex.ReferenceEra = er[3] // reference shares the server era family
}

func PHIDenStr() string { return fmt.Sprintf("%d", PHIDen) }

var leapTable = map[uint32]uint8{}

func applyLeapPolicy(x2, x3 ntp.ExtendedStamp, li uint8, policy string,
	leaps map[uint32]uint8) (inside bool, adjusted bool) {
	if policy == LeapRaw {
		return false, false
	}
	for bound, kind := range leaps {
		if kind != ntp.LILastMin61 {
			continue
		}
		lo := ntp.ExtendedStamp{Era: x3.Era, Stamp: ntp.Stamp{Sec: bound, Frac: 0}}
		hi := ntp.ExtendStamp(lo.Stamp, lo.Era)
		hi = ntp.AddTick32(hi, big.NewInt(1<<32))
		if li == ntp.LILastMin61 && (x3.Cmp(lo) >= 0 && x3.Cmp(hi) < 0) {
			if policy == LeapAvoid {
				return true, false
			}
			return false, true
		}
	}
	return false, false
}
