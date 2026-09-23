package ledger

import (
	"fmt"

	"ntp-ledger/internal/ntp"
)

// FilterNames lists the filter pipeline in evaluation order. Every
// filter runs for every exchange and records a verdict, so the UI can
// expand the full evidence trail; the first rejection is the primary
// reason.
var FilterNames = []string{
	"kod",
	"li-unsync",
	"origin-mismatch",
	"duplicate-transmit",
	"late-response",
	"era-unresolvable",
	"precision-bounds",
	"negative-delay",
	"leap-announced",
}

// peerFilter carries per-peer state across exchanges in recv order.
type peerFilter struct {
	seenTx    map[ntp.Timestamp]bool // server transmit timestamps already seen
	maxSeqRsp int                    // highest request seq with a response already processed
	started   bool
}

func runFilters(ex *Exchange, ev *Eval, opts Options, st *peerFilter) []FilterVerdict {
	verdicts := make([]FilterVerdict, 0, len(FilterNames))
	add := func(name string, reject bool, detail string) {
		verdicts = append(verdicts, FilterVerdict{Name: name, Reject: reject, Detail: detail})
	}

	// 1. kiss-o'-death: stratum 0 server response.
	if ex.Resp.IsKod() {
		add("kod", true, fmt.Sprintf("kiss code %q", ex.Resp.KissCode()))
	} else {
		add("kod", false, "")
	}

	// 2. LI=3: server unsynchronized.
	if ex.Resp.LI == ntp.LIUnsynced {
		add("li-unsync", true, "leap indicator 3: server clock unsynchronized")
	} else {
		add("li-unsync", false, "")
	}

	// 3. originate timestamp must echo our request transmit timestamp.
	if ex.Resp.Orig != ex.T1 {
		add("origin-mismatch", true, fmt.Sprintf(
			"originate %08x.%08x != request transmit %08x.%08x",
			ex.Resp.Orig.Sec, ex.Resp.Orig.Frac, ex.T1.Sec, ex.T1.Frac))
	} else {
		add("origin-mismatch", false, "")
	}

	// 4. duplicate server transmit timestamp (replay or retransmit).
	if st.seenTx[ex.Resp.Tx] {
		add("duplicate-transmit", true, fmt.Sprintf(
			"transmit %08x.%08x already seen from this peer", ex.Resp.Tx.Sec, ex.Resp.Tx.Frac))
	} else {
		add("duplicate-transmit", false, "")
	}

	// 5. late response: a newer request was already answered.
	if st.started && ex.Seq < st.maxSeqRsp {
		add("late-response", true, fmt.Sprintf(
			"response for seq %d arrived after seq %d", ex.Seq, st.maxSeqRsp))
	} else {
		add("late-response", false, "")
	}

	// 6. era must be resolvable to at least one candidate.
	if len(ev.EraCands) == 0 {
		add("era-unresolvable", true, "no era places the exchange within half an era of its reference")
	} else {
		add("era-unresolvable", false, "")
	}

	// 7. precision exponent bounds.
	if !ntp.PrecisionValid(ex.Resp.Precision) {
		add("precision-bounds", true, fmt.Sprintf(
			"precision exponent %d outside [-32, 24]", ex.Resp.Precision))
	} else {
		add("precision-bounds", false, "")
	}

	// 8. negative round-trip delay is physically impossible.
	if ev.Delay.Sign() < 0 {
		add("negative-delay", true, fmt.Sprintf("delay %s", ev.Delay))
	} else {
		add("negative-delay", false, "")
	}

	// 9. leap announcement handling per policy. The leap indicator never
	// shifts any timestamp; strict policy only excludes the sample.
	leapAnnounced := ex.Resp.LI == ntp.LILast61 || ex.Resp.LI == ntp.LILast59
	switch {
	case leapAnnounced && opts.Leap == LeapStrict:
		add("leap-announced", true, fmt.Sprintf("LI=%d leap announced, strict policy excludes sample", ex.Resp.LI))
	case leapAnnounced:
		add("leap-announced", false, fmt.Sprintf("LI=%d leap announced, kept by %s policy", ex.Resp.LI, opts.Leap))
	default:
		add("leap-announced", false, "")
	}

	// Update per-peer state: a non-KoD response counts its transmit
	// timestamp and advances the responded-sequence watermark.
	if !ex.Resp.IsKod() {
		st.seenTx[ex.Resp.Tx] = true
		if !st.started || ex.Seq > st.maxSeqRsp {
			st.maxSeqRsp = ex.Seq
		}
		st.started = true
	}
	return verdicts
}
