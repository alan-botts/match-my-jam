package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir db dir: %w", err)
		}
	}
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(); err != nil {
		return nil, err
	}
	if err := migrate(conn); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return conn, nil
}

func migrate(conn *sql.DB) error {
	// Run backfill ALTERs first so subsequent CREATE INDEX / table code
	// can rely on the columns existing, even on databases migrated from
	// an older schema.
	if _, err := conn.Exec(`ALTER TABLE users ADD COLUMN invite_token TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") && !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("alter users add invite_token: %w", err)
		}
	}
	if _, err := conn.Exec(`ALTER TABLE playlists ADD COLUMN image_url TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") && !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("alter playlists add image_url: %w", err)
		}
	}
	if _, err := conn.Exec(`ALTER TABLE liked_tracks ADD COLUMN album_image_url TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") && !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("alter liked_tracks add album_image_url: %w", err)
		}
	}
	if _, err := conn.Exec(`ALTER TABLE saved_albums ADD COLUMN image_url TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") && !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("alter saved_albums add image_url: %w", err)
		}
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			display_name TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
			invite_token TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email != ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_invite_token ON users(invite_token) WHERE invite_token != ''`,

		`CREATE TABLE IF NOT EXISTS auth_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			provider TEXT NOT NULL,
			provider_user_id TEXT NOT NULL,
			provider_email TEXT NOT NULL DEFAULT '',
			access_token TEXT NOT NULL DEFAULT '',
			refresh_token TEXT NOT NULL DEFAULT '',
			token_type TEXT NOT NULL DEFAULT '',
			expires_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(provider, provider_user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_accounts_user ON auth_accounts(user_id)`,

		`CREATE TABLE IF NOT EXISTS playlists (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			provider TEXT NOT NULL,
			provider_playlist_id TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			owner_name TEXT NOT NULL DEFAULT '',
			track_count INTEGER NOT NULL DEFAULT 0,
			is_public INTEGER NOT NULL DEFAULT 0,
			is_collaborative INTEGER NOT NULL DEFAULT 0,
			snapshot_id TEXT NOT NULL DEFAULT '',
			image_url TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, provider, provider_playlist_id)
		)`,

		`CREATE TABLE IF NOT EXISTS playlist_tracks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
			position INTEGER NOT NULL,
			provider_track_id TEXT NOT NULL,
			track_name TEXT NOT NULL DEFAULT '',
			artist_name TEXT NOT NULL DEFAULT '',
			album_name TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			added_at TIMESTAMP,
			UNIQUE(playlist_id, position)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_playlist_tracks_track ON playlist_tracks(provider_track_id)`,

		`CREATE TABLE IF NOT EXISTS liked_tracks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			provider TEXT NOT NULL,
			provider_track_id TEXT NOT NULL,
			track_name TEXT NOT NULL DEFAULT '',
			artist_name TEXT NOT NULL DEFAULT '',
			album_name TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			album_image_url TEXT NOT NULL DEFAULT '',
			added_at TIMESTAMP,
			UNIQUE(user_id, provider, provider_track_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_liked_tracks_track ON liked_tracks(provider_track_id)`,

		`CREATE TABLE IF NOT EXISTS saved_albums (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			provider TEXT NOT NULL,
			provider_album_id TEXT NOT NULL,
			album_name TEXT NOT NULL DEFAULT '',
			artist_name TEXT NOT NULL DEFAULT '',
			image_url TEXT NOT NULL DEFAULT '',
			added_at TIMESTAMP,
			UNIQUE(user_id, provider, provider_album_id)
		)`,

		`CREATE TABLE IF NOT EXISTS friendships (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			requester_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			addressee_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			status TEXT NOT NULL DEFAULT 'pending',
			requested_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			responded_at TIMESTAMP,
			UNIQUE(requester_id, addressee_id),
			CHECK(requester_id != addressee_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_friendships_addressee ON friendships(addressee_id, status)`,

		`CREATE TABLE IF NOT EXISTS sync_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			provider TEXT NOT NULL,
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			finished_at TIMESTAMP,
			status TEXT NOT NULL DEFAULT 'running',
			playlists_synced INTEGER NOT NULL DEFAULT 0,
			liked_synced INTEGER NOT NULL DEFAULT 0,
			albums_synced INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_runs_user ON sync_runs(user_id, started_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(s); err != nil {
			return fmt.Errorf("exec %q: %w", s[:40], err)
		}
	}
	return nil
}
