package store

import (
	"context"
	"database/sql"
	"time"
)

type User struct {
	ID          int64
	DisplayName string
	Email       string
	AvatarURL   string
}

type AuthAccount struct {
	ID             int64
	UserID         int64
	Provider       string
	ProviderUserID string
	ProviderEmail  string
	AccessToken    string
	RefreshToken   string
	TokenType      string
	ExpiresAt      *time.Time
}

type Store struct{ DB *sql.DB }

func New(db *sql.DB) *Store { return &Store{DB: db} }

func (s *Store) FindOrCreateUserByProvider(ctx context.Context, provider, providerUserID, email, displayName, avatar string) (*User, *AuthAccount, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var acct AuthAccount
	row := tx.QueryRowContext(ctx,
		`SELECT id, user_id, provider, provider_user_id, provider_email FROM auth_accounts WHERE provider = ? AND provider_user_id = ?`,
		provider, providerUserID,
	)
	err = row.Scan(&acct.ID, &acct.UserID, &acct.Provider, &acct.ProviderUserID, &acct.ProviderEmail)

	var userID int64
	switch {
	case err == sql.ErrNoRows:
		// Try account linking by email.
		if email != "" {
			_ = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE email = ?`, email).Scan(&userID)
		}
		if userID == 0 {
			res, ierr := tx.ExecContext(ctx,
				`INSERT INTO users (display_name, email, avatar_url) VALUES (?, ?, ?)`,
				displayName, email, avatar,
			)
			if ierr != nil {
				return nil, nil, ierr
			}
			userID, _ = res.LastInsertId()
		} else {
			if _, uerr := tx.ExecContext(ctx,
				`UPDATE users SET display_name = CASE WHEN display_name='' THEN ? ELSE display_name END,
				avatar_url = CASE WHEN avatar_url='' THEN ? ELSE avatar_url END,
				updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				displayName, avatar, userID,
			); uerr != nil {
				return nil, nil, uerr
			}
		}
		res, ierr := tx.ExecContext(ctx,
			`INSERT INTO auth_accounts (user_id, provider, provider_user_id, provider_email) VALUES (?, ?, ?, ?)`,
			userID, provider, providerUserID, email,
		)
		if ierr != nil {
			return nil, nil, ierr
		}
		acct.ID, _ = res.LastInsertId()
		acct.UserID = userID
		acct.Provider = provider
		acct.ProviderUserID = providerUserID
		acct.ProviderEmail = email
	case err != nil:
		return nil, nil, err
	default:
		userID = acct.UserID
		if _, uerr := tx.ExecContext(ctx,
			`UPDATE users SET display_name = CASE WHEN ? != '' THEN ? ELSE display_name END,
			avatar_url = CASE WHEN ? != '' THEN ? ELSE avatar_url END,
			updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			displayName, displayName, avatar, avatar, userID,
		); uerr != nil {
			return nil, nil, uerr
		}
	}

	u := &User{ID: userID, DisplayName: displayName, Email: email, AvatarURL: avatar}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return u, &acct, nil
}

func (s *Store) SaveToken(ctx context.Context, acctID int64, accessToken, refreshToken, tokenType string, expiresAt time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE auth_accounts SET access_token = ?, refresh_token = CASE WHEN ? != '' THEN ? ELSE refresh_token END,
		token_type = ?, expires_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		accessToken, refreshToken, refreshToken, tokenType, expiresAt, acctID,
	)
	return err
}

func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, display_name, email, avatar_url FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.DisplayName, &u.Email, &u.AvatarURL)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetAuthAccount(ctx context.Context, userID int64, provider string) (*AuthAccount, error) {
	var a AuthAccount
	var expires sql.NullTime
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, user_id, provider, provider_user_id, provider_email, access_token, refresh_token, token_type, expires_at
		 FROM auth_accounts WHERE user_id = ? AND provider = ?`, userID, provider,
	).Scan(&a.ID, &a.UserID, &a.Provider, &a.ProviderUserID, &a.ProviderEmail, &a.AccessToken, &a.RefreshToken, &a.TokenType, &expires)
	if err != nil {
		return nil, err
	}
	if expires.Valid {
		t := expires.Time
		a.ExpiresAt = &t
	}
	return &a, nil
}

func (s *Store) StartSyncRun(ctx context.Context, userID int64, provider string) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO sync_runs (user_id, provider, status) VALUES (?, ?, 'running')`,
		userID, provider,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishSyncRun(ctx context.Context, runID int64, status, errMsg string, playlists, liked, albums int) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE sync_runs SET finished_at = CURRENT_TIMESTAMP, status = ?, error = ?, playlists_synced = ?, liked_synced = ?, albums_synced = ? WHERE id = ?`,
		status, errMsg, playlists, liked, albums, runID,
	)
	return err
}

type SyncRunSummary struct {
	ID              int64
	StartedAt       time.Time
	FinishedAt      *time.Time
	Status          string
	PlaylistsSynced int
	LikedSynced     int
	AlbumsSynced    int
	Error           string
}

func (s *Store) LatestSyncRun(ctx context.Context, userID int64, provider string) (*SyncRunSummary, error) {
	var r SyncRunSummary
	var finished sql.NullTime
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, started_at, finished_at, status, playlists_synced, liked_synced, albums_synced, error
		 FROM sync_runs WHERE user_id = ? AND provider = ? ORDER BY started_at DESC LIMIT 1`,
		userID, provider,
	).Scan(&r.ID, &r.StartedAt, &finished, &r.Status, &r.PlaylistsSynced, &r.LikedSynced, &r.AlbumsSynced, &r.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if finished.Valid {
		t := finished.Time
		r.FinishedAt = &t
	}
	return &r, nil
}

type Stats struct {
	Playlists int
	Liked     int
	Albums    int
}

func (s *Store) UserStats(ctx context.Context, userID int64) (Stats, error) {
	var st Stats
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlists WHERE user_id = ?`, userID).Scan(&st.Playlists); err != nil {
		return st, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM liked_tracks WHERE user_id = ?`, userID).Scan(&st.Liked); err != nil {
		return st, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM saved_albums WHERE user_id = ?`, userID).Scan(&st.Albums); err != nil {
		return st, err
	}
	return st, nil
}
