package ntp

import (
	"encoding/binary"
	"errors"
)

// Leap indicator values (RFC 5905 §7.3).
const (
	LIWarning   = 0 // no warning
	LILastMin61 = 1 // last minute has 61 s (positive leap)
	LILastMin59 = 2 // last minute has 59 s (negative leap)
	LIAlarm     = 3 // alarm condition, clock not synchronized
)

// Mode values.
const (
	ModeReserved = iota
	ModeSymmetricActive
	ModeSymmetricPassive
	ModeClient
	ModeServer
	ModeBroadcast
	ModeNTPControl
	ModePrivateUse
)

// MinPacketLen is the length of the NTPv4 header.
const MinPacketLen = 48

// Packet is the parsed NTPv4 header. All raw 64-bit timestamp fields are
// retained on RawFields so the ledger never loses the imported wire data.
type Packet struct {
	LI          uint8
	Version     uint8
	Mode        uint8
	Stratum     uint8
	Poll        int8
	Precision   int8
	RootDelay   Fp16
	RootDisp    Fp16
	ReferenceID uint32
	Reference   Stamp
	Origin      Stamp
	Receive     Stamp
	Transmit    Stamp

	RawRootDelay uint32
	RawRootDisp  uint32
	RawFields    RawFields
}

// RawFields keeps every original 64-bit timestamp as received.
type RawFields struct {
	Reference uint64
	Origin    uint64
	Receive   uint64
	Transmit  uint64
	RawHeader [48]byte
}

// Parse decodes a 48-byte NTP packet. Fields shorter than 48 bytes,
// unknown versions (0 or >4) and negative 16.16 fields are reported.
func Parse(b []byte) (*Packet, error) {
	if len(b) < MinPacketLen {
		return nil, errors.New("ntp: packet shorter than 48 bytes")
	}
	p := &Packet{}
	p.LI = b[0] >> 6
	p.Version = (b[0] >> 3) & 0x07
	p.Mode = b[0] & 0x07
	p.Stratum = b[1]
	p.Poll = int8(b[2])
	p.Precision = int8(b[3])
	p.RawRootDelay = binary.BigEndian.Uint32(b[4:8])
	p.RawRootDisp = binary.BigEndian.Uint32(b[8:12])
	p.ReferenceID = binary.BigEndian.Uint32(b[12:16])
	rd, err := Fp16FromRaw32(p.RawRootDelay)
	if err != nil {
		return nil, err
	}
	p.RootDelay = rd
	rdd, err := Fp16FromRaw32(p.RawRootDisp)
	if err != nil {
		return nil, err
	}
	p.RootDisp = rdd
	p.Reference = StampFromRaw64(binary.BigEndian.Uint64(b[16:24]))
	p.Origin = StampFromRaw64(binary.BigEndian.Uint64(b[24:32]))
	p.Receive = StampFromRaw64(binary.BigEndian.Uint64(b[32:40]))
	p.Transmit = StampFromRaw64(binary.BigEndian.Uint64(b[40:48]))
	p.RawFields = RawFields{
		Reference: p.Reference.Raw64(),
		Origin:    p.Origin.Raw64(),
		Receive:   p.Receive.Raw64(),
		Transmit:  p.Transmit.Raw64(),
	}
	copy(p.RawFields.RawHeader[:], b[:48])
	return p, nil
}

// Encode writes the header back into 48 bytes.
func (p *Packet) Encode() [48]byte {
	var b [48]byte
	b[0] = p.LI<<6 | p.Version<<3 | p.Mode
	b[1] = p.Stratum
	b[2] = uint8(p.Poll)
	b[3] = uint8(p.Precision)
	binary.BigEndian.PutUint32(b[4:8], p.RootDelay.Raw32())
	binary.BigEndian.PutUint32(b[8:12], p.RootDisp.Raw32())
	binary.BigEndian.PutUint32(b[12:16], p.ReferenceID)
	binary.BigEndian.PutUint64(b[16:24], p.Reference.Raw64())
	binary.BigEndian.PutUint64(b[24:32], p.Origin.Raw64())
	binary.BigEndian.PutUint64(b[32:40], p.Receive.Raw64())
	binary.BigEndian.PutUint64(b[40:48], p.Transmit.Raw64())
	return b
}

// KissCode returns the ASCII kiss-o'-death code when the packet is a
// stratum-0 server response with an ASCII reference identifier.
func (p *Packet) KissCode() (string, bool) {
	if p.Stratum != 0 || p.Mode != ModeServer {
		return "", false
	}
	rid := p.ReferenceID
	for _, sh := range []uint32{rid >> 24, (rid >> 16) & 0xff, (rid >> 8) & 0xff, rid & 0xff} {
		c := byte(sh)
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return "", false
		}
	}
	return string([]byte{byte(rid >> 24), byte(rid >> 16), byte(rid >> 8), byte(rid)}), true
}
