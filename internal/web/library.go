package web

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func formatTotalDuration(totalMs int) string {
	totalSec := totalMs / 1000
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func (s *Server) handlePlaylists(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, _ := s.Store.GetUser(r.Context(), uid)
	playlists, err := s.Store.UserPlaylists(r.Context(), uid)
	if err != nil {
		log.Printf("playlists: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	s.render(w, "playlists.html", map[string]interface{}{
		"User":      user,
		"Playlists": playlists,
	})
}

func (s *Server) handlePlaylistDetail(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, _ := s.Store.GetUser(r.Context(), uid)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	playlist, err := s.Store.PlaylistByID(r.Context(), id, uid)
	if err != nil {
		http.Error(w, "playlist not found", http.StatusNotFound)
		return
	}
	tracks, err := s.Store.PlaylistTracks(r.Context(), id)
	if err != nil {
		log.Printf("playlist tracks: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	var totalMs int
	for _, t := range tracks {
		totalMs += t.DurationMs
	}
	s.render(w, "playlist_detail.html", map[string]interface{}{
		"User":          user,
		"Playlist":      playlist,
		"Tracks":        tracks,
		"TotalDuration": formatTotalDuration(totalMs),
	})
}

const likedPerPage = 100

func (s *Server) handleLiked(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, _ := s.Store.GetUser(r.Context(), uid)

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * likedPerPage

	total, _ := s.Store.CountLikedTracks(r.Context(), uid)
	totalPages := (total + likedPerPage - 1) / likedPerPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
		offset = (page - 1) * likedPerPage
	}

	tracks, err := s.Store.UserLikedTracksPaged(r.Context(), uid, likedPerPage, offset)
	if err != nil {
		log.Printf("liked tracks: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	var totalMs int
	for _, t := range tracks {
		totalMs += t.DurationMs
	}
	s.render(w, "liked.html", map[string]interface{}{
		"User":          user,
		"Tracks":        tracks,
		"TotalDuration": formatTotalDuration(totalMs),
		"Page":          page,
		"TotalPages":    totalPages,
		"TotalTracks":   total,
		"HasPrev":       page > 1,
		"HasNext":       page < totalPages,
		"PrevPage":      page - 1,
		"NextPage":      page + 1,
	})
}

func (s *Server) handleAlbums(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, _ := s.Store.GetUser(r.Context(), uid)
	albums, err := s.Store.UserSavedAlbums(r.Context(), uid)
	if err != nil {
		log.Printf("saved albums: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	s.render(w, "albums.html", map[string]interface{}{
		"User":   user,
		"Albums": albums,
	})
}

func (s *Server) handleExportPlaylists(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	playlists, err := s.Store.UserPlaylists(r.Context(), uid)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="playlists.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Name", "Owner", "Tracks", "Public", "Collaborative", "Spotify URL"})
	for _, p := range playlists {
		pub := "No"
		if p.IsPublic {
			pub = "Yes"
		}
		collab := "No"
		if p.IsCollaborative {
			collab = "Yes"
		}
		_ = cw.Write([]string{
			p.Name, p.OwnerName, strconv.Itoa(p.TrackCount), pub, collab,
			"https://open.spotify.com/playlist/" + p.ProviderPlaylistID,
		})
	}
	cw.Flush()
}

func (s *Server) handleExportLiked(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	tracks, err := s.Store.UserLikedTracks(r.Context(), uid)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="liked_tracks.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Track", "Artist", "Album", "Genre", "Duration", "Added", "Spotify URL"})
	for _, t := range tracks {
		added := ""
		if t.AddedAt != nil {
			added = t.AddedAt.Format("2006-01-02")
		}
		dur := fmt.Sprintf("%d:%02d", t.DurationMs/1000/60, t.DurationMs/1000%60)
		url := ""
		if t.ProviderTrackID != "" {
			url = "https://open.spotify.com/track/" + t.ProviderTrackID
		}
		_ = cw.Write([]string{t.TrackName, t.ArtistName, t.AlbumName, t.Genre, dur, added, url})
	}
	cw.Flush()
}

func (s *Server) handleExportAlbums(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	albums, err := s.Store.UserSavedAlbums(r.Context(), uid)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="saved_albums.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Album", "Artist", "Added", "Spotify URL"})
	for _, a := range albums {
		added := ""
		if a.AddedAt != nil {
			added = a.AddedAt.Format("2006-01-02")
		}
		url := ""
		if a.ProviderAlbumID != "" {
			url = "https://open.spotify.com/album/" + a.ProviderAlbumID
		}
		_ = cw.Write([]string{a.AlbumName, a.ArtistName, added, url})
	}
	cw.Flush()
}
