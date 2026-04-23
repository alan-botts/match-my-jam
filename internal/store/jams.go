package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type JamSession struct {
	ID        int64
	OwnerID   int64
	Name      string
	Token     string
	CreatedAt time.Time
	Owner     User
	Members   []JamMember
}

type JamMember struct {
	User     User
	JoinedAt time.Time
}

type JamPairOverlap struct {
	A       User
	B       User
	Overlap *Overlap
}

func (s *Store) CreateJamSession(ctx context.Context, ownerID int64, name string) (*JamSession, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "New jam"
	}
	for i := 0; i < 5; i++ {
		token := newInviteToken()
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO jam_sessions (owner_id, name, token) VALUES (?, ?, ?)`,
			ownerID, name, token,
		)
		if err != nil {
			tx.Rollback()
			continue
		}
		id, _ := res.LastInsertId()
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO jam_members (jam_id, user_id) VALUES (?, ?)`, id, ownerID,
		); err != nil {
			tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return s.GetJamSession(ctx, id)
	}
	return nil, sql.ErrNoRows
}

func (s *Store) GetJamSession(ctx context.Context, id int64) (*JamSession, error) {
	var j JamSession
	err := s.DB.QueryRowContext(ctx,
		`SELECT j.id, j.owner_id, j.name, j.token, j.created_at,
		        u.id, u.display_name, u.email, u.avatar_url
		 FROM jam_sessions j JOIN users u ON u.id = j.owner_id WHERE j.id = ?`, id,
	).Scan(&j.ID, &j.OwnerID, &j.Name, &j.Token, &j.CreatedAt,
		&j.Owner.ID, &j.Owner.DisplayName, &j.Owner.Email, &j.Owner.AvatarURL)
	if err != nil {
		return nil, err
	}
	members, err := s.JamMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	j.Members = members
	return &j, nil
}

func (s *Store) FindJamByToken(ctx context.Context, token string) (*JamSession, error) {
	if token == "" {
		return nil, sql.ErrNoRows
	}
	var id int64
	if err := s.DB.QueryRowContext(ctx, `SELECT id FROM jam_sessions WHERE token = ?`, token).Scan(&id); err != nil {
		return nil, err
	}
	return s.GetJamSession(ctx, id)
}

func (s *Store) UserJamSessions(ctx context.Context, userID int64) ([]JamSession, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT j.id, j.owner_id, j.name, j.token, j.created_at,
		        u.id, u.display_name, u.email, u.avatar_url,
		        COUNT(m2.user_id) AS member_count
		 FROM jam_sessions j
		 JOIN jam_members m ON m.jam_id = j.id AND m.user_id = ?
		 JOIN users u ON u.id = j.owner_id
		 LEFT JOIN jam_members m2 ON m2.jam_id = j.id
		 GROUP BY j.id, j.owner_id, j.name, j.token, j.created_at, u.id, u.display_name, u.email, u.avatar_url
		 ORDER BY j.created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JamSession
	for rows.Next() {
		var j JamSession
		var memberCount int
		if err := rows.Scan(&j.ID, &j.OwnerID, &j.Name, &j.Token, &j.CreatedAt,
			&j.Owner.ID, &j.Owner.DisplayName, &j.Owner.Email, &j.Owner.AvatarURL, &memberCount); err != nil {
			return nil, err
		}
		j.Members = make([]JamMember, memberCount)
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) JamMembers(ctx context.Context, jamID int64) ([]JamMember, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT u.id, u.display_name, u.email, u.avatar_url, m.joined_at
		 FROM jam_members m JOIN users u ON u.id = m.user_id
		 WHERE m.jam_id = ? ORDER BY m.joined_at ASC`, jamID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JamMember
	for rows.Next() {
		var m JamMember
		if err := rows.Scan(&m.User.ID, &m.User.DisplayName, &m.User.Email, &m.User.AvatarURL, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) AddJamMember(ctx context.Context, jamID, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO jam_members (jam_id, user_id) VALUES (?, ?)`, jamID, userID)
	return err
}

func (s *Store) UserInJam(ctx context.Context, jamID, userID int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM jam_members WHERE jam_id = ? AND user_id = ?`, jamID, userID).Scan(&n)
	return n > 0, err
}

func (s *Store) JamPairOverlaps(ctx context.Context, jamID int64) ([]JamPairOverlap, error) {
	members, err := s.JamMembers(ctx, jamID)
	if err != nil {
		return nil, err
	}
	var out []JamPairOverlap
	for i := 0; i < len(members); i++ {
		for k := i + 1; k < len(members); k++ {
			o, err := s.ComputeOverlap(ctx, members[i].User.ID, members[k].User.ID)
			if err != nil {
				return nil, err
			}
			out = append(out, JamPairOverlap{A: members[i].User, B: members[k].User, Overlap: o})
		}
	}
	return out, nil
}
