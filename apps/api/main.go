package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"singpanel/internal/api"
	"singpanel/internal/store"
)

func main() {
	addr := flag.String("addr", envDefault("SING_PANEL_ADDR", ":8080"), "HTTP listen address")
	dbPath := flag.String("db", envDefault("SING_PANEL_DB", ".runtime/sing-panel.db"), "SQLite database path")
	webDir := flag.String("web-dir", envDefault("SING_PANEL_WEB_DIR", ""), "optional directory for built web assets")
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

func envDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
