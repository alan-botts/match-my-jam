package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
)

func newInviteToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// InviteTokenForUser returns the user's invite token, generating and
// persisting one if they don't have one yet.
func (s *Store) InviteTokenForUser(ctx context.Context, userID int64) (string, error) {
	var tok string
	err := s.DB.QueryRowContext(ctx, `SELECT invite_token FROM users WHERE id = ?`, userID).Scan(&tok)
	if err != nil {
		return "", err
	}
	if tok != "" {
		return tok, nil
	}
	// Generate with a small retry loop to avoid rare unique-index collisions.
	for i := 0; i < 5; i++ {
		tok = newInviteToken()
		_, err = s.DB.ExecContext(ctx, `UPDATE users SET invite_token = ? WHERE id = ?`, tok, userID)
		if err == nil {
			return tok, nil
		}
	}
	return "", err
}

// FindUserByInviteToken looks up the user that owns a given invite token.
func (s *Store) FindUserByInviteToken(ctx context.Context, token string) (*User, error) {
	if token == "" {
		return nil, sql.ErrNoRows
	}
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, display_name, email, avatar_url FROM users WHERE invite_token = ?`, token,
	).Scan(&u.ID, &u.DisplayName, &u.Email, &u.AvatarURL)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// AutoAcceptFriendship creates an already-accepted friendship between two
// users. Idempotent: if a friendship in either direction already exists, it
// flips it to accepted when it was pending, and is a no-op otherwise.
func (s *Store) AutoAcceptFriendship(ctx context.Context, a, b int64) error {
	if a == b {
		return errors.New("cannot friend yourself")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existingID int64
	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT id, status FROM friendships
		 WHERE (requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)`,
		a, b, b, a,
	).Scan(&existingID, &status)
	switch {
	case err == sql.ErrNoRows:
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO friendships (requester_id, addressee_id, status, responded_at)
			 VALUES (?, ?, 'accepted', CURRENT_TIMESTAMP)`,
			a, b,
		); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if status != "accepted" {
			if _, err := tx.ExecContext(ctx,
				`UPDATE friendships SET status = 'accepted', responded_at = CURRENT_TIMESTAMP WHERE id = ?`,
				existingID,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
