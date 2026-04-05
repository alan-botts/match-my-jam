package store

import (
	"context"
	"database/sql"
	"time"
)

type Playlist struct {
	ID              int64
	Name            string
	OwnerName       string
	TrackCount      int
	IsPublic        bool
	IsCollaborative bool
}

type PlaylistTrack struct {
	Position   int
	TrackName  string
	ArtistName string
	AlbumName  string
	DurationMs int
	AddedAt    *time.Time
}

type LikedTrack struct {
	TrackName  string
	ArtistName string
	AlbumName  string
	DurationMs int
	AddedAt    *time.Time
}

type SavedAlbum struct {
	AlbumName  string
	ArtistName string
	AddedAt    *time.Time
}

func (s *Store) UserPlaylists(ctx context.Context, userID int64) ([]Playlist, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, name, owner_name, track_count, is_public, is_collaborative
		 FROM playlists WHERE user_id = ? ORDER BY name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		var p Playlist
		var pub, collab int
		if err := rows.Scan(&p.ID, &p.Name, &p.OwnerName, &p.TrackCount, &pub, &collab); err != nil {
			return nil, err
		}
		p.IsPublic = pub != 0
		p.IsCollaborative = collab != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PlaylistByID(ctx context.Context, playlistID, userID int64) (*Playlist, error) {
	var p Playlist
	var pub, collab int
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, name, owner_name, track_count, is_public, is_collaborative
		 FROM playlists WHERE id = ? AND user_id = ?`, playlistID, userID,
	).Scan(&p.ID, &p.Name, &p.OwnerName, &p.TrackCount, &pub, &collab)
	if err != nil {
		return nil, err
	}
	p.IsPublic = pub != 0
	p.IsCollaborative = collab != 0
	return &p, nil
}

func (s *Store) PlaylistTracks(ctx context.Context, playlistID int64) ([]PlaylistTrack, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT position, track_name, artist_name, album_name, duration_ms, added_at
		 FROM playlist_tracks WHERE playlist_id = ? ORDER BY position`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlaylistTrack
	for rows.Next() {
		var t PlaylistTrack
		var addedAt sql.NullTime
		if err := rows.Scan(&t.Position, &t.TrackName, &t.ArtistName, &t.AlbumName, &t.DurationMs, &addedAt); err != nil {
			return nil, err
		}
		if addedAt.Valid {
			v := addedAt.Time
			t.AddedAt = &v
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) UserLikedTracks(ctx context.Context, userID int64) ([]LikedTrack, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT track_name, artist_name, album_name, duration_ms, added_at
		 FROM liked_tracks WHERE user_id = ? ORDER BY added_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LikedTrack
	for rows.Next() {
		var t LikedTrack
		var addedAt sql.NullTime
		if err := rows.Scan(&t.TrackName, &t.ArtistName, &t.AlbumName, &t.DurationMs, &addedAt); err != nil {
			return nil, err
		}
		if addedAt.Valid {
			v := addedAt.Time
			t.AddedAt = &v
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) UserSavedAlbums(ctx context.Context, userID int64) ([]SavedAlbum, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT album_name, artist_name, added_at
		 FROM saved_albums WHERE user_id = ? ORDER BY added_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedAlbum
	for rows.Next() {
		var a SavedAlbum
		var addedAt sql.NullTime
		if err := rows.Scan(&a.AlbumName, &a.ArtistName, &addedAt); err != nil {
			return nil, err
		}
		if addedAt.Valid {
			v := addedAt.Time
			a.AddedAt = &v
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
