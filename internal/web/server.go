// Package web serves the offline era ledger UI: dashboard, per-peer
// exchange detail with expandable filter evidence, and a comparison
// view across anchors and leap policies. Charts are inline SVG.
package web

import (
	"html/template"
	"net/http"
	"strconv"

	"ntp-ledger/internal/ledger"
	"ntp-ledger/internal/ntp"
	"ntp-ledger/internal/store"
)

// Server is the ledger web UI.
type Server struct {
	st  *store.Store
	mux *http.ServeMux
	tpl *template.Template
}

// New builds the HTTP handler for the ledger UI.
func New(st *store.Store) (*Server, error) {
	tpl, err := template.New("all").Funcs(template.FuncMap{
		"fixed":   func(f ntp.Fixed) string { return f.String() },
		"fixedms": func(f ntp.Fixed) string { return strconv.FormatFloat(f.Float()*1000, 'f', 3, 64) },
		"hex32":   func(v uint32) string { return strconv.FormatUint(uint64(v), 16) },
		"rawhex":  rawHex,
	}).Parse(pagesTpl)
	if err != nil {
		return nil, err
	}
	s := &Server{st: st, mux: http.NewServeMux(), tpl: tpl}
	s.mux.HandleFunc("/", s.handleDash)
	s.mux.HandleFunc("/peer", s.handlePeer)
	s.mux.HandleFunc("/compare", s.handleCompare)
	s.mux.HandleFunc("/anchors", s.handleAnchors)
	return s, nil
}

// Handler returns the root handler.
func (s *Server) Handler() http.Handler { return s.mux }

// evalOptions resolves request parameters into ledger options.
func (s *Server) evalOptions(r *http.Request) (ledger.Options, []store.Anchor, error) {
	anchors, err := s.st.Anchors()
	if err != nil {
		return ledger.Options{}, nil, err
	}
	leap, err := ledger.ParseLeapPolicy(r.URL.Query().Get("leap"))
	if err != nil {
		leap = ledger.LeapPermissive
	}
	opts := ledger.Options{Leap: leap, MaxEra: 1}
	if id := r.URL.Query().Get("anchor"); id != "" {
		for _, a := range anchors {
			if strconv.FormatInt(a.ID, 10) == id {
				abs := a.Abs()
				opts.Anchor = &abs
			}
		}
	}
	return opts, anchors, nil
}

func (s *Server) loadPeers(opts ledger.Options) ([]*ledger.Peer, error) {
	inputs, err := s.st.LoadPeers()
	if err != nil {
		return nil, err
	}
	return ledger.Evaluate(inputs, opts), nil
}

func (s *Server) handleDash(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	opts, anchors, err := s.evalOptions(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	peers, err := s.loadPeers(opts)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, "dash", map[string]any{
		"Peers": peers, "Anchors": anchors, "Opts": opts,
		"AnchorID": r.URL.Query().Get("anchor"), "Leap": opts.Leap.String(),
	})
}

func (s *Server) handlePeer(w http.ResponseWriter, r *http.Request) {
	opts, anchors, err := s.evalOptions(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	peers, err := s.loadPeers(opts)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	var peer *ledger.Peer
	for _, p := range peers {
		if p.ID == id {
			peer = p
		}
	}
	if peer == nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "peer", map[string]any{
		"Peer": peer, "Anchors": anchors, "Opts": opts,
		"AnchorID": r.URL.Query().Get("anchor"), "Leap": opts.Leap.String(),
		"Chart": peerChart(peer), "PeerID": strconv.FormatInt(peer.ID, 10),
	})
}

// policyRun is one evaluated variant in the comparison view.
type policyRun struct {
	Label  string
	Anchor string
	Leap   string
	Peers  []*ledger.Peer
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	anchors, err := s.st.Anchors()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	inputs, err := s.st.LoadPeers()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	anchorID := r.URL.Query().Get("anchor")
	var anchor *ntp.Abs
	anchorName := "(none)"
	for _, a := range anchors {
		if strconv.FormatInt(a.ID, 10) == anchorID {
			abs := a.Abs()
			anchor = &abs
			anchorName = a.Name
		}
	}
	var runs []policyRun
	for _, leap := range []ledger.LeapPolicy{ledger.LeapStrict, ledger.LeapPermissive, ledger.LeapIgnore} {
		for _, useAnchor := range []bool{false, true} {
			opts := ledger.Options{Leap: leap, MaxEra: 1}
			label := "no anchor"
			if useAnchor {
				if anchor == nil {
					continue
				}
				opts.Anchor = anchor
				label = "anchor: " + anchorName
			}
			runs = append(runs, policyRun{
				Label: label, Anchor: label, Leap: leap.String(),
				Peers: ledger.Evaluate(inputs, opts),
			})
		}
	}
	s.render(w, "compare", map[string]any{
		"Runs": runs, "Anchors": anchors, "AnchorID": anchorID,
	})
}

func (s *Server) handleAnchors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	sec, err := strconv.ParseInt(r.Form.Get("unix_sec"), 10, 64)
	if err != nil {
		http.Error(w, "invalid unix_sec", 400)
		return
	}
	if err := s.st.AddAnchor(r.Form.Get("name"), sec, r.Form.Get("note")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func rawHex(b []byte) string {
	out := make([]byte, 0, len(b)*3)
	for i, c := range b {
		if i > 0 {
			out = append(out, ' ')
		}
		const hexd = "0123456789abcdef"
		out = append(out, hexd[c>>4], hexd[c&0xf])
	}
	return string(out)
}
