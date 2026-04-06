package sync

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/alan-botts/match-my-jam/internal/spotify"
	"github.com/alan-botts/match-my-jam/internal/store"
	"golang.org/x/oauth2"
)

type SpotifySyncer struct {
	Store     *store.Store
	OAuthConf *oauth2.Config
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

	pls, err := client.AllPlaylists(ctx)
	if err != nil {
		return fmt.Errorf("fetch playlists: %w", err)
	}
	if err := s.upsertPlaylists(ctx, userID, pls, client); err != nil {
		return fmt.Errorf("upsert playlists: %w", err)
	}
	*playlistCount = len(pls)

	likedTracks, err := client.LikedTracks(ctx)
	if err != nil {
		return fmt.Errorf("fetch liked tracks: %w", err)
	}
	if err := s.replaceLiked(ctx, userID, likedTracks); err != nil {
		return fmt.Errorf("replace liked: %w", err)
	}
	*likedCount = len(likedTracks)

	savedAlbums, err := client.SavedAlbums(ctx)
	if err != nil {
		return fmt.Errorf("fetch saved albums: %w", err)
	}
	if err := s.replaceAlbums(ctx, userID, savedAlbums); err != nil {
		return fmt.Errorf("replace albums: %w", err)
	}
	*albumCount = len(savedAlbums)

	return nil
}

func (s *SpotifySyncer) upsertPlaylists(ctx context.Context, userID int64, pls []spotify.PlaylistSummary, client *spotify.Client) error {
	for _, p := range pls {
		var playlistID int64
		err := s.Store.DB.QueryRowContext(ctx,
			`INSERT INTO playlists (user_id, provider, provider_playlist_id, name, description, owner_name, track_count, is_public, is_collaborative, snapshot_id, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(user_id, provider, provider_playlist_id) DO UPDATE SET
			   name=excluded.name, description=excluded.description, owner_name=excluded.owner_name,
			   track_count=excluded.track_count, is_public=excluded.is_public, is_collaborative=excluded.is_collaborative,
			   snapshot_id=excluded.snapshot_id, updated_at=CURRENT_TIMESTAMP
			 RETURNING id`,
			userID, spotify.Provider, p.ID, p.Name, p.Description, p.Owner.DisplayName, p.Tracks.Total,
			boolToInt(p.Public), boolToInt(p.Collaborative), p.SnapshotID,
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
		if err := s.replacePlaylistTracks(ctx, playlistID, tracks); err != nil {
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

func (s *SpotifySyncer) replacePlaylistTracks(ctx context.Context, playlistID int64, tracks []spotify.PlaylistTrack) error {
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_tracks WHERE playlist_id = ?`, playlistID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO playlist_tracks (playlist_id, position, provider_track_id, track_name, artist_name, album_name, duration_ms, added_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, t := range tracks {
		if t.Track == nil {
			// Null item — track was removed or is unavailable.
			continue
		}
		if t.Track.IsEpisode() {
			// Podcast episodes are not music tracks; skip them.
			continue
		}
		// Local files have no Spotify ID but may have name/artist/album.
		// We store them with an empty provider_track_id so they still
		// appear in the playlist view. The is_local flag on the outer
		// PlaylistTrack or the inner Track can both indicate this.
		trackID := t.Track.ID
		if trackID == "" && !t.IsLocal && !t.Track.IsLocal {
			// No ID and not a local file — skip unknown item.
			continue
		}
		if _, err := stmt.ExecContext(ctx, playlistID, i, trackID, t.Track.Name, t.Track.ArtistNames(), t.Track.Album.Name, t.Track.DurationMs, nullTime(parseTime(t.AddedAt))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SpotifySyncer) replaceLiked(ctx context.Context, userID int64, items []spotify.SavedTrack) error {
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM liked_tracks WHERE user_id = ? AND provider = ?`, userID, spotify.Provider); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO liked_tracks (user_id, provider, provider_track_id, track_name, artist_name, album_name, duration_ms, added_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, it := range items {
		if it.Track.ID == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, userID, spotify.Provider, it.Track.ID, it.Track.Name, it.Track.ArtistNames(), it.Track.Album.Name, it.Track.DurationMs, nullTime(parseTime(it.AddedAt))); err != nil {
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
		`INSERT OR IGNORE INTO saved_albums (user_id, provider, provider_album_id, album_name, artist_name, added_at) VALUES (?, ?, ?, ?, ?, ?)`,
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
		if _, err := stmt.ExecContext(ctx, userID, spotify.Provider, it.Album.ID, it.Album.Name, artist, nullTime(parseTime(it.AddedAt))); err != nil {
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
