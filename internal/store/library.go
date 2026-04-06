package store

import (
	"context"
	"database/sql"
	"time"
)

type Playlist struct {
	ID                 int64
	ProviderPlaylistID string
	Name               string
	OwnerName          string
	TrackCount         int
	IsPublic           bool
	IsCollaborative    bool
	ImageURL           string
}

type PlaylistTrack struct {
	Position        int
	ProviderTrackID string
	TrackName       string
	ArtistName      string
	AlbumName       string
	DurationMs      int
	Genre           string
	PreviewURL      string
	AddedAt         *time.Time
}

type LikedTrack struct {
	ProviderTrackID string
	TrackName       string
	ArtistName      string
	AlbumName       string
	DurationMs      int
	AlbumImageURL   string
	Genre           string
	PreviewURL      string
	AddedAt         *time.Time
}

type SavedAlbum struct {
	ProviderAlbumID string
	AlbumName       string
	ArtistName      string
	ImageURL        string
	AddedAt         *time.Time
}

func (s *Store) UserPlaylists(ctx context.Context, userID int64) ([]Playlist, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, provider_playlist_id, name, owner_name, track_count, is_public, is_collaborative, image_url
		 FROM playlists WHERE user_id = ? ORDER BY name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		var p Playlist
		var pub, collab int
		if err := rows.Scan(&p.ID, &p.ProviderPlaylistID, &p.Name, &p.OwnerName, &p.TrackCount, &pub, &collab, &p.ImageURL); err != nil {
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
		`SELECT id, provider_playlist_id, name, owner_name, track_count, is_public, is_collaborative, image_url
		 FROM playlists WHERE id = ? AND user_id = ?`, playlistID, userID,
	).Scan(&p.ID, &p.ProviderPlaylistID, &p.Name, &p.OwnerName, &p.TrackCount, &pub, &collab, &p.ImageURL)
	if err != nil {
		return nil, err
	}
	p.IsPublic = pub != 0
	p.IsCollaborative = collab != 0
	return &p, nil
}

func (s *Store) PlaylistTracks(ctx context.Context, playlistID int64) ([]PlaylistTrack, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT position, provider_track_id, track_name, artist_name, album_name, duration_ms, genre, preview_url, added_at
		 FROM playlist_tracks WHERE playlist_id = ? ORDER BY position`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlaylistTrack
	for rows.Next() {
		var t PlaylistTrack
		var addedAt sql.NullTime
		if err := rows.Scan(&t.Position, &t.ProviderTrackID, &t.TrackName, &t.ArtistName, &t.AlbumName, &t.DurationMs, &t.Genre, &t.PreviewURL, &addedAt); err != nil {
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
	return s.UserLikedTracksPaged(ctx, userID, -1, 0)
}

func (s *Store) UserLikedTracksPaged(ctx context.Context, userID int64, limit, offset int) ([]LikedTrack, error) {
	q := `SELECT provider_track_id, track_name, artist_name, album_name, duration_ms, album_image_url, genre, preview_url, added_at
		 FROM liked_tracks WHERE user_id = ? ORDER BY added_at DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		q += ` LIMIT ? OFFSET ?`
		rows, err = s.DB.QueryContext(ctx, q, userID, limit, offset)
	} else {
		rows, err = s.DB.QueryContext(ctx, q, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LikedTrack
	for rows.Next() {
		var t LikedTrack
		var addedAt sql.NullTime
		if err := rows.Scan(&t.ProviderTrackID, &t.TrackName, &t.ArtistName, &t.AlbumName, &t.DurationMs, &t.AlbumImageURL, &t.Genre, &t.PreviewURL, &addedAt); err != nil {
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

func (s *Store) CountLikedTracks(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM liked_tracks WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

func (s *Store) UserSavedAlbums(ctx context.Context, userID int64) ([]SavedAlbum, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT provider_album_id, album_name, artist_name, image_url, added_at
		 FROM saved_albums WHERE user_id = ? ORDER BY added_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedAlbum
	for rows.Next() {
		var a SavedAlbum
		var addedAt sql.NullTime
		if err := rows.Scan(&a.ProviderAlbumID, &a.AlbumName, &a.ArtistName, &a.ImageURL, &addedAt); err != nil {
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
