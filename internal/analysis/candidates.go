package analysis

import (
	"eraledger/internal/ntp"
	"fmt"
	"math/big"
	"sort"
)

type candEval struct {
	eras   [4]uint32
	delay  *big.Int
	offset *big.Int
	cost   int
}

// enumerateCandidates builds every era assignment (eras for t1..t4)
// satisfying the offline NTP sanity constraints:
//
//   - t2 <= t3 (server processing order),
//   - t1 <= t2 and t3 <= t4 (server stamps inside the request window),
//   - non-negative round-trip delay bounded by the local elapsed time,
//   - local elapsed time matching t4-t1 within LocalTolNS.
//
// Each of the four stamps gets its own era shift (-1..+1 around the era
// implied by the trusted anchor); a client send before the rollover and
// its receive after the rollover therefore resolve to different eras.
// Without an anchor every absolute era is retained, since only relative
// shifts are knowable offline.
func (a *Analyzer) enumerateCandidates(ex *Exchange, baseEra uint32, s Settings) {
	t1raw := ex.Request.Transmit
	t2raw := ex.Response.Receive
	t3raw := ex.Response.Transmit
	t4raw := ntp.FromUnixNano(ex.RecvNS)

	localDelta32 := big.NewInt(ntp.Ticks32OfDuration(ex.RecvNS - ex.SendNS))
	tol32 := big.NewInt(ntp.Ticks32OfDuration(s.LocalTolNS))
	hi := new(big.Int).Add(localDelta32, tol32)
	lo := new(big.Int).Sub(localDelta32, tol32)
	maxOff32 := big.NewInt(ntp.Ticks32OfDuration(s.MaxOffsetNS))

	serverShifts := []int{-1, 0, 1}
	var s1Sh, s4Sh []int
	clientEra := baseEra
	if !s.HasAnchor {
		clientEra = 0
		s1Sh = []int{0}
		s4Sh = []int{0}
	} else {
		// The trusted local clock fixes the era of every local observation
		// individually: t1 is stamped at send time, t4 at receive time.
		e1f := ntp.FromUnixNano(ex.SendNS)
		e4f := ntp.FromUnixNano(ex.RecvNS)
		s1Sh = []int{int(e1f.Era) - int(baseEra)}
		s4Sh = []int{int(e4f.Era) - int(baseEra)}
	}
	var evals []candEval
	for _, s1 := range s1Sh {
		for _, s2 := range serverShifts {
			for _, s3 := range serverShifts {
				for _, s4 := range s4Sh {
					era := func(sh int) (uint32, bool) {
						v := int64(clientEra) + int64(sh)
						if v < 0 {
							return 0, false
						}
						return uint32(v), true
					}
					e1, ok1 := era(s1)
					e2, ok2 := era(s2)
					e3, ok3 := era(s3)
					e4, ok4 := era(s4)
					if !(ok1 && ok2 && ok3 && ok4) {
						continue
					}
					x1 := ntp.ExtendStamp(t1raw, e1)
					x2 := ntp.ExtendStamp(t2raw, e2)
					x3 := ntp.ExtendStamp(t3raw, e3)
					x4 := ntp.ExtendStamp(t4raw.Stamp, e4)
					if x2.Cmp(x3) > 0 || x1.Cmp(x2) > 0 || x3.Cmp(x4) > 0 {
					}
					delay := new(big.Int).Add(ntp.Sub(x4, x1), ntp.Sub(x2, x3))
					// The local clock measures the request round trip. A
					// negative packet delay is retained here so the fixed-
					// point stage can reject it with dedicated evidence.
					ld := ntp.Sub(x4, x1)
					if ld.Cmp(lo) < 0 || ld.Cmp(hi) > 0 {
						continue
					}
					if delay.Sign() >= 0 && delay.Cmp(hi) > 0 {
						continue
					}
					offset := new(big.Int).Add(ntp.Sub(x2, x1), ntp.Sub(x3, x4))
					offset.Rsh(offset, 1)
					evals = append(evals, candEval{
						eras:   [4]uint32{e1, e2, e3, e4},
						delay:  delay,
						offset: offset,
						cost:   absi(s1) + absi(s2) + absi(s3) + absi(s4),
					})
				}
			}
		}
	}
	sort.Slice(evals, func(i, j int) bool {
		for k := 0; k < 4; k++ {
			if evals[i].eras[k] != evals[j].eras[k] {
				return evals[i].eras[k] < evals[j].eras[k]
			}
		}
		return false
	})

	var validIdx []int
	for i, ev := range evals {
		c := EraCandidate{
			Eras:         ev.eras,
			BaseEra:      baseEra,
			ShiftCost:    ev.cost,
			DelayTick32:  ev.delay.String(),
			OffsetTick32: ev.offset.String(),
			Valid:        true,
		}
		if !s.HasAnchor {
			c.BaseEra = 0
			c.Note = "no trusted anchor: relative eras only, absolute era unknown"
		} else if maxOff32.Sign() > 0 {
			absOff := new(big.Int).Abs(ev.offset)
			if absOff.Cmp(maxOff32) > 0 {
				c.Valid = false
				c.Reason = fmt.Sprintf(
					"offset %d tick32 exceeds anchor plausibility bound %d tick32",
					absOff, maxOff32)
			}
		}
		ex.Candidates = append(ex.Candidates, c)
		if c.Valid {
			validIdx = append(validIdx, i)
		}
	}
	if len(validIdx) == 0 {
		ex.Candidates = nil
		return
	}
	best := validIdx[0]
	for _, idx := range validIdx[1:] {
		if evals[idx].cost < evals[best].cost {
			best = idx
		}
	}
	ex.ChosenCandidate = best
	ex.Candidates[best].Chosen = true
	if len(validIdx) > 1 {
		ex.EraNote = fmt.Sprintf("%d plausible era assignments; minimum-shift candidate chosen",
			len(validIdx))
	} else {
		ex.EraNote = "unique plausible era assignment"
	}
}

func absi(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
