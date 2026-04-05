package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/alan-botts/match-my-jam/internal/config"
	"github.com/alan-botts/match-my-jam/internal/googleauth"
	"github.com/alan-botts/match-my-jam/internal/session"
	"github.com/alan-botts/match-my-jam/internal/spotify"
	"github.com/alan-botts/match-my-jam/internal/store"
	mmjsync "github.com/alan-botts/match-my-jam/internal/sync"
	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"
)

//go:embed templates/*.html static/*
var assets embed.FS

type Server struct {
	Cfg          *config.Config
	Sessions     *session.Manager
	Store        *store.Store
	SpotifyOAuth *oauth2.Config
	GoogleOAuth  *oauth2.Config
	SpotifySync  *mmjsync.SpotifySyncer
	Templates    *template.Template
}

func New(cfg *config.Config, s *store.Store) (*Server, error) {
	secure := strings.HasPrefix(cfg.BaseURL, "https://")
	sm := session.New(cfg.SessionHashKey, cfg.SessionBlockKey, secure)
	sp := spotify.OAuthConfig(cfg.SpotifyClientID, cfg.SpotifyClientSecret, cfg.BaseURL+"/auth/spotify/callback")
	var gp *oauth2.Config
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		gp = googleauth.OAuthConfig(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.BaseURL+"/auth/google/callback")
	}
	tpl, err := template.New("").Funcs(funcs()).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{
		Cfg:          cfg,
		Sessions:     sm,
		Store:        s,
		SpotifyOAuth: sp,
		GoogleOAuth:  gp,
		SpotifySync:  &mmjsync.SpotifySyncer{Store: s, OAuthConf: sp},
		Templates:    tpl,
	}, nil
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Handle("/static/*", http.FileServer(http.FS(assets)))

	r.Get("/", s.handleIndex)
	r.Get("/login", s.handleLogin)
	r.Post("/logout", s.handleLogout)
	r.Get("/logout", s.handleLogout)

	r.Get("/auth/spotify/start", s.handleSpotifyStart)
	r.Get("/auth/spotify/callback", s.handleSpotifyCallback)
	r.Get("/auth/google/start", s.handleGoogleStart)
	r.Get("/auth/google/callback", s.handleGoogleCallback)

	r.Get("/join/{token}", s.handleJoin)

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/dashboard", s.handleDashboard)
		r.Post("/sync/spotify", s.handleSyncSpotify)

		r.Get("/invite", s.handleInvite)
		r.Get("/friends", s.handleFriends)
		r.Post("/friends/request", s.handleFriendRequest)
		r.Post("/friends/{id}/respond", s.handleFriendRespond)
		r.Get("/friends/{id}/overlap", s.handleOverlap)

		r.Get("/library/playlists", s.handlePlaylists)
		r.Get("/library/playlists/{id}", s.handlePlaylistDetail)
		r.Get("/library/liked", s.handleLiked)
		r.Get("/library/albums", s.handleAlbums)

		r.Get("/settings", s.handleSettings)
		r.Post("/settings/disconnect/{id}", s.handleDisconnect)
		r.Post("/settings/delete-account", s.handleDeleteAccount)
	})

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/admin", func(r chi.Router) {
		r.Use(s.adminAuth)
		r.Get("/users", s.handleAdminListUsers)
		r.Post("/users/{id}/delete", s.handleAdminDeleteUser)
	})

	return r
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, ok := s.Sessions.Get(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, d.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type ctxKey int

const userIDKey ctxKey = 1

func currentUserID(r *http.Request) int64 {
	v, _ := r.Context().Value(userIDKey).(int64)
	return v
}

func (s *Server) render(w http.ResponseWriter, name string, data map[string]interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if data == nil {
		data = map[string]interface{}{}
	}
	if _, ok := data["Title"]; !ok {
		data["Title"] = defaultTitles[name]
		if data["Title"] == "" {
			data["Title"] = "Match My Jam"
		}
	}
	if err := s.Templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

var defaultTitles = map[string]string{
	"landing.html":         "Match My Jam — find the music you share",
	"login.html":           "Log in · Match My Jam",
	"dashboard.html":       "Dashboard · Match My Jam",
	"friends.html":         "Friends · Match My Jam",
	"overlap.html":         "Overlap · Match My Jam",
	"settings.html":        "Settings · Match My Jam",
	"invite.html":          "Invite · Match My Jam",
	"join.html":            "Join Match My Jam",
	"playlists.html":       "Playlists · Match My Jam",
	"playlist_detail.html": "Playlist · Match My Jam",
	"liked.html":           "Liked Tracks · Match My Jam",
	"albums.html":          "Saved Albums · Match My Jam",
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.Sessions.Get(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	s.render(w, "landing.html", nil)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.Sessions.Get(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	s.render(w, "login.html", nil)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.Sessions.Clear(w)
	// Nuke every app cookie so re-auth starts from a clean slate.
	for _, name := range []string{stateCookieName, googleStateCookie, inviteCookieName} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1})
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

const stateCookieName = "mmj_oauth_state"

func (s *Server) handleSpotifyStart(w http.ResponseWriter, r *http.Request) {
	secure := strings.HasPrefix(s.Cfg.BaseURL, "https://")
	// Clear any stale state cookie before setting a fresh one.
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Value: "", Path: "/", MaxAge: -1})
	state := randomState()
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(10 * time.Minute),
	})
	http.Redirect(w, r, s.SpotifyOAuth.AuthCodeURL(state), http.StatusSeeOther)
}

func (s *Server) handleSpotifyCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Value: "", Path: "/", MaxAge: -1})

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	tok, err := s.SpotifyOAuth.Exchange(ctx, code)
	if err != nil {
		log.Printf("spotify exchange: %v", err)
		http.Error(w, "oauth exchange failed", http.StatusBadGateway)
		return
	}

	client := spotify.NewClient(ctx, s.SpotifyOAuth, tok)
	profile, err := client.Me(ctx)
	if err != nil {
		log.Printf("spotify me: %v", err)
		http.Error(w, "failed to load profile", http.StatusBadGateway)
		return
	}
	avatar := ""
	if len(profile.Images) > 0 {
		avatar = profile.Images[0].URL
	}

	user, acct, err := s.Store.FindOrCreateUserByProvider(ctx, spotify.Provider, profile.ID, profile.Email, profile.DisplayName, avatar)
	if err != nil {
		log.Printf("find/create user: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if err := s.Store.SaveToken(ctx, acct.ID, tok.AccessToken, tok.RefreshToken, tok.TokenType, tok.Expiry); err != nil {
		log.Printf("save token: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	if err := s.Sessions.Set(w, session.Data{UserID: user.ID}); err != nil {
		log.Printf("set session: %v", err)
	}
	s.applyInviteCookie(w, r, user.ID)

	// Kick off an initial sync in the background.
	go func(uid int64) {
		bg, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.SpotifySync.Run(bg, uid); err != nil {
			log.Printf("initial sync user=%d: %v", uid, err)
		}
	}(user.ID)

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, err := s.Store.GetUser(r.Context(), uid)
	if err != nil {
		http.Error(w, "user not found", http.StatusInternalServerError)
		return
	}
	stats, _ := s.Store.UserStats(r.Context(), uid)
	lastSync, _ := s.Store.LatestSyncRun(r.Context(), uid, spotify.Provider)
	s.render(w, "dashboard.html", map[string]interface{}{
		"User":     user,
		"Stats":    stats,
		"LastSync": lastSync,
	})
}

func (s *Server) handleSyncSpotify(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.SpotifySync.Run(bg, uid); err != nil {
			log.Printf("manual sync user=%d: %v", uid, err)
		}
	}()
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func randomState() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func funcs() template.FuncMap {
	return template.FuncMap{
		"fmtTime": func(t *time.Time) string {
			if t == nil {
				return "—"
			}
			return t.Format("Jan 2, 3:04 PM")
		},
		"fmtDuration": func(ms int) string {
			totalSec := ms / 1000
			m := totalSec / 60
			sec := totalSec % 60
			return fmt.Sprintf("%d:%02d", m, sec)
		},
	}
}
