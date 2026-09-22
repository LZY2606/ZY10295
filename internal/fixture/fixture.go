// Package fixture builds the deterministic built-in capture used by the
// server and the test suite. All timestamps are integer tick values; the
// data never depends on wall time or the network.
package fixture

import (
	"encoding/binary"
	"eraledger/internal/analysis"
	"eraledger/internal/ntp"
)

// Builder accumulates capture records.
type Builder struct {
	sources map[string]analysis.ClockSource
	leaps   []analysis.LeapBound
	recs    []analysis.CaptureInput
	seq     int
}

// New returns a builder seeded with the capture clock sources.
func New() *Builder {
	b := &Builder{sources: map[string]analysis.ClockSource{}}
	b.Source("gps-pps", "gps-pps", true, "disciplined 1PPS local capture clock")
	b.Source("free-run", "local-oscillator", false, "unsynchronized local oscillator")
	return b
}

// Source registers a capture clock source.
func (b *Builder) Source(id, kind string, trusted bool, note string) *Builder {
	b.sources[id] = analysis.ClockSource{ID: id, Kind: kind, Trusted: trusted, Note: note}
	return b
}

// Leap registers a leap boundary (NTP seconds + LI kind).
func (b *Builder) Leap(sec uint32, kind uint8) *Builder {
	b.leaps = append(b.leaps, analysis.LeapBound{NTPSeconds: sec, Kind: kind})
	return b
}

// Send records a client request.
func (b *Builder) Send(client, server, clock string, localNS int64, tx uint64) *Builder {
	b.seq++
	pkt := &ntp.Packet{
		Version: 4, Mode: ntp.ModeClient,
		Poll: 6, Precision: -24,
		Transmit: ntp.StampFromRaw64(tx),
	}
	raw := pkt.Encode()
	b.recs = append(b.recs, analysis.CaptureInput{
		Seq: b.seq, Client: client, Server: server,
		Packet: raw[:], LocalNS: localNS, LocalClock: clock, Dir: "send",
	})
	return b
}

// RespHead is the settable header for a server response. Root delay and
// root dispersion are raw 16.16 wire words.
type RespHead struct {
	LI           uint8
	Stratum      uint8
	Poll         int8
	Precision    int8
	RootDelayRaw uint32
	RootDispRaw  uint32
	ReferenceID  uint32
	Reference    uint64
	Origin       uint64
	Receive      uint64
	Transmit     uint64
}

// Resp records a server response.
func (b *Builder) Resp(client, server, clock string, localNS int64, h RespHead) *Builder {
	b.seq++
	rd := mustFp(h.RootDelayRaw)
	rp := mustFp(h.RootDispRaw)
	pkt := &ntp.Packet{
		LI: h.LI, Version: 4, Mode: ntp.ModeServer,
		Stratum: h.Stratum, Poll: h.Poll, Precision: h.Precision,
		RootDelay: rd, RootDisp: rp,
		RawRootDelay: rd.Raw32(), RawRootDisp: rp.Raw32(),
		ReferenceID: h.ReferenceID,
		Reference:   ntp.StampFromRaw64(h.Reference),
		Origin:      ntp.StampFromRaw64(h.Origin),
		Receive:     ntp.StampFromRaw64(h.Receive),
		Transmit:    ntp.StampFromRaw64(h.Transmit),
	}
	raw := pkt.Encode()
	b.recs = append(b.recs, analysis.CaptureInput{
		Seq: b.seq, Client: client, Server: server,
		Packet: raw[:], LocalNS: localNS, LocalClock: clock, Dir: "recv",
	})
	return b
}

// Raw records an arbitrary packet.
func (b *Builder) Raw(client, server, clock string, localNS int64, dir string, pkt *ntp.Packet) *Builder {
	b.seq++
	raw := pkt.Encode()
	b.recs = append(b.recs, analysis.CaptureInput{
		Seq: b.seq, Client: client, Server: server,
		Packet: raw[:], LocalNS: localNS, LocalClock: clock, Dir: dir,
	})
	return b
}

// Build produces the capture.
func (b *Builder) Build() *analysis.Capture {
	srcs := make([]analysis.ClockSource, 0, len(b.sources))
	for _, s := range b.sources {
		srcs = append(srcs, s)
	}
	return &analysis.Capture{Sources: srcs, Leaps: b.leaps, Records: b.recs}
}

func mustFp(v uint32) ntp.Fp16 {
	f, err := ntp.Fp16FromRaw32(v)
	if err != nil {
		panic(err)
	}
	return f
}

// AsciiRefID packs four ASCII bytes as a reference id.
func AsciiRefID(s string) uint32 {
	if len(s) != 4 {
		panic("refid must be 4 bytes")
	}
	return binary.BigEndian.Uint32([]byte(s))
}

// Stamp packs NTP seconds and a 32-bit fraction.
func Stamp(sec uint32, frac uint32) uint64 { return uint64(sec)<<32 | uint64(frac) }

// Q returns n quarters of a second in fraction units (n * 2^30).
func Q(quarters uint32) uint32 { return quarters << 30 }
