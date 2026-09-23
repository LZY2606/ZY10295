package ntp

import (
	"encoding/binary"
	"fmt"
)

// PacketLen is the size of a bare NTPv4 packet on the wire.
const PacketLen = 48

// Leap indicator values.
const (
	LINone     = 0 // no warning
	LILast61   = 1 // last minute of the month has 61 seconds
	LILast59   = 2 // last minute of the month has 59 seconds
	LIUnsynced = 3 // clock unsynchronized
)

// Mode values.
const (
	ModeClient = 3
	ModeServer = 4
)

// Packet is a decoded 48-byte NTP packet. All timestamp fields keep the
// raw 32.32 wire values; era resolution happens in the ledger.
type Packet struct {
	LI        uint8
	VN        uint8
	Mode      uint8
	Stratum   uint8
	Poll      int8
	Precision int8
	RootDelay Short
	RootDisp  Short
	RefID     [4]byte
	Ref       Timestamp
	Orig      Timestamp
	Rx        Timestamp
	Tx        Timestamp
}

// Parse decodes a 48-byte NTP packet.
func Parse(b []byte) (Packet, error) {
	if len(b) < PacketLen {
		return Packet{}, fmt.Errorf("ntp: packet too short: %d bytes", len(b))
	}
	var p Packet
	p.LI = b[0] >> 6
	p.VN = (b[0] >> 3) & 0x7
	p.Mode = b[0] & 0x7
	p.Stratum = b[1]
	p.Poll = int8(b[2])
	p.Precision = int8(b[3])
	p.RootDelay = Short{Sec: binary.BigEndian.Uint16(b[4:6]), Frac: binary.BigEndian.Uint16(b[6:8])}
	p.RootDisp = Short{Sec: binary.BigEndian.Uint16(b[8:10]), Frac: binary.BigEndian.Uint16(b[10:12])}
	copy(p.RefID[:], b[12:16])
	p.Ref = DecodeTimestamp(b[16:24])
	p.Orig = DecodeTimestamp(b[24:32])
	p.Rx = DecodeTimestamp(b[32:40])
	p.Tx = DecodeTimestamp(b[40:48])
	return p, nil
}

// Bytes encodes the packet into its 48-byte wire form.
func (p Packet) Bytes() []byte {
	b := make([]byte, PacketLen)
	b[0] = p.LI<<6 | p.VN<<3 | p.Mode
	b[1] = p.Stratum
	b[2] = byte(p.Poll)
	b[3] = byte(p.Precision)
	binary.BigEndian.PutUint16(b[4:6], p.RootDelay.Sec)
	binary.BigEndian.PutUint16(b[6:8], p.RootDelay.Frac)
	binary.BigEndian.PutUint16(b[8:10], p.RootDisp.Sec)
	binary.BigEndian.PutUint16(b[10:12], p.RootDisp.Frac)
	copy(b[12:16], p.RefID[:])
	p.Ref.Encode(b[16:24])
	p.Orig.Encode(b[24:32])
	p.Rx.Encode(b[32:40])
	p.Tx.Encode(b[40:48])
	return b
}

// IsKod reports whether the packet is a kiss-o'-death response
// (server mode, stratum 0). The kiss code lives in the RefID field.
func (p Packet) IsKod() bool {
	return p.Mode == ModeServer && p.Stratum == 0
}

// KissCode returns the 4-byte ASCII kiss code for a KoD packet.
func (p Packet) KissCode() string {
	return string(p.RefID[:])
}

// RefIDString renders the reference identifier: ASCII for stratum 0/1
// sources, dotted-quad otherwise.
func (p Packet) RefIDString() string {
	if p.Stratum <= 1 {
		return string(p.RefID[:])
	}
	return fmt.Sprintf("%d.%d.%d.%d", p.RefID[0], p.RefID[1], p.RefID[2], p.RefID[3])
}
