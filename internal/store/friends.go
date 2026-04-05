package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type Friendship struct {
	ID          int64
	RequesterID int64
	AddresseeID int64
	Status      string // "pending", "accepted", "declined"
	RequestedAt time.Time
	RespondedAt *time.Time
}

type FriendRequest struct {
	Friendship
	OtherUser User
}

var ErrSelfFriend = errors.New("cannot friend yourself")

// FindUserByEmail does a case-insensitive lookup.
func (s *Store) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, sql.ErrNoRows
	}
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, display_name, email, avatar_url FROM users WHERE LOWER(email) = ?`, email,
	).Scan(&u.ID, &u.DisplayName, &u.Email, &u.AvatarURL)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// SendFriendRequest creates a pending friendship. If one already exists (in
// either direction), it returns the existing row without error.
func (s *Store) SendFriendRequest(ctx context.Context, requesterID, addresseeID int64) (*Friendship, error) {
	if requesterID == addresseeID {
		return nil, ErrSelfFriend
	}
	// Check existing in either direction.
	var f Friendship
	var responded sql.NullTime
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, requester_id, addressee_id, status, requested_at, responded_at FROM friendships
		 WHERE (requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)`,
		requesterID, addresseeID, addresseeID, requesterID,
	).Scan(&f.ID, &f.RequesterID, &f.AddresseeID, &f.Status, &f.RequestedAt, &responded)
	if err == nil {
		if responded.Valid {
			t := responded.Time
			f.RespondedAt = &t
		}
		return &f, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO friendships (requester_id, addressee_id, status) VALUES (?, ?, 'pending')`,
		requesterID, addresseeID,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Friendship{ID: id, RequesterID: requesterID, AddresseeID: addresseeID, Status: "pending", RequestedAt: time.Now()}, nil
}

func (s *Store) RespondToFriendRequest(ctx context.Context, friendshipID, addresseeID int64, accept bool) error {
	status := "accepted"
	if !accept {
		status = "declined"
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE friendships SET status = ?, responded_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND addressee_id = ? AND status = 'pending'`,
		status, friendshipID, addresseeID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// IncomingRequests returns pending friend requests sent *to* userID.
func (s *Store) IncomingRequests(ctx context.Context, userID int64) ([]FriendRequest, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT f.id, f.requester_id, f.addressee_id, f.status, f.requested_at,
		        u.id, u.display_name, u.email, u.avatar_url
		 FROM friendships f
		 JOIN users u ON u.id = f.requester_id
		 WHERE f.addressee_id = ? AND f.status = 'pending'
		 ORDER BY f.requested_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FriendRequest
	for rows.Next() {
		var fr FriendRequest
		if err := rows.Scan(&fr.ID, &fr.RequesterID, &fr.AddresseeID, &fr.Status, &fr.RequestedAt,
			&fr.OtherUser.ID, &fr.OtherUser.DisplayName, &fr.OtherUser.Email, &fr.OtherUser.AvatarURL); err != nil {
			return nil, err
		}
		out = append(out, fr)
	}
	return out, rows.Err()
}

// OutgoingRequests returns pending requests userID has sent.
func (s *Store) OutgoingRequests(ctx context.Context, userID int64) ([]FriendRequest, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT f.id, f.requester_id, f.addressee_id, f.status, f.requested_at,
		        u.id, u.display_name, u.email, u.avatar_url
		 FROM friendships f
		 JOIN users u ON u.id = f.addressee_id
		 WHERE f.requester_id = ? AND f.status = 'pending'
		 ORDER BY f.requested_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FriendRequest
	for rows.Next() {
		var fr FriendRequest
		if err := rows.Scan(&fr.ID, &fr.RequesterID, &fr.AddresseeID, &fr.Status, &fr.RequestedAt,
			&fr.OtherUser.ID, &fr.OtherUser.DisplayName, &fr.OtherUser.Email, &fr.OtherUser.AvatarURL); err != nil {
			return nil, err
		}
		out = append(out, fr)
	}
	return out, rows.Err()
}

// Friends returns accepted friends of userID.
func (s *Store) Friends(ctx context.Context, userID int64) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT u.id, u.display_name, u.email, u.avatar_url FROM friendships f
		 JOIN users u ON u.id = CASE WHEN f.requester_id = ? THEN f.addressee_id ELSE f.requester_id END
		 WHERE (f.requester_id = ? OR f.addressee_id = ?) AND f.status = 'accepted'
		 ORDER BY u.display_name`,
		userID, userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.Email, &u.AvatarURL); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// AreFriends reports whether a and b have an accepted friendship.
func (s *Store) AreFriends(ctx context.Context, a, b int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM friendships
		 WHERE status = 'accepted' AND
		 ((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?))`,
		a, b, b, a,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

type TrackMatch struct {
	ProviderTrackID string
	TrackName       string
	ArtistName      string
	AlbumName       string
}

type AlbumMatch struct {
	ProviderAlbumID string
	AlbumName       string
	ArtistName      string
}

type Overlap struct {
	LikedTracks  []TrackMatch
	Albums       []AlbumMatch
	PlaylistHits []TrackMatch // tracks present in liked OR any playlist of both users
}

// ComputeOverlap returns intersecting content between userA and userB.
func (s *Store) ComputeOverlap(ctx context.Context, a, b int64) (*Overlap, error) {
	o := &Overlap{}

	// Shared liked tracks (by provider_track_id).
	rows, err := s.DB.QueryContext(ctx,
		`SELECT la.provider_track_id, la.track_name, la.artist_name, la.album_name
		 FROM liked_tracks la
		 JOIN liked_tracks lb ON la.provider_track_id = lb.provider_track_id AND la.provider = lb.provider
		 WHERE la.user_id = ? AND lb.user_id = ?
		 ORDER BY la.artist_name, la.track_name`,
		a, b,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t TrackMatch
		if err := rows.Scan(&t.ProviderTrackID, &t.TrackName, &t.ArtistName, &t.AlbumName); err != nil {
			rows.Close()
			return nil, err
		}
		o.LikedTracks = append(o.LikedTracks, t)
	}
	rows.Close()

	// Shared saved albums.
	rows, err = s.DB.QueryContext(ctx,
		`SELECT sa.provider_album_id, sa.album_name, sa.artist_name
		 FROM saved_albums sa
		 JOIN saved_albums sb ON sa.provider_album_id = sb.provider_album_id AND sa.provider = sb.provider
		 WHERE sa.user_id = ? AND sb.user_id = ?
		 ORDER BY sa.artist_name, sa.album_name`,
		a, b,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var al AlbumMatch
		if err := rows.Scan(&al.ProviderAlbumID, &al.AlbumName, &al.ArtistName); err != nil {
			rows.Close()
			return nil, err
		}
		o.Albums = append(o.Albums, al)
	}
	rows.Close()

	// Tracks present in any playlist of both users (dedup by provider_track_id).
	rows, err = s.DB.QueryContext(ctx,
		`SELECT DISTINCT ta.provider_track_id, ta.track_name, ta.artist_name, ta.album_name
		 FROM playlist_tracks ta
		 JOIN playlists pa ON pa.id = ta.playlist_id AND pa.user_id = ?
		 JOIN playlist_tracks tb ON tb.provider_track_id = ta.provider_track_id
		 JOIN playlists pb ON pb.id = tb.playlist_id AND pb.user_id = ?
		 WHERE ta.provider_track_id != ''
		 ORDER BY ta.artist_name, ta.track_name
		 LIMIT 2000`,
		a, b,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t TrackMatch
		if err := rows.Scan(&t.ProviderTrackID, &t.TrackName, &t.ArtistName, &t.AlbumName); err != nil {
			rows.Close()
			return nil, err
		}
		o.PlaylistHits = append(o.PlaylistHits, t)
	}
	rows.Close()

	return o, nil
}

// AuthAccounts lists all linked provider accounts for a user.
func (s *Store) AuthAccounts(ctx context.Context, userID int64) ([]AuthAccount, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, user_id, provider, provider_user_id, provider_email FROM auth_accounts WHERE user_id = ? ORDER BY provider`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuthAccount
	for rows.Next() {
		var a AuthAccount
		if err := rows.Scan(&a.ID, &a.UserID, &a.Provider, &a.ProviderUserID, &a.ProviderEmail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAuthAccount(ctx context.Context, userID, acctID int64) error {
	// Refuse if this would leave the user with zero linked accounts.
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_accounts WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return err
	}
	if n <= 1 {
		return errors.New("cannot disconnect your last sign-in method")
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM auth_accounts WHERE id = ? AND user_id = ?`, acctID, userID)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	return err
}
