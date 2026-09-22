package analysis

import (
	"math/big"
)

// Rat64 is an exact non-negative rational duration in tick64 units.
type Rat64 struct {
	N *big.Int
	D *big.Int
}

func newRat(n int64) Rat64 {
	return Rat64{N: big.NewInt(n), D: big.NewInt(1)}
}

func ratFrom(num, den *big.Int) Rat64 {
	r := Rat64{N: new(big.Int).Set(num), D: new(big.Int).Set(den)}
	return r.norm()
}

func (r Rat64) norm() Rat64 {
	if r.D.Sign() == 0 {
		return Rat64{N: new(big.Int), D: big.NewInt(1)}
	}
	g := new(big.Int).GCD(nil, nil, r.N, r.D)
	if g.Sign() != 0 {
		r.N.Quo(r.N, g)
		r.D.Quo(r.D, g)
	}
	if r.D.Sign() < 0 {
		r.N.Neg(r.N)
		r.D.Neg(r.D)
	}
	return r
}

// Add returns r+o.
func (r Rat64) Add(o Rat64) Rat64 {
	n := new(big.Int).Mul(r.N, o.D)
	n.Add(n, new(big.Int).Mul(o.N, r.D))
	return ratFrom(n, new(big.Int).Mul(r.D, o.D))
}

// Cmp returns -1/0/+1.
func (r Rat64) Cmp(o Rat64) int {
	l := new(big.Int).Mul(r.N, o.D)
	rr := new(big.Int).Mul(o.N, r.D)
	return l.Cmp(rr)
}

// Floor returns the integer floor (always exact-safe tick64 here).
func (r Rat64) Floor() *big.Int {
	return new(big.Int).Quo(r.N, r.D)
}

// IsZero reports whether the value is zero.
func (r Rat64) IsZero() bool { return r.N.Sign() == 0 }
