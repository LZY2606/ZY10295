package store_test

import (
	"path/filepath"
	"testing"

	"ntp-ledger/internal/fixture"
	"ntp-ledger/internal/ledger"
	"ntp-ledger/internal/ntp"
	"ntp-ledger/internal/store"
)

func openSeeded(t *testing.T) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.Seed(st); err != nil {
		t.Fatal(err)
	}
	return st, path
}

func TestMigrateIdempotent(t *testing.T) {
	st, path := openSeeded(t)
	st.Close()
	// reopening must not fail or re-apply migrations
	st2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
}

func TestSeedAndReload(t *testing.T) {
	st, _ := openSeeded(t)
	defer st.Close()
	empty, err := st.IsEmpty()
	if err != nil || empty {
		t.Fatalf("IsEmpty = %v, %v", empty, err)
	}
	peers, err := st.LoadPeers()
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 3 {
		t.Fatalf("peers = %d, want 3", len(peers))
	}
	total := 0
	for _, p := range peers {
		total += len(p.Exchanges)
	}
	if total != 19 {
		t.Fatalf("exchanges = %d, want 19", total)
	}
	anchors, err := st.Anchors()
	if err != nil || len(anchors) != 1 {
		t.Fatalf("anchors = %v, %v", anchors, err)
	}
	if anchors[0].UnixSec != fixture.DefaultAnchorUnix {
		t.Fatalf("anchor unix = %d, want %d", anchors[0].UnixSec, fixture.DefaultAnchorUnix)
	}
}

func TestReloadEvaluatesIdentically(t *testing.T) {
	st, _ := openSeeded(t)
	defer st.Close()
	peers, err := st.LoadPeers()
	if err != nil {
		t.Fatal(err)
	}
	anchors, _ := st.Anchors()
	abs := anchors[0].Abs()
	opts := ledger.Options{Anchor: &abs, MaxEra: 1, Leap: ledger.LeapPermissive}
	fromDB := ledger.Evaluate(peers, opts)
	fromFixture := ledger.Evaluate(fixture.Build(), opts)
	if len(fromDB) != len(fromFixture) {
		t.Fatalf("peer count %d vs %d", len(fromDB), len(fromFixture))
	}
	for i := range fromDB {
		a, b := fromDB[i], fromFixture[i]
		if a.Reach != b.Reach || len(a.Samples) != len(b.Samples) || len(a.Evals) != len(b.Evals) {
			t.Fatalf("peer %s: db and fixture evaluation differ", a.Name)
		}
		for j := range a.Evals {
			ea, eb := a.Evals[j], b.Evals[j]
			if ea.Accepted != eb.Accepted || ea.Reason != eb.Reason ||
				ea.Delay != eb.Delay || ea.Offset != eb.Offset {
				t.Fatalf("peer %s seq %d: db and fixture differ", a.Name, ea.Ex.Seq)
			}
		}
	}
}

func TestAnchorAbs(t *testing.T) {
	a := store.Anchor{UnixSec: fixture.WrapUnix + 10}
	abs := a.Abs()
	if abs.Era != 1 || abs.Sec != 10 {
		t.Fatalf("abs = %+v, want era 1 sec 10", abs)
	}
	b := store.Anchor{UnixSec: fixture.WrapUnix - 10}
	absB := b.Abs()
	if absB.Era != 0 || absB.Sec != uint32(ntp.EraSpan-10) {
		t.Fatalf("abs = %+v, want era 0 sec 2^32-10", absB)
	}
}
