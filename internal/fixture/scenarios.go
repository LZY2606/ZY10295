package fixture

import (
	"eraledger/internal/analysis"
	"eraledger/internal/ntp"
)

// NTP seconds around the 2036 rollover and the fixture leap boundary.
const (
	lastSecEra0 = uint32(0xFFFFFFFF)
	leapSec     = uint32(100) // first NTP second of era 1 carrying the leap
	unixWrap    = int64(2085978496)
)

// Server parameters.
var (
	rootDelayA = uint32(16384) // 0.250 s
	rootDispA  = uint32(8192)  // 0.125 s
	rootDelayD = uint32(24576) // 0.375 s (worse distance)
	rootDispD  = uint32(12288) // 0.1875 s
)

// ns builds unix nanoseconds from a whole second and a quarter fraction.
func ns(s int64, q int64) int64 { return s*1_000_000_000 + q*250_000_000 }

// Build returns the deterministic built-in capture.
func Build() *analysis.Capture {
	b := New()
	b.Leap(leapSec, ntp.LILastMin61)
	const client = "host-l"
	const srvA = "10.0.0.1"
	const srvB = "10.0.0.2"
	const srvC = "10.0.0.3"
	const srvD = "10.0.0.4"
	gps := "gps-pps"
	w := unixWrap

	// --- Peer A: clean stratum-2 server crossing the 2036 rollover ---

	// A1: 600 s before wrap. t1 raw sec = 2^32 - 600.
	tx1 := Stamp(lastSecEra0-599, 0)
	b.Send(client, srvA, gps, ns(w-600, 0), tx1)
	b.Resp(client, srvA, gps, ns(w-600, 3), RespHead{
		Stratum: 2, Poll: 6, Precision: -20,
		RootDelayRaw: rootDelayA, RootDispRaw: rootDispA,
		ReferenceID: AsciiRefID("GPS\000"),
		Reference: Stamp(lastSecEra0-600, 0),
		Origin:    tx1,
		Receive:   Stamp(lastSecEra0-599, Q(1)),
		Transmit:  Stamp(lastSecEra0-599, Q(2)),
	})

	// A2: exchange straddling the rollover. t1 is the final half-second of
	// era 0 (local send at w - 0.5 s); t2/t3 are seconds 4..5 of era 1.
	tx2 := Stamp(lastSecEra0, Q(2))
	b.Send(client, srvA, gps, ns(w-1, 2), tx2)
	b.Resp(client, srvA, gps, ns(w+5, 1), RespHead{
		Stratum: 2, Poll: 6, Precision: -20,
		RootDelayRaw: rootDelayA, RootDispRaw: rootDispA,
		ReferenceID: AsciiRefID("GPS\000"),
		Reference: Stamp(4, 0),
		Origin:    tx2,
		Receive:   Stamp(4, Q(3)),
		Transmit:  Stamp(5, 0),
	})

	// A3: leap announced (LI=1) well before the inserted second: raw policy.
	tx3 := Stamp(90, 0)
	b.Send(client, srvA, gps, ns(w+90, 0), tx3)
	b.Resp(client, srvA, gps, ns(w+90, 3), RespHead{
		LI: ntp.LILastMin61, Stratum: 2, Poll: 6, Precision: -20,
		RootDelayRaw: rootDelayA, RootDispRaw: rootDispA,
		ReferenceID: AsciiRefID("GPS\000"),
		Reference: Stamp(90, 0),
		Origin:    tx3,
		Receive:   Stamp(90, Q(1)),
		Transmit:  Stamp(90, Q(2)),
	})

	// A4: server timestamps inside the inserted second.
	tx4 := Stamp(leapSec, Q(2))
	b.Send(client, srvA, gps, ns(w+100, 2), tx4)
	b.Resp(client, srvA, gps, ns(w+100+1, 0), RespHead{
		LI: ntp.LILastMin61, Stratum: 2, Poll: 6, Precision: -20,
		RootDelayRaw: rootDelayA, RootDispRaw: rootDispA,
		ReferenceID: AsciiRefID("GPS\000"),
		Reference: Stamp(leapSec, 0),
		Origin:    tx4,
		Receive:   Stamp(leapSec, Q(1)),
		Transmit:  Stamp(leapSec, Q(2)),
	})

	// A5: post-leap, LI back to 0.
	tx5 := Stamp(110, 0)
	b.Send(client, srvA, gps, ns(w+110, 0), tx5)
	b.Resp(client, srvA, gps, ns(w+110, 3), RespHead{
		Stratum: 2, Poll: 6, Precision: -20,
		RootDelayRaw: rootDelayA, RootDispRaw: rootDispA,
		ReferenceID: AsciiRefID("GPS\000"),
		Reference: Stamp(110, 0),
		Origin:    tx5,
		Receive:   Stamp(110, Q(1)),
		Transmit:  Stamp(110, Q(2)),
	})

	// A6: duplicate transmit (same tx) then a repeated response.
	tx6 := Stamp(300, 0)
	b.Send(client, srvA, gps, ns(w+300, 0), tx6)
	b.Send(client, srvA, gps, ns(w+300, 0), tx6)
	dupHead := RespHead{
		Stratum: 2, Poll: 6, Precision: -20,
		RootDelayRaw: rootDelayA, RootDispRaw: rootDispA,
		ReferenceID: AsciiRefID("GPS\000"),
		Reference: Stamp(300, 0), Origin: tx6,
		Receive: Stamp(300, Q(1)), Transmit: Stamp(300, Q(2)),
	}
	b.Resp(client, srvA, gps, ns(w+300, 3), dupHead)
	b.Resp(client, srvA, gps, ns(w+300, 3), dupHead)

	// A7: late response (40 s after the request).
	tx7 := Stamp(400, 0)
	b.Send(client, srvA, gps, ns(w+400, 0), tx7)
	b.Resp(client, srvA, gps, ns(w+440, 1), RespHead{
		Stratum: 2, Poll: 6, Precision: -20,
		RootDelayRaw: rootDelayA, RootDispRaw: rootDispA,
		ReferenceID: AsciiRefID("GPS\000"),
		Reference: Stamp(440, 0), Origin: tx7,
		Receive: Stamp(440, 0), Transmit: Stamp(440, 0),
	})

	// --- Peer B: KoD and LI=3 ---

	// B1: kiss-o'-death DENY.
	tb1 := Stamp(500, 0)
	b.Send(client, srvB, gps, ns(w+500, 0), tb1)
	deny := &ntp.Packet{
		Version: 4, Mode: ntp.ModeServer, Stratum: 0,
		Poll: 6, Precision: -20,
		ReferenceID: AsciiRefID("DENY"),
		Origin:      ntp.StampFromRaw64(tb1),
		Receive:     ntp.StampFromRaw64(Stamp(500, Q(1))),
		Transmit:    ntp.StampFromRaw64(Stamp(500, Q(2))),
	}
	b.Raw(client, srvB, gps, ns(w+500, 3), "recv", deny)

	// B2: LI=3 unsynchronized server.
	tb2 := Stamp(510, 0)
	b.Send(client, srvB, gps, ns(w+510, 0), tb2)
	b.Resp(client, srvB, gps, ns(w+510, 3), RespHead{
		LI: ntp.LIAlarm, Stratum: 3, Poll: 6, Precision: -20,
		RootDelayRaw: rootDelayA, RootDispRaw: rootDispA,
		ReferenceID: AsciiRefID("LOCL"),
		Reference: Stamp(510, 0), Origin: tb2,
		Receive: Stamp(510, Q(1)), Transmit: Stamp(510, Q(2)),
	})

	// --- Peer C: origin mismatch then negative delay ---

	// C1: response whose origin matches no open request.
	b.Resp(client, srvC, gps, ns(w+600, 3), RespHead{
		Stratum: 2, Poll: 6, Precision: -20,
		RootDelayRaw: rootDelayA, RootDispRaw: rootDispA,
		ReferenceID: AsciiRefID("GPS\000"),
		Reference: Stamp(600, 0), Origin: Stamp(999, Q(1)),
		Receive: Stamp(600, Q(1)), Transmit: Stamp(600, Q(2)),
	})

	// C2: well-paired exchange whose server clocks produce negative delay.
	tc2 := Stamp(610, Q(2))
	b.Send(client, srvA, gps, ns(w+610, 0), Stamp(610, 0)) // unrelated A traffic
	b.Send(client, srvC, gps, ns(w+610, 2), tc2)
	b.Resp(client, srvC, gps, ns(w+610+1, 0), RespHead{
		Stratum: 2, Poll: 6, Precision: -20,
		RootDelayRaw: rootDelayA, RootDispRaw: rootDispA,
		ReferenceID: AsciiRefID("GPS\000"),
		Reference: Stamp(610, 0), Origin: tc2,
		// server claims 1.0 s of processing inside a 0.75 s local round
		// trip: the era-consistent reading has a negative delay (-0.25 s)
		Receive: Stamp(610, 0), Transmit: Stamp(611, 0),
	})

	// --- Peer D: incoherent reference clock (regression + refid + stratum) ---
	for _, q := range []struct {
		txSec, refSec uint32
		stratum       uint8
		refid         string
	}{
		{700, 700, 2, "GPS\000"},
		{710, 710, 2, "GPS\000"},
		{720, 600, 4, "PPS\000"},
	} {
		tx := Stamp(q.txSec, 0)
		b.Send(client, srvD, gps, ns(w+int64(q.txSec), 0), tx)
		b.Resp(client, srvD, gps, ns(w+int64(q.txSec), 3), RespHead{
			Stratum: q.stratum, Poll: 6, Precision: -20,
			RootDelayRaw: rootDelayD, RootDispRaw: rootDispD,
			ReferenceID: AsciiRefID(q.refid),
			Reference: Stamp(q.refSec, 0), Origin: tx,
			Receive: Stamp(q.txSec, Q(1)), Transmit: Stamp(q.txSec, Q(2)),
		})
	}

	return b.Build()
}
