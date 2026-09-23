package ledger_test

import (
	"reflect"
	"testing"

	"ntp-ledger/internal/fixture"
	"ntp-ledger/internal/ledger"
	"ntp-ledger/internal/ntp"
)

func anchorOpts() ledger.Options {
	a := ntp.Abs{Era: 1, Sec: 200 * 86400} // matches fixture.DefaultAnchorUnix
	return ledger.Options{Anchor: &a, MaxEra: 1, Leap: ledger.LeapPermissive}
}

func findPeer(peers []*ledger.Peer, name string) *ledger.Peer {
	for _, p := range peers {
		if p.Name == name {
			return p
		}
	}
	return nil
}

func evalBySeq(p *ledger.Peer, seq int) *ledger.Eval {
	for _, ev := range p.Evals {
		if ev.Ex.Seq == seq {
			return ev
		}
	}
	return nil
}

func TestEraResolvedAcrossWrapWithKnownClientClock(t *testing.T) {
	peers := ledger.Evaluate(fixture.Build(), anchorOpts())
	gps := findPeer(peers, "gps-stratum1")
	if gps == nil {
		t.Fatal("peer gps-stratum1 missing")
	}
	for seq, wantEra := range map[int]uint32{1: 0, 2: 0, 3: 0, 4: 1, 5: 1, 6: 1} {
		ev := evalBySeq(gps, seq)
		if ev == nil || ev.Selected == nil {
			t.Fatalf("seq %d: no selected era", seq)
		}
		if ev.Selected.ServerEra != wantEra {
			t.Fatalf("seq %d: server era %d, want %d", seq, ev.Selected.ServerEra, wantEra)
		}
		if len(ev.EraCands) != 1 {
			t.Fatalf("seq %d: %d candidates, want exactly 1", seq, len(ev.EraCands))
		}
		if !ev.Accepted {
			t.Fatalf("seq %d: rejected: %s", seq, ev.Reason)
		}
	}
}

func TestEraCandidatesRetainedWithoutAnchor(t *testing.T) {
	opts := ledger.Options{MaxEra: 1, Leap: ledger.LeapPermissive}
	peers := ledger.Evaluate(fixture.Build(), opts)
	tap := findPeer(peers, "tap32-only")
	if tap == nil {
		t.Fatal("peer tap32-only missing")
	}
	for _, ev := range tap.Evals {
		if len(ev.EraCands) != 2 {
			t.Fatalf("seq %d: %d candidates, want 2 (era 0 and 1)", ev.Ex.Seq, len(ev.EraCands))
		}
		if ev.Selected != nil {
			t.Fatalf("seq %d: era selected without anchor", ev.Ex.Seq)
		}
	}
}

func TestEraSelectionDeterministicWithAnchor(t *testing.T) {
	first := ledger.Evaluate(fixture.Build(), anchorOpts())
	second := ledger.Evaluate(fixture.Build(), anchorOpts())
	if !reflect.DeepEqual(first, second) {
		t.Fatal("two evaluations with the same anchor differ")
	}
	tap := findPeer(first, "tap32-only")
	for _, ev := range tap.Evals {
		if ev.Selected == nil || ev.Selected.ServerEra != 1 {
			t.Fatalf("seq %d: anchor at era 1 must select era 1, got %+v", ev.Ex.Seq, ev.Selected)
		}
	}
}

func TestDelayOffsetEraIndependent(t *testing.T) {
	// The same wire fields evaluated with or without an anchor must give
	// identical delay/offset: era only places the exchange in time.
	withAnchor := ledger.Evaluate(fixture.Build(), anchorOpts())
	without := ledger.Evaluate(fixture.Build(), ledger.Options{MaxEra: 1, Leap: ledger.LeapPermissive})
	for i, p := range withAnchor {
		for j, ev := range p.Evals {
			other := without[i].Evals[j]
			if ev.Delay != other.Delay || ev.Offset != other.Offset {
				t.Fatalf("peer %s seq %d: delay/offset depend on era", p.Name, ev.Ex.Seq)
			}
		}
	}
}

func TestOffsetApproximation(t *testing.T) {
	peers := ledger.Evaluate(fixture.Build(), anchorOpts())
	gps := findPeer(peers, "gps-stratum1")
	// fixture: offset 3 ms + half of 2 ms server processing = 4 ms
	ev := evalBySeq(gps, 1)
	got := ev.Offset.Float() * 1000
	if got < 3.9 || got > 4.1 {
		t.Fatalf("offset = %.4f ms, want about 4 ms", got)
	}
	// delay = 40 ms RTT - 2 ms server processing = 38 ms
	if d := ev.Delay.Float() * 1000; d < 37.9 || d > 38.1 {
		t.Fatalf("delay = %.4f ms, want about 38 ms", d)
	}
	if ev.RootDist.Sign() <= 0 || ev.Dispersion.Sign() <= 0 {
		t.Fatalf("root distance and dispersion must be positive: %s %s", ev.RootDist, ev.Dispersion)
	}
}

func TestFilterRejectionsAndEvidence(t *testing.T) {
	peers := ledger.Evaluate(fixture.Build(), anchorOpts())
	flaky := findPeer(peers, "flaky-campus")
	want := map[int]string{
		2:  "kod",
		3:  "duplicate-transmit",
		4:  "late-response",
		7:  "li-unsync",
		8:  "origin-mismatch",
		9:  "negative-delay",
		10: "precision-bounds",
	}
	for seq, reason := range want {
		ev := evalBySeq(flaky, seq)
		if ev == nil {
			t.Fatalf("seq %d missing", seq)
		}
		if ev.Accepted {
			t.Fatalf("seq %d: accepted, want reject %s", seq, reason)
		}
		if ev.Reason != reason {
			t.Fatalf("seq %d: reason %q, want %q", seq, ev.Reason, reason)
		}
		// full evidence trail: every filter recorded a verdict, in order
		if len(ev.Verdicts) != len(ledger.FilterNames) {
			t.Fatalf("seq %d: %d verdicts, want %d", seq, len(ev.Verdicts), len(ledger.FilterNames))
		}
		for i, v := range ev.Verdicts {
			if v.Name != ledger.FilterNames[i] {
				t.Fatalf("seq %d: verdict %d is %q, want %q", seq, i, v.Name, ledger.FilterNames[i])
			}
		}
	}
	for _, seq := range []int{1, 5, 6} {
		if ev := evalBySeq(flaky, seq); !ev.Accepted {
			t.Fatalf("seq %d: rejected (%s), want accepted", seq, ev.Reason)
		}
	}
}

func TestLeapPolicyStrictRejectsAnnouncedSample(t *testing.T) {
	opts := anchorOpts()
	opts.Leap = ledger.LeapStrict
	peers := ledger.Evaluate(fixture.Build(), opts)
	flaky := findPeer(peers, "flaky-campus")
	ev := evalBySeq(flaky, 6) // LI=1 announced
	if ev.Accepted || ev.Reason != "leap-announced" {
		t.Fatalf("strict policy: accepted=%v reason=%q, want leap-announced reject", ev.Accepted, ev.Reason)
	}
	// permissive keeps it, flagged but never time-shifted
	opts.Leap = ledger.LeapPermissive
	peers = ledger.Evaluate(fixture.Build(), opts)
	flaky = findPeer(peers, "flaky-campus")
	ev = evalBySeq(flaky, 6)
	if !ev.Accepted {
		t.Fatal("permissive policy must accept announced sample")
	}
	var flagged bool
	for _, s := range flaky.Samples {
		if s.Seq == 6 {
			flagged = s.LeapFlag
		}
	}
	if !flagged {
		t.Fatal("sample 6 must carry the leap flag")
	}
}

func TestReachRegister(t *testing.T) {
	peers := ledger.Evaluate(fixture.Build(), anchorOpts())
	gps := findPeer(peers, "gps-stratum1")
	if gps.Reach != 0x3f { // six accepted exchanges: 0b111111
		t.Fatalf("gps reach = %03o, want 077", gps.Reach)
	}
	flaky := findPeer(peers, "flaky-campus")
	if len(flaky.Samples) != 3 { // seq 1, 5, 6 accepted under permissive
		t.Fatalf("flaky samples = %d, want 3", len(flaky.Samples))
	}
}

func TestRefIDHistoryIncoherent(t *testing.T) {
	peers := ledger.Evaluate(fixture.Build(), anchorOpts())
	flaky := findPeer(peers, "flaky-campus")
	// last accepted sample is seq 6 with refid PPS; the GPS->PPS->IPv4
	// incoherence stays visible in the per-exchange evidence.
	// stratum >= 2 renders refid as dotted-quad: "PPS\0" -> 80.80.83.0
	if flaky.RefID != "80.80.83.0" {
		t.Fatalf("refid = %q, want 80.80.83.0", flaky.RefID)
	}
}
