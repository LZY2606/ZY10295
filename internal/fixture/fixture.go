// Package fixture builds the built-in demonstration dataset: exchanges
// on both sides of the 2036 era wrap, duplicate transmits, kiss-o'-death,
// late responses, leap-indicator changes and incoherent reference
// clocks. All times are derived from constants, never from the wall
// clock, so every run is identical.
package fixture

import (
	"ntp-ledger/internal/ledger"
	"ntp-ledger/internal/ntp"
	"ntp-ledger/internal/store"
)

// WrapUnix is the Unix second where NTP era 0 ends and era 1 begins
// (2036-02-07T06:28:16Z).
const WrapUnix = int64(1)<<32 - ntp.UnixEpochOffset

// DefaultAnchor is the trusted anchor offered by the demo page.
const DefaultAnchorUnix = WrapUnix + 200*86400 // 200 days into era 1

type params struct {
	peerID   int64
	seq      int
	order    int
	clock    string
	known    bool
	t1unix   int64 // client send time, unix seconds
	rttMs    int64 // client round-trip time
	procMs   int64 // server processing time
	offMs    int64 // server clock offset relative to client
	li       uint8
	stratum  uint8
	refid    string
	prec     int8
	kod      bool
	origSkew int64 // seconds added to originate field (origin mismatch)
	dupTxOf  *ntp.Timestamp
}

func short(ms int64) ntp.Short {
	v := uint32(ms) * 65536 / 1000
	return ntp.Short{Sec: uint16(v >> 16), Frac: uint16(v)}
}

func mk(p params) *ledger.Exchange {
	t1ns := p.t1unix * 1_000_000_000
	t4ns := t1ns + p.rttMs*1_000_000
	rxns := t1ns + p.rttMs*500_000 + p.offMs*1_000_000
	txns := rxns + p.procMs*1_000_000

	eraOf := func(ns int64) uint32 {
		return uint32(uint64(ns/1_000_000_000+ntp.UnixEpochOffset) / ntp.EraSpan)
	}
	ex := &ledger.Exchange{
		PeerID:       p.peerID,
		Seq:          p.seq,
		RecvOrder:    p.order,
		CaptureClock: p.clock,
		ClientKnown:  p.known,
		T1UnixNs:     t1ns,
		T4UnixNs:     t4ns,
		T1:           ntp.FromUnix(t1ns/1_000_000_000, t1ns%1_000_000_000, eraOf(t1ns)),
		T4:           ntp.FromUnix(t4ns/1_000_000_000, t4ns%1_000_000_000, eraOf(t4ns)),
	}
	pkt := ntp.Packet{
		LI:        p.li,
		VN:        4,
		Mode:      ntp.ModeServer,
		Stratum:   p.stratum,
		Poll:      6,
		Precision: p.prec,
		RootDelay: short(1),
		RootDisp:  short(1),
		Orig:      ex.T1,
		Rx:        ntp.FromUnix(rxns/1_000_000_000, rxns%1_000_000_000, eraOf(rxns)),
		Tx:        ntp.FromUnix(txns/1_000_000_000, txns%1_000_000_000, eraOf(txns)),
		Ref:       ntp.FromUnix(rxns/1_000_000_000-300, 0, eraOf(rxns)),
	}
	var refid [4]byte
	copy(refid[:], p.refid)
	pkt.RefID = refid
	if p.kod {
		pkt.Stratum = 0
		copy(pkt.RefID[:], "RATE")
	}
	if p.origSkew != 0 {
		pkt.Orig.Sec += uint32(p.origSkew)
	}
	if p.dupTxOf != nil {
		pkt.Tx = *p.dupTxOf
	}
	ex.Resp = pkt
	ex.Raw = pkt.Bytes()
	return ex
}

// Build constructs the fixture peers and exchanges.
func Build() []ledger.PeerInput {
	pre := WrapUnix - 4000  // about 67 minutes before the wrap
	post := WrapUnix + 4000 // about 67 minutes after the wrap

	gps := []params{
		{seq: 1, order: 1, t1unix: pre + 0, rttMs: 40, procMs: 2, offMs: 3, stratum: 1, refid: "GPS", prec: -25},
		{seq: 2, order: 2, t1unix: pre + 64, rttMs: 42, procMs: 2, offMs: 3, stratum: 1, refid: "GPS", prec: -25},
		{seq: 3, order: 3, t1unix: pre + 128, rttMs: 38, procMs: 2, offMs: 4, stratum: 1, refid: "GPS", prec: -25},
		// exchanges on the far side of the 2036 wrap: era 1, small Sec
		{seq: 4, order: 4, t1unix: post + 0, rttMs: 41, procMs: 2, offMs: 3, stratum: 1, refid: "GPS", prec: -25},
		{seq: 5, order: 5, t1unix: post + 64, rttMs: 39, procMs: 2, offMs: 3, stratum: 1, refid: "GPS", prec: -25},
		{seq: 6, order: 6, t1unix: post + 128, rttMs: 40, procMs: 2, offMs: 3, stratum: 1, refid: "GPS", prec: -25},
	}

	// Captured through a 32-bit tap: no full client wall time, so the
	// era of these post-wrap exchanges is ambiguous without an anchor.
	tap := []params{
		{seq: 1, order: 1, known: false, clock: "tap32", t1unix: post + 1000, rttMs: 60, procMs: 3, offMs: -8, stratum: 2, refid: "GPS", prec: -22},
		{seq: 2, order: 2, known: false, clock: "tap32", t1unix: post + 1064, rttMs: 62, procMs: 3, offMs: -8, stratum: 2, refid: "GPS", prec: -22},
		{seq: 3, order: 3, known: false, clock: "tap32", t1unix: post + 1128, rttMs: 61, procMs: 3, offMs: -7, stratum: 2, refid: "GPS", prec: -22},
	}

	flakyBase := pre - 10000
	flaky := []params{
		{seq: 1, order: 1, t1unix: flakyBase + 0, rttMs: 50, procMs: 2, offMs: 12, stratum: 2, refid: "GPS", prec: -20},
		{seq: 2, order: 2, t1unix: flakyBase + 64, rttMs: 50, procMs: 2, offMs: 12, stratum: 2, refid: "GPS", prec: -20, kod: true},
		{seq: 3, order: 3, t1unix: flakyBase + 128, rttMs: 51, procMs: 2, offMs: 12, stratum: 2, refid: "PPS", prec: -20},
		{seq: 4, order: 6, t1unix: flakyBase + 192, rttMs: 49, procMs: 2, offMs: 13, stratum: 2, refid: "PPS", prec: -20}, // late: answered after seq 5
		{seq: 5, order: 4, t1unix: flakyBase + 256, rttMs: 50, procMs: 2, offMs: 12, stratum: 2, refid: "PPS", prec: -20},
		{seq: 6, order: 5, t1unix: flakyBase + 320, rttMs: 50, procMs: 2, offMs: 12, li: ntp.LILast61, stratum: 2, refid: "PPS", prec: -20},
		{seq: 7, order: 7, t1unix: flakyBase + 384, rttMs: 50, procMs: 2, offMs: 12, li: ntp.LIUnsynced, stratum: 2, refid: "PPS", prec: -20},
		{seq: 8, order: 8, t1unix: flakyBase + 448, rttMs: 50, procMs: 2, offMs: 12, stratum: 2, refid: "PPS", prec: -20, origSkew: 1},
		{seq: 9, order: 9, t1unix: flakyBase + 512, rttMs: 1, procMs: 5, offMs: 0, stratum: 2, refid: "PPS", prec: -20},                  // negative delay
		{seq: 10, order: 10, t1unix: flakyBase + 576, rttMs: 50, procMs: 2, offMs: 12, stratum: 2, refid: "\x85\xf3\x02\x1e", prec: -33}, // bad precision, IPv4 refid
	}

	mkAll := func(peerID int64, clock string, ps []params) []*ledger.Exchange {
		out := make([]*ledger.Exchange, 0, len(ps))
		known := clock != "tap32"
		var firstTx *ntp.Timestamp
		for _, pp := range ps {
			pp.peerID = peerID
			if pp.clock == "" {
				pp.clock = clock
			}
			if !pp.known {
				pp.known = known
			}
			ex := mk(pp)
			if peerID == 3 && pp.seq == 3 && firstTx != nil {
				// duplicate of the first response's transmit timestamp
				ex.Resp.Tx = *firstTx
				ex.Raw = ex.Resp.Bytes()
			}
			if peerID == 3 && pp.seq == 1 {
				tx := ex.Resp.Tx
				firstTx = &tx
			}
			out = append(out, ex)
		}
		return out
	}

	return []ledger.PeerInput{
		{ID: 1, Name: "gps-stratum1", Addr: "192.0.2.10", Exchanges: mkAll(1, "unix", gps)},
		{ID: 2, Name: "tap32-only", Addr: "192.0.2.20", Exchanges: mkAll(2, "tap32", tap)},
		{ID: 3, Name: "flaky-campus", Addr: "192.0.2.30", Exchanges: mkAll(3, "unix", flaky)},
	}
}

// Seed inserts the fixture into an empty store, plus the default anchor.
func Seed(s *store.Store) error {
	empty, err := s.IsEmpty()
	if err != nil {
		return err
	}
	if !empty {
		return nil
	}
	for _, p := range Build() {
		pid, err := s.InsertPeer(p.Name, p.Addr)
		if err != nil {
			return err
		}
		for _, ex := range p.Exchanges {
			ex.PeerID = pid
			if _, err := s.InsertExchange(ex); err != nil {
				return err
			}
		}
	}
	return s.AddAnchor("gps-2037", DefaultAnchorUnix, "trusted GPS anchor, 200 days into era 1")
}
