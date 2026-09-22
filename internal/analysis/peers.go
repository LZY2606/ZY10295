package analysis

import (
	"eraledger/internal/ntp"
	"fmt"
	"math/big"
	"sort"
)

func (a *Analyzer) buildPeers(exchanges []*Exchange, s Settings, evidence *[]Evidence) []*PeerState {
	byPeer := map[string]*PeerState{}
	var order []string
	get := func(cl, sv string) *PeerState {
		id := cl + "|" + sv
		p, ok := byPeer[id]
		if !ok {
			p = &PeerState{ID: id, Client: cl, Server: sv}
			byPeer[id] = p
			order = append(order, id)
		}
		return p
	}

	type req struct {
		ex *Exchange
		ok bool
	}
	events := map[string][]req{}

	for _, ex := range exchanges {
		p := get(ex.Client, ex.Peer)
		p.ExchangeIDs = append(p.ExchangeIDs, ex.ID)
		if ex.Request != nil && ex.SendSeq > 0 {
			events[p.ID] = append(events[p.ID], req{ex: ex, ok: !ex.Rejected})
		}
		if ex.DuplicateTransmit && !ex.Rejected {
			*evidence = append(*evidence, Evidence{
				ID: len(*evidence) + 1, Scope: "peer", Ref: p.ID,
				Code:   "duplicate_transmit",
				Detail: fmt.Sprintf("%s reused an open transmit timestamp", ex.ID),
			})
		}
		if ex.Late {
			*evidence = append(*evidence, Evidence{
				ID: len(*evidence) + 1, Scope: "peer", Ref: p.ID,
				Code: "late_response",
				Detail: ex.ID + " arrived " + fmt.Sprintf("%d", (ex.RecvNS-ex.SendNS)/1_000_000_000) +
					" s after the request",
			})
		}
	}

	for _, id := range order {
		p := byPeer[id]
		evs := events[id]
		sort.SliceStable(evs, func(i, j int) bool {
			if evs[i].ex.SendSeq != evs[j].ex.SendSeq {
				return evs[i].ex.SendSeq < evs[j].ex.SendSeq
			}
			return evs[i].ex.RecvSeq < evs[j].ex.RecvSeq
		})
		seenReq := map[int]bool{}
		for _, e := range evs {
			if seenReq[e.ex.SendSeq] {
				continue
			}
			seenReq[e.ex.SendSeq] = true
			p.Reach <<= 1
			if e.ok {
				p.Reach |= 1
			}
		}
		p.ReachBinary = fmt.Sprintf("%08b", p.Reach)
		a.fillFilter(p, exchanges, s, evidence)
		a.coherence(p, exchanges, evidence)
		collectEras(p, exchanges)
	}

	peers := make([]*PeerState, 0, len(order))
	for _, id := range order {
		peers = append(peers, byPeer[id])
	}
	return peers
}

func (a *Analyzer) fillFilter(p *PeerState, exchanges []*Exchange, s Settings, evidence *[]Evidence) {
	var valid []*Exchange
	for _, ex := range exchanges {
		if ex.Client == p.Client || ex.Peer != p.Server {
			// note: also require client equality below
		}
		if ex.Client == p.Client && ex.Peer == p.Server && !ex.Rejected && ex.Request != nil {
			valid = append(valid, ex)
		}
	}
	sort.SliceStable(valid, func(i, j int) bool { return valid[i].RecvSeq < valid[j].RecvSeq })

	seenT3 := map[uint64]string{}
	type slot struct {
		ex  *Exchange
		rho Rat64
	}
	var slots []slot
	for _, ex := range valid {
		if prior, dup := seenT3[ex.RawTransmit]; dup {
			*evidence = append(*evidence, Evidence{
				ID: len(*evidence) + 1, Scope: "peer", Ref: p.ID,
				Code: RejectDuplicateResp,
				Detail: fmt.Sprintf("%s repeats transmit %016x first seen at %s",
					ex.ID, ex.RawTransmit, prior),
			})
			continue
		}
		seenT3[ex.RawTransmit] = ex.ID
		slots = append(slots, slot{ex: ex, rho: rhoOf(ex)})
	}
	ranked := make([]int, len(slots))
	for i := range slots {
		ranked[i] = i
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := slots[ranked[i]], slots[ranked[j]]
		if a.ex.RootDistanceTick64 != b.ex.RootDistanceTick64 {
			return a.ex.RootDistanceTick64 < b.ex.RootDistanceTick64
		}
		if c := a.rho.Cmp(b.rho); c != 0 {
			return c < 0
		}
		return a.ex.RawTransmit > b.ex.RawTransmit
	})
	for rank, idx := range ranked {
		keep := rank < s.MaxFilter
		fs := FilterSample{
			ExchangeID:   slots[idx].ex.ID,
			T3Raw:        slots[idx].ex.RawTransmit,
			RootDistance: slots[idx].ex.RootDistanceTick64,
			Rho:          slots[idx].rho.N.String() + "/" + slots[idx].rho.D.String(),
			OffsetTick32: slots[idx].ex.OffsetTick32,
			RecvNS:       slots[idx].ex.RecvNS,
			Kept:         keep,
			Rank:         rank + 1,
		}
		if !keep {
			fs.Dropped = true
			fs.DropReason = "filter window full: higher root distance"
		}
		p.Filter = append(p.Filter, fs)
		if rank == 0 {
			p.BestExchange = slots[idx].ex.ID
			p.BestDistance = slots[idx].ex.RootDistanceTick64
			p.BestOffset = slots[idx].ex.OffsetTick32
		}
	}
	if len(valid) > 0 {
		bestEx := valid[len(valid)-1]
		for _, ex := range valid {
			if ex.ID == p.BestExchange {
				bestEx = ex
			}
		}
		p.Stratum = bestEx.Stratum
		p.ReferenceID = bestEx.ReferenceID
	}
}

func rhoOf(ex *Exchange) Rat64 {
	n, _ := new(big.Int).SetString(ex.PhiTick64Num, 10)
	d, _ := new(big.Int).SetString(ex.PhiTick64Den, 10)
	r := ratFrom(n, d)
	r = r.Add(newRat(int64(ex.RootDispTick64 / 2)))
	r = r.Add(newRat(int64(ex.RootDelayTick64 / 2)))
	r = r.Add(newRat(int64(ex.ServerPrecTick64)))
	r = r.Add(newRat(int64(ex.ClientPrecTick64)))
	return r
}

// refInstant resolves a server reference timestamp into the same era as
// the exchange's chosen server receive stamp.
func refInstant(ex *Exchange) (ntp.ExtendedStamp, bool) {
	if ex.Rejected || ex.Request == nil || ex.RawReference == 0 ||
		ex.ChosenCandidate >= len(ex.Candidates) {
		return ntp.ExtendedStamp{}, false
	}
	era := ex.Candidates[ex.ChosenCandidate].Eras[0]
	return ntp.ExtendStamp(ntp.StampFromRaw64(ex.RawReference), era), true
}

func (a *Analyzer) coherence(p *PeerState, exchanges []*Exchange, evidence *[]Evidence) {
	var valid []*Exchange
	for _, ex := range exchanges {
		if ex.Client == p.Client && ex.Peer == p.Server && !ex.Rejected && ex.Request != nil {
			valid = append(valid, ex)
		}
	}
	sort.SliceStable(valid, func(i, j int) bool { return valid[i].RecvSeq < valid[j].RecvSeq })

	var lastRef ntp.ExtendedStamp
	lastRefSet := false
	lastStratum := uint8(0)
	refIDs := map[uint32]int{}
	for _, ex := range valid {
		refIDs[ex.ReferenceID]++
		if cur, ok := refInstant(ex); ok && lastRefSet && cur.Cmp(lastRef) < 0 {
			p.Incoherent = true
			note := fmt.Sprintf("%s: reference timestamp regressed %016x -> %016x",
				ex.ID, lastRef.Raw64(), cur.Raw64())
			p.Notes = append(p.Notes, note)
			*evidence = append(*evidence, Evidence{
				ID: len(*evidence) + 1, Scope: "peer", Ref: p.ID,
				Code: "reference_regression", Detail: note,
			})
		} else if ok {
			lastRef = cur
			lastRefSet = true
		}
		if lastRefSet {
			if d := int(ex.Stratum) - int(lastStratum); d > 2 {
				p.Incoherent = true
				note := fmt.Sprintf("%s: stratum jumped %d -> %d", ex.ID, lastStratum, ex.Stratum)
				p.Notes = append(p.Notes, note)
				*evidence = append(*evidence, Evidence{
					ID: len(*evidence) + 1, Scope: "peer", Ref: p.ID,
					Code: "stratum_jump", Detail: note,
				})
			}
		}
		lastStratum = ex.Stratum
	}
	if len(refIDs) > 1 {
		p.Incoherent = true
		ids := make([]uint32, 0, len(refIDs))
		for id := range refIDs {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		note := fmt.Sprintf("reference id changed across samples: %v", ids)
		p.Notes = append(p.Notes, note)
		*evidence = append(*evidence, Evidence{
			ID: len(*evidence) + 1, Scope: "peer", Ref: p.ID,
			Code: "reference_id_change", Detail: note,
		})
	}
}

func collectEras(p *PeerState, exchanges []*Exchange) {
	eraSet := map[uint32]bool{}
	for _, ex := range exchanges {
		if ex.Client != p.Client || ex.Peer != p.Server || ex.Rejected {
			continue
		}
		for _, c := range ex.Candidates {
			if c.Valid {
				eraSet[c.Eras[3]] = true
			}
		}
	}
	var eras []uint32
	for e := range eraSet {
		eras = append(eras, e)
	}
	sort.Slice(eras, func(i, j int) bool { return eras[i] < eras[j] })
	p.EraCandidates = eras
}

// rankPeers applies the deterministic selection order:
//  1. peers with at least one surviving sample,
//  2. coherent reference clocks before incoherent ones,
//  3. lower stratum,
//  4. smaller best root distance,
//  5. stable peer id as the final tie break.
func rankPeers(peers []*PeerState) []string {
	var avail []*PeerState
	for _, p := range peers {
		if p.BestExchange != "" {
			avail = append(avail, p)
		}
	}
	sort.SliceStable(avail, func(i, j int) bool {
		a, b := avail[i], avail[j]
		if a.Incoherent != b.Incoherent {
			return !a.Incoherent
		}
		if a.Stratum != b.Stratum {
			return a.Stratum < b.Stratum
		}
		if a.BestDistance != b.BestDistance {
			return a.BestDistance < b.BestDistance
		}
		return a.ID < b.ID
	})
	out := make([]string, 0, len(avail))
	for _, p := range avail {
		out = append(out, p.ID)
	}
	return out
}
