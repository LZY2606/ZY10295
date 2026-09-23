package ntp

import "testing"

func TestSubExact(t *testing.T) {
	a := Abs{Era: 0, Sec: 5, Frac: 0x80000000}
	b := Abs{Era: 0, Sec: 3, Frac: 0x40000000}
	d := Sub(a, b)
	if d.Sec != 2 || d.Frac != 0x40000000 {
		t.Fatalf("Sub = %+v, want {2 0x40000000}", d)
	}
}

func TestSubFractionBorrowBoundary(t *testing.T) {
	// a.Frac < b.Frac must borrow exactly one second, including at the
	// fraction boundary 0xFFFFFFFF.
	a := Abs{Era: 0, Sec: 5, Frac: 0}
	b := Abs{Era: 0, Sec: 4, Frac: 0xFFFFFFFF}
	d := Sub(a, b)
	if d.Sec != 0 || d.Frac != 1 {
		t.Fatalf("Sub = %+v, want {0 1}", d)
	}
}

func TestAddFractionCarryBoundary(t *testing.T) {
	s := Add(Fixed{Sec: 0, Frac: 0xFFFFFFFF}, Fixed{Sec: 0, Frac: 2})
	if s.Sec != 1 || s.Frac != 1 {
		t.Fatalf("Add = %+v, want {1 1}", s)
	}
}

func TestNegRoundTrip(t *testing.T) {
	for _, f := range []Fixed{{3, 0x40000000}, {0, 0}, {-7, 1}, {0, 0xFFFFFFFF}} {
		if got := Add(f, Neg(f)); got.Sec != 0 || got.Frac != 0 {
			t.Fatalf("Add(%v, Neg) = %v, want 0", f, got)
		}
	}
}

func TestHalfExactAndNegative(t *testing.T) {
	if h := Half(Fixed{Sec: 5, Frac: 0}); h.Sec != 2 || h.Frac != 0x80000000 {
		t.Fatalf("Half(5) = %+v, want {2 0x80000000}", h)
	}
	// -5 / 2 = -2.5 exactly
	if h := Half(Fixed{Sec: -5, Frac: 0}); h.Sec != -3 || h.Frac != 0x80000000 {
		t.Fatalf("Half(-5) = %+v, want {-3 0x80000000}", h)
	}
}

func TestUnwrapDeltaAcrossEraBoundary(t *testing.T) {
	// b sits 16 seconds before the wrap, a 10 seconds after: the raw
	// 32-bit subtraction is wildly negative and must be corrected.
	a := Timestamp{Sec: 10, Frac: 0}
	b := Timestamp{Sec: 0xFFFFFFF0, Frac: 0}
	d := UnwrapDelta(a, b)
	if d.Sec != 26 || d.Frac != 0 {
		t.Fatalf("UnwrapDelta = %+v, want {26 0}", d)
	}
	// reverse direction
	d = UnwrapDelta(b, a)
	if d.Sec != -26 || d.Frac != 0 {
		t.Fatalf("UnwrapDelta reverse = %+v, want {-26 0}", d)
	}
}

func TestUnwrapDeltaFractionBoundary(t *testing.T) {
	a := Timestamp{Sec: 100, Frac: 0}
	b := Timestamp{Sec: 99, Frac: 0xFFFFFFFF}
	d := UnwrapDelta(a, b)
	if d.Sec != 0 || d.Frac != 1 {
		t.Fatalf("UnwrapDelta = %+v, want {0 1}", d)
	}
}

func TestScaleDispersionGrowth(t *testing.T) {
	// 1 second at 15e-6 s/s = 15 us = 15*2^32/1e6 fraction units (truncated).
	g := Scale(Fixed{Sec: 1, Frac: 0}, 15, 1_000_000)
	want := uint32(15 * (1 << 32) / 1_000_000)
	if g.Sec != 0 || g.Frac != want {
		t.Fatalf("Scale = %+v, want {0 %d}", g, want)
	}
}

func TestShortToFixed(t *testing.T) {
	s := Short{Sec: 1, Frac: 0x8000} // 1.5 s
	f := s.ToFixed()
	if f.Sec != 1 || f.Frac != 0x80000000 {
		t.Fatalf("ToFixed = %+v, want {1 0x80000000}", f)
	}
}

func TestPrecisionBounds(t *testing.T) {
	for _, p := range []int8{-32, -25, 0, 24} {
		if !PrecisionValid(p) {
			t.Fatalf("precision %d should be valid", p)
		}
	}
	for _, p := range []int8{-33, -128, 25, 127} {
		if PrecisionValid(p) {
			t.Fatalf("precision %d should be invalid", p)
		}
	}
}

func TestPacketRoundTrip(t *testing.T) {
	p := Packet{
		LI: 1, VN: 4, Mode: ModeServer, Stratum: 2, Poll: 6, Precision: -25,
		RootDelay: Short{Sec: 0, Frac: 66}, RootDisp: Short{Sec: 0, Frac: 33},
		RefID: [4]byte{'G', 'P', 'S', 0},
		Ref:   Timestamp{Sec: 100, Frac: 1},
		Orig:  Timestamp{Sec: 200, Frac: 2},
		Rx:    Timestamp{Sec: 300, Frac: 3},
		Tx:    Timestamp{Sec: 400, Frac: 0xFFFFFFFF},
	}
	q, err := Parse(p.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if q != p {
		t.Fatalf("round trip mismatch:\n%+v\n%+v", q, p)
	}
}

func TestKod(t *testing.T) {
	p := Packet{Mode: ModeServer, Stratum: 0, RefID: [4]byte{'R', 'A', 'T', 'E'}}
	if !p.IsKod() || p.KissCode() != "RATE" {
		t.Fatalf("KoD not detected: %+v", p)
	}
	p.Stratum = 1
	if p.IsKod() {
		t.Fatal("stratum 1 must not be KoD")
	}
}
