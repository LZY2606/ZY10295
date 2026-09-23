package web

import (
	"fmt"
	"html/template"
	"strings"

	"ntp-ledger/internal/ledger"
)

// peerChart renders an inline SVG of offset and delay per exchange, in
// receive order. Accepted samples are dots, rejected exchanges are crosses.
func peerChart(p *ledger.Peer) template.HTML {
	const (
		w, h = 720.0, 240.0
		padL = 70.0
		padR = 16.0
		padT = 16.0
		padB = 28.0
	)
	n := len(p.Evals)
	if n == 0 {
		return ""
	}
	// Y range over offsets and delays of all exchanges, in ms.
	lo, hi := 0.0, 0.0
	for _, ev := range p.Evals {
		for _, v := range []float64{ev.Offset.Float() * 1000, ev.Delay.Float() * 1000} {
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
	}
	if hi-lo < 1 {
		hi, lo = hi+0.5, lo-0.5
	}
	x := func(i int) float64 {
		if n == 1 {
			return padL + (w-padL-padR)/2
		}
		return padL + float64(i)*(w-padL-padR)/float64(n-1)
	}
	y := func(ms float64) float64 {
		return padT + (hi-ms)*(h-padT-padB)/(hi-lo)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="offset and delay chart">`, w, h, w, h)
	fmt.Fprintf(&b, `<rect width="%.0f" height="%.0f" fill="#f8f9fa"/>`, w, h)
	// zero line
	if lo < 0 && hi > 0 {
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#999" stroke-dasharray="4 3"/>`, padL, y(0), w-padR, y(0))
	}
	// axes labels
	fmt.Fprintf(&b, `<text x="4" y="%.1f" font-size="10" fill="#555">%.1f ms</text>`, y(hi)+3, hi)
	fmt.Fprintf(&b, `<text x="4" y="%.1f" font-size="10" fill="#555">%.1f ms</text>`, y(lo)+3, lo)
	fmt.Fprintf(&b, `<text x="4" y="%.1f" font-size="10" fill="#555">0</text>`, y(0)+3)
	// delay polyline
	var pts strings.Builder
	for i, ev := range p.Evals {
		fmt.Fprintf(&pts, "%.1f,%.1f ", x(i), y(ev.Delay.Float()*1000))
	}
	fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="#7aa5d2" stroke-width="1.5"/>`, strings.TrimSpace(pts.String()))
	// offset points
	for i, ev := range p.Evals {
		cx, cy := x(i), y(ev.Offset.Float()*1000)
		if ev.Accepted {
			fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="4" fill="#1a7f37"><title>seq %d offset %s delay %s</title></circle>`, cx, cy, ev.Ex.Seq, ev.Offset, ev.Delay)
		} else {
			fmt.Fprintf(&b, `<g stroke="#c00" stroke-width="2"><line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/><line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/><title>seq %d rejected: %s</title></g>`,
				cx-4, cy-4, cx+4, cy+4, cx-4, cy+4, cx+4, cy-4, ev.Ex.Seq, ev.Reason)
		}
	}
	fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-size="10" fill="#1a7f37">&#9679; offset (accepted)</text>`, padL, h-8)
	fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-size="10" fill="#7aa5d2">&#9472; delay</text>`, padL+130, h-8)
	fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-size="10" fill="#c00">&#10005; rejected</text>`, padL+210, h-8)
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}
