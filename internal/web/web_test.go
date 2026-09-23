package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ntp-ledger/internal/fixture"
	"ntp-ledger/internal/store"
	"ntp-ledger/internal/web"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.Seed(st); err != nil {
		t.Fatal(err)
	}
	srv, err := web.New(st)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); st.Close() })
	return ts
}

func get(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestDashboardShowsTitle(t *testing.T) {
	ts := newServer(t)
	body := get(t, ts.URL+"/")
	if !strings.Contains(body, "时差年代账") {
		t.Fatal("dashboard missing title 时差年代账")
	}
	for _, name := range []string{"gps-stratum1", "tap32-only", "flaky-campus"} {
		if !strings.Contains(body, name) {
			t.Fatalf("dashboard missing peer %s", name)
		}
	}
}

func TestPeerPageEvidenceExpandable(t *testing.T) {
	ts := newServer(t)
	body := get(t, ts.URL+"/peer?id=3")
	for _, want := range []string{"证据", "kod", "duplicate-transmit", "negative-delay", "<svg"} {
		if !strings.Contains(body, want) {
			t.Fatalf("peer page missing %q", want)
		}
	}
}

func TestComparePage(t *testing.T) {
	ts := newServer(t)
	body := get(t, ts.URL+"/compare?anchor=1")
	for _, want := range []string{"策略对比", "strict", "permissive", "anchor: gps-2037"} {
		if !strings.Contains(body, want) {
			t.Fatalf("compare page missing %q", want)
		}
	}
}

func TestAddAnchor(t *testing.T) {
	ts := newServer(t)
	resp, err := http.PostForm(ts.URL+"/anchors", map[string][]string{
		"name": {"manual"}, "unix_sec": {"2085979000"}, "note": {"test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 { // client follows the 303 redirect
		t.Fatalf("status %d", resp.StatusCode)
	}
	body := get(t, ts.URL+"/")
	if !strings.Contains(body, "manual") {
		t.Fatal("new anchor not listed")
	}
}
