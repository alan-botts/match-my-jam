package sync

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/alan-botts/match-my-jam/internal/spotify"
	"github.com/alan-botts/match-my-jam/internal/store"
	"golang.org/x/oauth2"
)

type SpotifySyncer struct {
	Store     *store.Store
	OAuthConf *oauth2.Config

	// playlistTrackArtists maps provider_track_id -> first artist ID
	// so we can backfill genres after fetching artist details.
	mu                   sync.Mutex
	playlistTrackArtists map[string]string
}

func (s *SpotifySyncer) Run(ctx context.Context, userID int64) error {
	runID, err := s.Store.StartSyncRun(ctx, userID, spotify.Provider)
	if err != nil {
		return fmt.Errorf("start sync run: %w", err)
	}
	var p, l, a int
	err = s.doRun(ctx, userID, &p, &l, &a)
	if err != nil {
		_ = s.Store.FinishSyncRun(ctx, runID, "error", err.Error(), p, l, a)
		return err
	}
	return s.Store.FinishSyncRun(ctx, runID, "ok", "", p, l, a)
}

func (s *SpotifySyncer) doRun(ctx context.Context, userID int64, playlistCount, likedCount, albumCount *int) error {
	acct, err := s.Store.GetAuthAccount(ctx, userID, spotify.Provider)
	if err != nil {
		return fmt.Errorf("get auth account: %w", err)
	}
	tok := &oauth2.Token{
		AccessToken:  acct.AccessToken,
		RefreshToken: acct.RefreshToken,
		TokenType:    acct.TokenType,
	}
	if acct.ExpiresAt != nil {
		tok.Expiry = *acct.ExpiresAt
	}

	base := s.OAuthConf.TokenSource(ctx, tok)
	ts := &persistingTokenSource{base: base, store: s.Store, acctID: acct.ID, last: *tok}
	httpClient := oauth2.NewClient(ctx, ts)
	client := spotify.NewClientFromHTTP(httpClient)

	// Collect all unique artist IDs across liked tracks and playlist tracks
	// so we can batch-fetch genres afterwards.
	artistIDSet := make(map[string]bool)

	pls, err := client.AllPlaylists(ctx)
	if err != nil {
		return fmt.Errorf("fetch playlists: %w", err)
	}
	if err := s.upsertPlaylists(ctx, userID, pls, client, artistIDSet); err != nil {
		return fmt.Errorf("upsert playlists: %w", err)
	}
	*playlistCount = len(pls)

	var likedTracks []spotify.SavedTrack
	likedTracks, err = client.LikedTracks(ctx)
	if err != nil {
		if isAccessError(err) {
			log.Printf("liked tracks unavailable (quota): %v", err)
		} else {
			return fmt.Errorf("fetch liked tracks: %w", err)
		}
	} else {
		for _, it := range likedTracks {
			for _, a := range it.Track.Artists {
				if a.ID != "" {
					artistIDSet[a.ID] = true
				}
			}
		}
		*likedCount = len(likedTracks)
	}

	savedAlbums, err := client.SavedAlbums(ctx)
	if err != nil {
		if isAccessError(err) {
			log.Printf("saved albums unavailable (quota): %v", err)
		} else {
			return fmt.Errorf("fetch saved albums: %w", err)
		}
	} else {
		if err := s.replaceAlbums(ctx, userID, savedAlbums); err != nil {
			return fmt.Errorf("replace albums: %w", err)
		}
		*albumCount = len(savedAlbums)
	}

	// Batch-fetch artist genres and store in artists table.
	artistGenres := s.fetchAndStoreArtists(ctx, client, artistIDSet)

	// Now store liked tracks (needs genre map).
	if likedTracks != nil {
		if err := s.replaceLiked(ctx, userID, likedTracks, artistGenres); err != nil {
			return fmt.Errorf("replace liked: %w", err)
		}
	}

	// Backfill genres on playlist tracks that were already inserted.
	s.backfillPlaylistGenres(ctx, artistGenres)

	return nil
}

// fetchAndStoreArtists fetches full artist details in batches and upserts them
// into the artists table. Returns a map of artist ID -> comma-separated genres.
func (s *SpotifySyncer) fetchAndStoreArtists(ctx context.Context, client *spotify.Client, idSet map[string]bool) map[string]string {
	genres := make(map[string]string)
	if len(idSet) == 0 {
		return genres
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	artists, err := client.Artists(ctx, ids)
	if err != nil {
		if isAccessError(err) {
			log.Printf("artist genres unavailable (quota): %v", err)
		} else {
			log.Printf("fetch artists for genres: %v", err)
		}
		return genres
	}
	for _, a := range artists {
		g := ""
		if len(a.Genres) > 0 {
			limit := len(a.Genres)
			if limit > 3 {
				limit = 3
			}
			g = strings.Join(a.Genres[:limit], ", ")
		}
		genres[a.ID] = g
		// Upsert into artists table.
		_, _ = s.Store.DB.ExecContext(ctx,
			`INSERT INTO artists (provider, provider_artist_id, name, genres)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(provider, provider_artist_id) DO UPDATE SET name=excluded.name, genres=excluded.genres`,
			spotify.Provider, a.ID, a.Name, g,
		)
	}
	return genres
}

// backfillPlaylistGenres updates genre on playlist_tracks rows that were
// inserted before artist genres were available. Uses the track-to-artist
// mapping collected during replacePlaylistTracks.
func (s *SpotifySyncer) backfillPlaylistGenres(ctx context.Context, artistGenres map[string]string) {
	if len(artistGenres) == 0 {
		return
	}
	s.mu.Lock()
	trackArtists := s.playlistTrackArtists
	s.playlistTrackArtists = nil
	s.mu.Unlock()
	if trackArtists == nil {
		return
	}
	for trackID, artistID := range trackArtists {
		g := artistGenres[artistID]
		if g == "" {
			continue
		}
		_, _ = s.Store.DB.ExecContext(ctx,
			`UPDATE playlist_tracks SET genre = ? WHERE provider_track_id = ? AND genre = ''`,
			g, trackID,
		)
	}
}

func (s *SpotifySyncer) upsertPlaylists(ctx context.Context, userID int64, pls []spotify.PlaylistSummary, client *spotify.Client, artistIDSet map[string]bool) error {
	for _, p := range pls {
		imageURL := ""
		if len(p.Images) > 0 {
			imageURL = p.Images[len(p.Images)-1].URL // last = smallest
			if len(p.Images) > 1 {
				imageURL = p.Images[len(p.Images)-1].URL
			}
		}
		var playlistID int64
		err := s.Store.DB.QueryRowContext(ctx,
			`INSERT INTO playlists (user_id, provider, provider_playlist_id, name, description, owner_name, track_count, is_public, is_collaborative, snapshot_id, image_url, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(user_id, provider, provider_playlist_id) DO UPDATE SET
			   name=excluded.name, description=excluded.description, owner_name=excluded.owner_name,
			   track_count=excluded.track_count, is_public=excluded.is_public, is_collaborative=excluded.is_collaborative,
			   snapshot_id=excluded.snapshot_id, image_url=excluded.image_url, updated_at=CURRENT_TIMESTAMP
			 RETURNING id`,
			userID, spotify.Provider, p.ID, p.Name, p.Description, p.Owner.DisplayName, p.Tracks.Total,
			boolToInt(p.Public), boolToInt(p.Collaborative), p.SnapshotID, imageURL,
		).Scan(&playlistID)
		if err != nil {
			return err
		}

		tracks, err := client.PlaylistTracks(ctx, p.ID)
		if err != nil {
			// Spotify returns 403 for playlists its new-dev-mode quota
			// doesn't cover (curated content, some followed playlists).
			// Skip those so the rest of the sync can still finish.
			if isAccessError(err) {
				log.Printf("skipping playlist %q (%s): %v", p.Name, p.ID, err)
				continue
			}
			return fmt.Errorf("playlist %s tracks: %w", p.Name, err)
		}
		if err := s.replacePlaylistTracks(ctx, playlistID, tracks, artistIDSet); err != nil {
			return err
		}
	}
	return nil
}

func isAccessError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "403") || strings.Contains(msg, "404")
}

func (s *SpotifySyncer) replacePlaylistTracks(ctx context.Context, playlistID int64, tracks []spotify.PlaylistTrack, artistIDSet map[string]bool) error {
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_tracks WHERE playlist_id = ?`, playlistID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO playlist_tracks (playlist_id, position, provider_track_id, track_name, artist_name, album_name, duration_ms, preview_url, added_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	s.mu.Lock()
	if s.playlistTrackArtists == nil {
		s.playlistTrackArtists = make(map[string]string)
	}
	s.mu.Unlock()

	for i, t := range tracks {
		if t.Track == nil {
			continue
		}
		if t.Track.IsEpisode() {
			continue
		}
		trackID := t.Track.ID
		if trackID == "" && !t.IsLocal && !t.Track.IsLocal {
			continue
		}
		// Collect artist IDs for genre fetching.
		if len(t.Track.Artists) > 0 && t.Track.Artists[0].ID != "" {
			artistIDSet[t.Track.Artists[0].ID] = true
			if trackID != "" {
				s.mu.Lock()
				s.playlistTrackArtists[trackID] = t.Track.Artists[0].ID
				s.mu.Unlock()
			}
		}
		if _, err := stmt.ExecContext(ctx, playlistID, i, trackID, t.Track.Name, t.Track.ArtistNames(), t.Track.Album.Name, t.Track.DurationMs, t.Track.PreviewURL, nullTime(parseTime(t.AddedAt))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SpotifySyncer) replaceLiked(ctx context.Context, userID int64, items []spotify.SavedTrack, artistGenres map[string]string) error {
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM liked_tracks WHERE user_id = ? AND provider = ?`, userID, spotify.Provider); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO liked_tracks (user_id, provider, provider_track_id, track_name, artist_name, album_name, duration_ms, album_image_url, genre, preview_url, added_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, it := range items {
		if it.Track.ID == "" {
			continue
		}
		albumImg := ""
		if len(it.Track.Album.Images) > 0 {
			albumImg = it.Track.Album.Images[len(it.Track.Album.Images)-1].URL
		}
		genre := ""
		if len(it.Track.Artists) > 0 && it.Track.Artists[0].ID != "" {
			genre = artistGenres[it.Track.Artists[0].ID]
		}
		if _, err := stmt.ExecContext(ctx, userID, spotify.Provider, it.Track.ID, it.Track.Name, it.Track.ArtistNames(), it.Track.Album.Name, it.Track.DurationMs, albumImg, genre, it.Track.PreviewURL, nullTime(parseTime(it.AddedAt))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SpotifySyncer) replaceAlbums(ctx context.Context, userID int64, items []spotify.SavedAlbum) error {
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM saved_albums WHERE user_id = ? AND provider = ?`, userID, spotify.Provider); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO saved_albums (user_id, provider, provider_album_id, album_name, artist_name, image_url, added_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, it := range items {
		if it.Album.ID == "" {
			continue
		}
		artist := ""
		if len(it.Album.Artists) > 0 {
			artist = it.Album.Artists[0].Name
			for _, a := range it.Album.Artists[1:] {
				artist += ", " + a.Name
			}
		}
		albumImg := ""
		if len(it.Album.Images) > 0 {
			albumImg = it.Album.Images[len(it.Album.Images)-1].URL
		}
		if _, err := stmt.ExecContext(ctx, userID, spotify.Provider, it.Album.ID, it.Album.Name, artist, albumImg, nullTime(parseTime(it.AddedAt))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type persistingTokenSource struct {
	base   oauth2.TokenSource
	store  *store.Store
	acctID int64
	last   oauth2.Token
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := p.base.Token()
	if err != nil {
		return nil, err
	}
	if tok.AccessToken != p.last.AccessToken || tok.RefreshToken != p.last.RefreshToken {
		_ = p.store.SaveToken(context.Background(), p.acctID, tok.AccessToken, tok.RefreshToken, tok.TokenType, tok.Expiry)
		p.last = *tok
	}
	return tok, nil
}

func parseTime(s string) sql.NullTime {
	if s == "" {
		return sql.NullTime{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return sql.NullTime{Valid: true, Time: t}
	}
	return sql.NullTime{}
}

func nullTime(n sql.NullTime) interface{} {
	if !n.Valid {
		return nil
	}
	return n.Time
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
