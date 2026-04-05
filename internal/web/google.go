package web

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/alan-botts/match-my-jam/internal/googleauth"
	"github.com/alan-botts/match-my-jam/internal/session"
)

func (s *Server) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	if s.GoogleOAuth == nil {
		http.Error(w, "Google sign-in is not configured on this instance", http.StatusNotImplemented)
		return
	}
	state := randomState()
	http.SetCookie(w, &http.Cookie{
		Name:     googleStateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   strings.HasPrefix(s.Cfg.BaseURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(10 * time.Minute),
	})
	http.Redirect(w, r, s.GoogleOAuth.AuthCodeURL(state), http.StatusSeeOther)
}

func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if s.GoogleOAuth == nil {
		http.Error(w, "Google sign-in is not configured", http.StatusNotImplemented)
		return
	}
	c, err := r.Cookie(googleStateCookie)
	if err != nil || c.Value == "" || c.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: googleStateCookie, Value: "", Path: "/", MaxAge: -1})

	ctx := r.Context()
	tok, err := s.GoogleOAuth.Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		log.Printf("google exchange: %v", err)
		http.Error(w, "oauth exchange failed", http.StatusBadGateway)
		return
	}
	info, err := googleauth.FetchUserInfo(ctx, s.GoogleOAuth, tok)
	if err != nil {
		log.Printf("google userinfo: %v", err)
		http.Error(w, "failed to load profile", http.StatusBadGateway)
		return
	}

	user, acct, err := s.Store.FindOrCreateUserByProvider(ctx, googleauth.Provider, info.Sub, info.Email, info.Name, info.Picture)
	if err != nil {
		log.Printf("find/create google user: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if err := s.Store.SaveToken(ctx, acct.ID, tok.AccessToken, tok.RefreshToken, tok.TokenType, tok.Expiry); err != nil {
		log.Printf("save token: %v", err)
	}
	_ = s.Sessions.Set(w, session.Data{UserID: user.ID})
	s.applyInviteCookie(w, r, user.ID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

const googleStateCookie = "mmj_google_state"
