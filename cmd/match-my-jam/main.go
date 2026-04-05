package main

import (
	"log"
	"net/http"
	"os"

	"github.com/alan-botts/match-my-jam/internal/config"
	"github.com/alan-botts/match-my-jam/internal/db"
	"github.com/alan-botts/match-my-jam/internal/store"
	"github.com/alan-botts/match-my-jam/internal/web"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	s := store.New(database)
	srv, err := web.New(cfg, s)
	if err != nil {
		log.Fatalf("web: %v", err)
	}

	addr := ":" + cfg.Port
	log.Printf("Match My Jam listening on %s (base=%s, db=%s)", addr, cfg.BaseURL, cfg.DBPath)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Printf("server error: %v", err)
		os.Exit(1)
	}
}
