package analysis_test

import (
	"eraledger/internal/analysis"
	"eraledger/internal/fixture"
	"fmt"
	"testing"
)

func TestSmoke(t *testing.T) {
	cap := fixture.Build()
	a := &analysis.Analyzer{}
	res := a.Analyze(cap, analysis.DefaultSettings())
	for _, ex := range res.Exchanges {
		fmt.Printf("%-14s rej=%-5v %-28s d=%12d o=%12d dist=%d cands=%d %s\n",
			ex.ID, ex.Rejected, ex.RejectCode, ex.DelayNS, ex.OffsetNS,
			ex.RootDistanceTick64, len(ex.Candidates), ex.EraNote)
	}
	fmt.Println("--- peers ---")
	for _, p := range res.Peers {
		fmt.Printf("%s reach=%s stratum=%d best=%s dist=%d incoh=%v eras=%v\n",
			p.ID, p.ReachBinary, p.Stratum, p.BestExchange, p.BestDistance,
			p.Incoherent, p.EraCandidates)
		for _, f := range p.Filter {
			fmt.Printf("   rank=%d %s kept=%v dist=%d off=%d\n", f.Rank, f.ExchangeID, f.Kept, f.RootDistance, f.OffsetTick32)
		}
	}
	fmt.Println("selected:", res.Selected)
	for _, e := range res.Evidence {
		fmt.Println("EV", e.Scope, e.Ref, e.Code, e.Detail)
	}
}
