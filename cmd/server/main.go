// Command server runs the offline NTP era ledger UI.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"ntp-ledger/internal/fixture"
	"ntp-ledger/internal/store"
	"ntp-ledger/internal/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5995", "listen address")
	dbPath := flag.String("db", "ledger.db", "SQLite database path")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := fixture.Seed(st); err != nil {
		log.Fatalf("seed fixtures: %v", err)
	}
	srv, err := web.New(st)
	if err != nil {
		log.Fatalf("build web: %v", err)
	}
	fmt.Printf("时差年代账 listening on http://%s\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}
