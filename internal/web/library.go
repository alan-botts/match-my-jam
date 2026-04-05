package web

import (
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

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
	s.render(w, "playlist_detail.html", map[string]interface{}{
		"User":     user,
		"Playlist": playlist,
		"Tracks":   tracks,
	})
}

func (s *Server) handleLiked(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, _ := s.Store.GetUser(r.Context(), uid)
	tracks, err := s.Store.UserLikedTracks(r.Context(), uid)
	if err != nil {
		log.Printf("liked tracks: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	s.render(w, "liked.html", map[string]interface{}{
		"User":   user,
		"Tracks": tracks,
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
