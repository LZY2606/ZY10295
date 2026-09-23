package ledger

import (
	"ntp-ledger/internal/ntp"
)

// eraWindow is the maximum plausible offset between two clocks in one
// exchange: half an era (2^31 s, about 68 years).
const eraWindow = int64(1) << 31

// clientEra derives the era of a client-side timestamp from the full
// capture wall time.
func clientEra(unixNs int64) uint32 {
	ntpSec := unixNs/1_000_000_000 + ntp.UnixEpochOffset
	return uint32(uint64(ntpSec) / ntp.EraSpan)
}

// eraCandidates enumerates the plausible era assignments for an
// exchange. When the capture record carries full client wall time, the
// client era is fixed and the server era must place the server transmit
// timestamp within half an era of the client receive time. Otherwise
// every era 0..MaxEra is a candidate for the exchange as a whole.
func eraCandidates(ex *Exchange, opts Options) []EraCandidate {
	if ex.ClientKnown {
		ce := clientEra(ex.T4UnixNs)
		t4 := ex.T4.AtEra(ce)
		var out []EraCandidate
		for se := uint32(0); se <= maxEra(opts); se++ {
			tx := ex.Resp.Tx.AtEra(se)
			d := ntp.Sub(tx, t4)
			if d.Sec > -eraWindow && d.Sec < eraWindow {
				out = append(out, EraCandidate{ClientEra: ce, ServerEra: se})
			}
		}
		return out
	}
	out := make([]EraCandidate, 0, maxEra(opts)+1)
	for e := uint32(0); e <= maxEra(opts); e++ {
		out = append(out, EraCandidate{ClientEra: e, ServerEra: e})
	}
	return out
}

func maxEra(opts Options) uint32 {
	if opts.MaxEra == 0 {
		return 1
	}
	return opts.MaxEra
}

// selectEra picks the era candidate closest to the anchor. Ties break
// toward the lower server era, then the lower client era, so selection
// is deterministic. Without an anchor a unique candidate is selected;
// multiple candidates are all retained and nothing is selected.
func selectEra(ex *Exchange, cands []EraCandidate, opts Options) ([]EraCandidate, EraCandidate, bool) {
	if len(cands) == 0 {
		return cands, EraCandidate{}, false
	}
	if opts.Anchor == nil {
		if len(cands) == 1 {
			return cands, cands[0], true
		}
		return cands, EraCandidate{}, false
	}
	scored := make([]EraCandidate, len(cands))
	copy(scored, cands)
	best := 0
	for i := range scored {
		scored[i].Dist = anchorDist(ex, scored[i], *opts.Anchor)
		if ntp.Cmp(scored[i].Dist, scored[best].Dist) < 0 ||
			(ntp.Cmp(scored[i].Dist, scored[best].Dist) == 0 && lessCand(scored[i], scored[best])) {
			best = i
		}
	}
	return scored, scored[best], true
}

func lessCand(a, b EraCandidate) bool {
	if a.ServerEra != b.ServerEra {
		return a.ServerEra < b.ServerEra
	}
	return a.ClientEra < b.ClientEra
}

// anchorDist measures how far the exchange (at this candidate) sits
// from the trusted anchor, using the client receive timestamp.
func anchorDist(ex *Exchange, c EraCandidate, anchor ntp.Abs) ntp.Fixed {
	return ntp.AbsVal(ntp.Sub(ex.T4.AtEra(c.ClientEra), anchor))
}
