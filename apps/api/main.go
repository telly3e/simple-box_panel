package main

import (
	"flag"
	"log"
	"net/http"

	"singpanel/internal/api"
	"singpanel/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", ".runtime/sing-panel.db", "SQLite database path")
	webDir := flag.String("web-dir", "", "optional directory for built web assets")
	flag.Parse()

	s, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()

	log.Printf("API listening on http://localhost%s", *addr)
	if err := http.ListenAndServe(*addr, api.NewServerWithWeb(s, *webDir)); err != nil {
		log.Fatal(err)
	}
}
