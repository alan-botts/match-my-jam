package web

import (
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const inviteCookieName = "mmj_invite"
const inviteCookieTTL = time.Hour

// handleJoin is the public landing for an invite link. It stashes the
// token in a short-lived cookie and redirects to the login page. On the
// next successful OAuth callback, we'll check the cookie and auto-friend.
func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	inviter, err := s.Store.FindUserByInviteToken(r.Context(), token)
	if err != nil {
		http.Redirect(w, r, "/?flash=invite+link+not+found", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     inviteCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   strings.HasPrefix(s.Cfg.BaseURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(inviteCookieTTL),
	})
	// If they're already logged in, just friend immediately.
	if d, ok := s.Sessions.Get(r); ok {
		s.applyInviteCookie(w, r, d.UserID)
		http.Redirect(w, r, "/friends?flash=friend+added", http.StatusSeeOther)
		return
	}
	s.render(w, "join.html", map[string]interface{}{
		"Inviter": inviter,
	})
}

// applyInviteCookie looks at the invite cookie on the request and, if
// valid, creates an accepted friendship between the inviter and the
// current user. It clears the cookie on success or failure.
func (s *Server) applyInviteCookie(w http.ResponseWriter, r *http.Request, userID int64) {
	c, err := r.Cookie(inviteCookieName)
	if err != nil || c.Value == "" {
		return
	}
	// Clear the cookie either way.
	http.SetCookie(w, &http.Cookie{Name: inviteCookieName, Value: "", Path: "/", MaxAge: -1})

	inviter, err := s.Store.FindUserByInviteToken(r.Context(), c.Value)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("lookup inviter: %v", err)
		}
		return
	}
	if inviter.ID == userID {
		return
	}
	if err := s.Store.AutoAcceptFriendship(r.Context(), inviter.ID, userID); err != nil {
		log.Printf("auto-accept friendship (inviter=%d, user=%d): %v", inviter.ID, userID, err)
	}
}

func (s *Server) handleInvite(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, _ := s.Store.GetUser(r.Context(), uid)
	token, err := s.Store.InviteTokenForUser(r.Context(), uid)
	if err != nil {
		log.Printf("invite token: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	link := s.Cfg.BaseURL + "/join/" + token
	s.render(w, "invite.html", map[string]interface{}{
		"User": user,
		"Link": link,
	})
}
