package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

type Config struct {
	BaseURL             string
	Port                string
	DBPath              string
	SessionHashKey      []byte
	SessionBlockKey     []byte
	SpotifyClientID     string
	SpotifyClientSecret string
	GoogleClientID      string
	GoogleClientSecret  string
}

func Load() (*Config, error) {
	cfg := &Config{
		BaseURL:             env("MMJ_BASE_URL", "http://localhost:8080"),
		Port:                env("PORT", "8080"),
		DBPath:              env("MMJ_DB_PATH", "./data/mmj.db"),
		SpotifyClientID:     os.Getenv("MMJ_SPOTIFY_CLIENT_ID"),
		SpotifyClientSecret: os.Getenv("MMJ_SPOTIFY_CLIENT_SECRET"),
		GoogleClientID:      os.Getenv("MMJ_GOOGLE_CLIENT_ID"),
		GoogleClientSecret:  os.Getenv("MMJ_GOOGLE_CLIENT_SECRET"),
	}

	hashHex := os.Getenv("MMJ_SESSION_HASH_KEY")
	blockHex := os.Getenv("MMJ_SESSION_BLOCK_KEY")
	if hashHex == "" || blockHex == "" {
		return nil, errors.New("MMJ_SESSION_HASH_KEY and MMJ_SESSION_BLOCK_KEY are required (hex-encoded)")
	}
	hk, err := hex.DecodeString(hashHex)
	if err != nil {
		return nil, fmt.Errorf("decode MMJ_SESSION_HASH_KEY: %w", err)
	}
	bk, err := hex.DecodeString(blockHex)
	if err != nil {
		return nil, fmt.Errorf("decode MMJ_SESSION_BLOCK_KEY: %w", err)
	}
	cfg.SessionHashKey = hk
	cfg.SessionBlockKey = bk

	if cfg.SpotifyClientID == "" || cfg.SpotifyClientSecret == "" {
		return nil, errors.New("MMJ_SPOTIFY_CLIENT_ID and MMJ_SPOTIFY_CLIENT_SECRET are required")
	}
	return cfg, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
