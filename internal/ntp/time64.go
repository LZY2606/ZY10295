// Package ntp defines exact integer fixed-point types used by NTPv4 packets
// and all arithmetic in the era ledger.
//
// Units used by the compute path (no floating point):
//
//   - tick32: 2^-32 s, the quantum of the 64-bit NTP timestamp fraction.
//   - tick64: 2^-64 s, the quantum used for precision exponents and the
//     16.16 fixed-point packet fields (root delay / root dispersion).
//
// A resolved timestamp needs 96 bits (32-bit era + 32-bit seconds +
// 32-bit fraction), so ExtendedStamp keeps the era separate and all
// differences are evaluated with math/big.
package ntp

import (
	"errors"
	"math/big"
)

// SecPerEra is the 32-bit seconds field period (2^32 seconds).
const SecPerEra uint64 = 1 << 32

// UnixEpochSeconds is the number of NTP seconds between 1900-01-01 and 1970-01-01.
const UnixEpochSeconds uint64 = 2208988800

// Stamp is an unresolved 64-bit NTP timestamp (32-bit seconds + 32-bit fraction).
type Stamp struct {
	Sec  uint32
	Frac uint32
}

// Raw64 returns the on-wire encoding.
func (s Stamp) Raw64() uint64 { return uint64(s.Sec)<<32 | uint64(s.Frac) }

// StampFromRaw64 decodes an on-wire timestamp.
func StampFromRaw64(v uint64) Stamp {
	return Stamp{Sec: uint32(v >> 32), Frac: uint32(v)}
}

// Fp16 is a decoded non-negative 16.16 fixed-point packet field.
// The stored value is the raw on-wire word (units of 2^-16 s).
type Fp16 uint32

// Fp16FromRaw32 decodes an on-wire 16.16 field. Negative values on the
// wire (sign bit 0x00008000) are reported to the caller as an error
// instead of being folded into the unsigned range.
func Fp16FromRaw32(v uint32) (Fp16, error) {
	if v&0x00008000 != 0 {
		return 0, errors.New("ntp: negative 16.16 fixed-point field")
	}
	return Fp16(v), nil
}

// Raw32 encodes the field for the wire.
func (f Fp16) Raw32() uint32 { return uint32(f) }

// Ticks32 returns the value in 2^-32 s units (raw << 16).
func (f Fp16) Ticks32() uint64 { return uint64(f) << 16 }

// Tick64 returns the value in 2^-64 s units (raw << 48).
func (f Fp16) Tick64() uint64 { return uint64(f) << 48 }

// PrecisionTick64 evaluates an NTP precision exponent: 2^p seconds in
// tick64 units, i.e. 2^(64+p). Positive exponents are invalid and rejected;
// exponents below -63 underflow to zero as in RFC 5905.
func PrecisionTick64(precision int8) (uint64, error) {
	if precision > 0 {
		return 0, errors.New("ntp: non-negative precision exponent is invalid")
	}
	e := uint(-precision)
	if e >= 64 {
		return 0, nil
	}
	return uint64(1) << (64 - e), nil
}

// ExtendedStamp is a 64-bit NTP timestamp resolved against a concrete era.
type ExtendedStamp struct {
	Era uint32
	Stamp
}

// ExtendStamp resolves a raw timestamp against an era number.
func ExtendStamp(s Stamp, era uint32) ExtendedStamp {
	return ExtendedStamp{Era: era, Stamp: s}
}

// Ticks32Big returns the absolute value in tick32 as a big.Int.
func (e ExtendedStamp) Ticks32Big() *big.Int {
	t := new(big.Int).SetUint64(uint64(e.Era))
	t.Lsh(t, 64)
	t.Add(t, new(big.Int).SetUint64(e.Raw64()))
	return t
}

// Cmp compares two extended timestamps: -1, 0 or +1.
func (e ExtendedStamp) Cmp(o ExtendedStamp) int {
	if e.Era != o.Era {
		if e.Era < o.Era {
			return -1
		}
		return 1
	}
	if e.Raw64() < o.Raw64() {
		return -1
	}
	if e.Raw64() > o.Raw64() {
		return 1
	}
	return 0
}

// Sub returns a-b in tick32 units.
func Sub(a, b ExtendedStamp) *big.Int {
	return new(big.Int).Sub(a.Ticks32Big(), b.Ticks32Big())
}

// AddTick32 returns a + d tick32.
func AddTick32(a ExtendedStamp, d *big.Int) ExtendedStamp {
	t := a.Ticks32Big()
	t.Add(t, d)
	era := new(big.Int).Rsh(t, 64).Uint64()
	raw := new(big.Int).And(t, new(big.Int).SetUint64(^uint64(0))).Uint64()
	return ExtendedStamp{Era: uint32(era), Stamp: StampFromRaw64(raw)}
}

// UnixSeconds returns the whole-second unix epoch value.
func (e ExtendedStamp) UnixSeconds() int64 {
	s := uint64(e.Era)<<32 | uint64(e.Sec)
	return int64(s) - int64(UnixEpochSeconds)
}

// FromUnixNano builds an extended stamp from unix nanoseconds (exact fraction).
func FromUnixNano(unixNs int64) ExtendedStamp {
	t := big.NewInt(unixNs)
	t.Lsh(t, 32)
	t.Quo(t, big.NewInt(1_000_000_000))
	off := new(big.Int).SetUint64(UnixEpochSeconds)
	off.Lsh(off, 32)
	t.Add(t, off)
	mask := new(big.Int).SetUint64(^uint64(0))
	raw := new(big.Int).And(t, mask).Uint64()
	era := new(big.Int).Rsh(t, 64).Uint64()
	return ExtendedStamp{Era: uint32(era), Stamp: StampFromRaw64(raw)}
}

// NsFromTick32 converts a signed tick32 duration into nanoseconds exactly
// (truncation toward zero) plus the residual tick32 fraction for display.
func NsFromTick32(ticks int64) int64 {
	b := big.NewInt(ticks)
	b.Mul(b, big.NewInt(1_000_000_000))
	b.Quo(b, big.NewInt(1<<32))
	return b.Int64()
}

// Ticks32OfDuration converts a signed nanosecond duration into tick32 units.
func Ticks32OfDuration(ns int64) int64 {
	b := big.NewInt(ns)
	b.Lsh(b, 32)
	b.Quo(b, big.NewInt(1_000_000_000))
	return b.Int64()
}
