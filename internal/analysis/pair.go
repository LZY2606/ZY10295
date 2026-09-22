package analysis

import (
	"eraledger/internal/ntp"
	"fmt"
)

// parsedRec is a capture record with its decoded packet (or parse error).
type parsedRec struct {
	in   CaptureInput
	pkt  *ntp.Packet
	perr error
}

// reqSlot is an outstanding client request awaiting a server response.
type reqSlot struct {
	rec       parsedRec
	responded bool
	dupTrans  bool // another open request shares the same transmit stamp
}

func (a *Analyzer) pairAll(cap_ *Capture) ([]*Exchange, []Evidence) {
	recs := make([]parsedRec, 0, len(cap_.Records))
	for _, r := range cap_.Records {
		pr := parsedRec{in: r}
		pkt, err := ntp.Parse(r.Packet)
		pr.pkt, pr.perr = pkt, err
		recs = append(recs, pr)
	}

	var evidence []Evidence
	addEv := func(scope, ref, code, detail string) Evidence {
		e := Evidence{ID: len(evidence) + 1, Scope: scope, Ref: ref, Code: code, Detail: detail}
		evidence = append(evidence, e)
		return e
	}

	// Open requests keyed by (client,server) in import order.
	type key struct{ c, s string }
	open := map[key][]*reqSlot{}
	var exchanges []*Exchange
	exSeq := map[string]int{}

	newID := func(peer string) string {
		exSeq[peer]++
		return fmt.Sprintf("%s#%d", peer, exSeq[peer])
	}

	for _, pr := range recs {
		r := pr.in
		k := key{r.Client, r.Server}
		if pr.perr != nil {
			ex := &Exchange{
				ID: newID(r.Client + "|" + r.Server), Client: r.Client, Peer: r.Server,
				RecvSeq: r.Seq, LocalClock: r.LocalClock, RecvNS: r.LocalNS,
				Rejected: true, RejectNote: pr.perr.Error(),
			}
			ex.RejectCode = classifyParseError(pr.perr)
			ex.RejectNote = pr.perr.Error()
			addEv("exchange", ex.ID, ex.RejectCode, pr.perr.Error())
			exchanges = append(exchanges, ex)
			continue
		}
		pkt := pr.pkt
		if pkt.Version < 1 || pkt.Version > 4 {
			ex := &Exchange{
				ID: newID(r.Client + "|" + r.Server), Client: r.Client, Peer: r.Server,
				RecvSeq: r.Seq, LocalClock: r.LocalClock, RecvNS: r.LocalNS,
				Response: pkt, Rejected: true, RejectCode: RejectBadVersion,
				RejectNote: fmt.Sprintf("version %d is not 1..4", pkt.Version),
			}
			fillRaws(ex)
			addEv("exchange", ex.ID, ex.RejectCode, ex.RejectNote)
			exchanges = append(exchanges, ex)
			continue
		}

		switch {
		case pkt.Mode == ntp.ModeClient && r.Dir == "send":
			slots := open[k]
			dup := false
			for _, s := range slots {
				if !s.responded && s.rec.pkt.Transmit.Raw64() == pkt.Transmit.Raw64() {
					dup = true
					s.dupTrans = true
				}
			}
			open[k] = append(slots, &reqSlot{rec: pr, dupTrans: dup})
			if dup {
				addEv("exchange",
					fmt.Sprintf("%s|%s", r.Client, r.Server),
					"duplicate_transmit",
					fmt.Sprintf("seq %d repeats a still-open transmit timestamp %016x",
						r.Seq, pkt.Transmit.Raw64()))
			}
		case pkt.Mode == ntp.ModeServer && r.Dir == "recv":
			var match *reqSlot
			matchIdx := -1
			for i, s := range open[k] {
				if s.responded {
					continue
				}
				if pkt.Origin.Raw64() == 0 || pkt.Origin.Raw64() == s.rec.pkt.Transmit.Raw64() {
					match = s
					matchIdx = i
					break
				}
			}
			ex := &Exchange{
				ID: newID(r.Client + "|" + r.Server), Client: r.Client, Peer: r.Server,
				RecvSeq: r.Seq, LocalClock: r.LocalClock, RecvNS: r.LocalNS,
				Response: pkt,
			}
			fillRaws(ex)
			if match == nil {
				code := RejectUnsolicited
				if pkt.Origin.Raw64() != 0 {
					code = RejectOriginMismatch
				}
				ex.Rejected = true
				ex.RejectCode = code
				ex.RejectNote = fmt.Sprintf("response origin %016x matches no open request",
					pkt.Origin.Raw64())
				addEv("exchange", ex.ID, code, ex.RejectNote)
				exchanges = append(exchanges, ex)
				continue
			}
			ex.Request = match.rec.pkt
			ex.SendSeq = match.rec.in.Seq
			ex.SendNS = match.rec.in.LocalNS
			ex.DuplicateTransmit = match.dupTrans
			// Is this a duplicate response (same T3 as the already answered
			// duplicate-transmit sibling)?
			for _, es := range exchanges {
				if es.Client == r.Client && es.Peer == r.Server && !es.Rejected &&
					es.Request != nil &&
					es.Request.Transmit.Raw64() == match.rec.pkt.Transmit.Raw64() &&
					es.Response != nil && es.Response.Transmit.Raw64() == pkt.Transmit.Raw64() {
					ex.DuplicateResponse = true
				}
			}
			if ex.DuplicateResponse {
				ex.Rejected = true
				ex.RejectCode = RejectDuplicateResp
				ex.RejectNote = "repeated response with same transmit timestamp as a prior one"
				addEv("exchange", ex.ID, RejectDuplicateResp, ex.RejectNote)
				exchanges = append(exchanges, ex)
				match.responded = true
				open[k][matchIdx] = match
				continue
			}
			match.responded = true
			open[k][matchIdx] = match
			if ex.DuplicateTransmit {
				addEv("exchange", ex.ID, "duplicate_transmit",
					"request shared its transmit timestamp with another open request")
			}
			exchanges = append(exchanges, ex)
		default:
			ex := &Exchange{
				ID: newID(r.Client + "|" + r.Server), Client: r.Client, Peer: r.Server,
				RecvSeq: r.Seq, LocalClock: r.LocalClock, RecvNS: r.LocalNS,
				Response: pkt, Rejected: true, RejectCode: "unexpected_packet",
				RejectNote: fmt.Sprintf("mode %d/dir %s is not a client send or server recv", pkt.Mode, r.Dir),
			}
			fillRaws(ex)
			addEv("exchange", ex.ID, ex.RejectCode, ex.RejectNote)
			exchanges = append(exchanges, ex)
		}
	}
	return exchanges, evidence
}

func fillRaws(ex *Exchange) {
	if ex.Response != nil {
		ex.RawOrigin = ex.Response.Origin.Raw64()
		ex.RawReceive = ex.Response.Receive.Raw64()
		ex.RawTransmit = ex.Response.Transmit.Raw64()
		ex.RawReference = ex.Response.Reference.Raw64()
		ex.LI = ex.Response.LI
		ex.Stratum = ex.Response.Stratum
		ex.Poll = ex.Response.Poll
		ex.ReferenceID = ex.Response.ReferenceID
	}
	if ex.Request != nil {
		ex.RawReqTransmit = ex.Request.Transmit.Raw64()
	}
}

func classifyParseError(err error) string {
	switch err.Error() {
	case "ntp: packet shorter than 48 bytes":
		return RejectShortPacket
	case "ntp: negative 16.16 fixed-point field":
		return RejectNegativeFP
	case "ntp: non-negative precision exponent is invalid":
		return RejectBadPrecision
	default:
		return RejectShortPacket
	}
}
