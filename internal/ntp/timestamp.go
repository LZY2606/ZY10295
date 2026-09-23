// Package ntp implements NTP wire-format types and exact integer
// fixed-point arithmetic for 32.32 timestamps and 16.16 short fields.
package ntp

import (
	"encoding/binary"
	"fmt"
)

// UnixEpochOffset is the number of seconds between 1900-01-01 and 1970-01-01.
const UnixEpochOffset = 2208988800

// EraSpan is the number of seconds covered by one 32-bit seconds era (2^32).
const EraSpan = uint64(1) << 32

// Timestamp is the 64-bit NTP timestamp as it appears on the wire:
// a 32-bit era-relative seconds field and a 32-bit fraction field.
type Timestamp struct {
	Sec  uint32
	Frac uint32
}

// Abs is an era-resolved absolute NTP time. Era counts 2^32-second
// epochs since 1900-01-01 (era 0 ends 2036-02-07).
type Abs struct {
	Era  uint32
	Sec  uint32
	Frac uint32
}

// AtEra resolves the 32-bit seconds field into an absolute time in era e.
func (t Timestamp) AtEra(e uint32) Abs {
	return Abs{Era: e, Sec: t.Sec, Frac: t.Frac}
}

// FromUnix builds the wire timestamp for a Unix time in a given era.
func FromUnix(unixSec int64, unixNsec int64, era uint32) Timestamp {
	ntpSec := unixSec + UnixEpochOffset - int64(era)*int64(EraSpan)
	frac := uint32((unixNsec * (1 << 32)) / 1_000_000_000)
	return Timestamp{Sec: uint32(ntpSec), Frac: frac}
}

// Unix converts an absolute NTP time to Unix seconds (fraction truncated
// toward zero in 2^-32 s units; use Fixed for exact arithmetic).
func (a Abs) Unix() int64 {
	return int64(a.Era)*int64(EraSpan) + int64(a.Sec) - UnixEpochOffset
}

// Fixed is a signed 32.32 fixed-point number of seconds:
// value = Sec + Frac/2^32. All arithmetic is exact integer math.
type Fixed struct {
	Sec  int64
	Frac uint32
}

// Sub returns a-b as an exact signed fixed-point value.
func Sub(a, b Abs) Fixed {
	sec := int64(a.Era)*int64(EraSpan) + int64(a.Sec) -
		(int64(b.Era)*int64(EraSpan) + int64(b.Sec))
	frac := int64(a.Frac) - int64(b.Frac)
	if frac < 0 {
		sec--
		frac += 1 << 32
	}
	return Fixed{Sec: sec, Frac: uint32(frac)}
}

// Add returns a+b with carry propagation from the fraction field.
func Add(a, b Fixed) Fixed {
	frac := uint64(a.Frac) + uint64(b.Frac)
	sec := a.Sec + b.Sec + int64(frac>>32)
	return Fixed{Sec: sec, Frac: uint32(frac)}
}

// Neg returns -a.
func Neg(a Fixed) Fixed {
	if a.Frac == 0 {
		return Fixed{Sec: -a.Sec}
	}
	return Fixed{Sec: -a.Sec - 1, Frac: -a.Frac}
}

// Half returns a/2, truncating the 2^-33 bit toward zero.
func Half(a Fixed) Fixed {
	sec := a.Sec >> 1 // floor division, also for negative Sec
	rem := a.Sec - sec*2
	frac := (uint64(rem)<<32 + uint64(a.Frac)) >> 1
	return Fixed{Sec: sec, Frac: uint32(frac)}
}

// Sign returns -1, 0 or +1.
func (a Fixed) Sign() int {
	switch {
	case a.Sec < 0:
		return -1
	case a.Sec == 0 && a.Frac == 0:
		return 0
	case a.Sec == 0:
		// 0 < value < 1, or -1 < value < 0; Frac != 0 with Sec==0 is positive.
		return 1
	default:
		return 1
	}
}

// AbsVal returns |a|.
func AbsVal(a Fixed) Fixed {
	if a.Sign() < 0 {
		return Neg(a)
	}
	return a
}

// Cmp compares a and b.
func Cmp(a, b Fixed) int {
	if a.Sec != b.Sec {
		if a.Sec < b.Sec {
			return -1
		}
		return 1
	}
	switch {
	case a.Frac < b.Frac:
		return -1
	case a.Frac > b.Frac:
		return 1
	}
	return 0
}

// Scale returns a*num/den using exact integer math (truncating toward zero
// in 2^-32 s units). Used for dispersion growth at a few ppm.
func Scale(a Fixed, num, den int64) Fixed {
	neg := a.Sign() < 0
	if neg {
		a = Neg(a)
	}
	secQ := a.Sec * num / den
	secR := a.Sec * num % den
	fracUnits := (secR*(1<<32) + int64(a.Frac)*num) / den
	out := Fixed{Sec: secQ + fracUnits/(1<<32), Frac: uint32(fracUnits % (1 << 32))}
	if neg {
		return Neg(out)
	}
	return out
}

// Float returns the value as float64 seconds (display only).
func (a Fixed) Float() float64 {
	return float64(a.Sec) + float64(a.Frac)/(1<<32)
}

func (a Fixed) String() string {
	return fmt.Sprintf("%.9fs", a.Float())
}

// Short is the 16.16 fixed-point format used by root delay and root
// dispersion fields.
type Short struct {
	Sec  uint16
	Frac uint16
}

// ToFixed widens a 16.16 short to a 32.32 fixed-point value.
func (s Short) ToFixed() Fixed {
	return Fixed{Sec: int64(s.Sec), Frac: uint32(s.Frac) << 16}
}

// UnwrapDelta returns a-b assuming the true difference is within ±2^31
// seconds, correcting a wrap of the 32-bit seconds field between the two
// timestamps (e.g. b just before and a just after the 2036 boundary).
func UnwrapDelta(a, b Timestamp) Fixed {
	sec := int64(a.Sec) - int64(b.Sec)
	if sec > int64(1<<31) {
		sec -= int64(EraSpan)
	} else if sec < -int64(1<<31) {
		sec += int64(EraSpan)
	}
	frac := int64(a.Frac) - int64(b.Frac)
	if frac < 0 {
		sec--
		frac += 1 << 32
	}
	return Fixed{Sec: sec, Frac: uint32(frac)}
}

// PrecisionToSeconds converts a signed precision exponent p to 2^p seconds.
func PrecisionToSeconds(p int8) float64 {
	if p >= 0 {
		return float64(uint64(1) << uint(p))
	}
	return 1.0 / float64(uint64(1)<<uint(-p))
}

// PrecisionValid reports whether a precision exponent is within the
// accepted bounds [-32, 24]. Values outside indicate a malformed packet.
func PrecisionValid(p int8) bool {
	return p >= -32 && p <= 24
}

// Encode writes the 64-bit wire form of t into b (big endian).
func (t Timestamp) Encode(b []byte) {
	binary.BigEndian.PutUint32(b[0:4], t.Sec)
	binary.BigEndian.PutUint32(b[4:8], t.Frac)
}

// DecodeTimestamp reads a 64-bit wire timestamp from b.
func DecodeTimestamp(b []byte) Timestamp {
	return Timestamp{
		Sec:  binary.BigEndian.Uint32(b[0:4]),
		Frac: binary.BigEndian.Uint32(b[4:8]),
	}
}

// SubFixed returns a-b for two fixed-point values.
func SubFixed(a, b Fixed) Fixed {
	return Add(a, Neg(b))
}
