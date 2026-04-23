package web

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const jamCookieName = "mmj_jam"
const jamCookieTTL = 24 * time.Hour

func (s *Server) handleJams(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, _ := s.Store.GetUser(r.Context(), uid)
	jams, err := s.Store.UserJamSessions(r.Context(), uid)
	if err != nil {
		log.Printf("list jams: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	s.render(w, "jams.html", map[string]interface{}{
		"User":  user,
		"Jams":  jams,
		"Flash": r.URL.Query().Get("flash"),
	})
}

func (s *Server) handleNewJam(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, _ := s.Store.GetUser(r.Context(), uid)
	s.render(w, "new_jam.html", map[string]interface{}{"User": user})
}

func (s *Server) handleCreateJam(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	name := strings.TrimSpace(r.FormValue("name"))
	jam, err := s.Store.CreateJamSession(r.Context(), uid, name)
	if err != nil {
		log.Printf("create jam: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/jams/"+strconv.FormatInt(jam.ID, 10)+"?flash=jam+started", http.StatusSeeOther)
}

func (s *Server) handleJamDetail(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	ok, err := s.Store.UserInJam(r.Context(), id, uid)
	if err != nil || !ok {
		http.Error(w, "jam not found", http.StatusNotFound)
		return
	}
	user, _ := s.Store.GetUser(r.Context(), uid)
	jam, err := s.Store.GetJamSession(r.Context(), id)
	if err != nil {
		http.Error(w, "jam not found", http.StatusNotFound)
		return
	}
	pairs, err := s.Store.JamPairOverlaps(r.Context(), id)
	if err != nil {
		log.Printf("jam overlaps: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	s.render(w, "jam.html", map[string]interface{}{
		"User":    user,
		"Jam":     jam,
		"Pairs":   pairs,
		"JoinURL": s.Cfg.BaseURL + "/jams/join/" + jam.Token,
		"Flash":   r.URL.Query().Get("flash"),
	})
}

func (s *Server) handleJoinJam(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	jam, err := s.Store.FindJamByToken(r.Context(), token)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("lookup jam: %v", err)
		}
		http.Redirect(w, r, "/?flash=jam+link+not+found", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     jamCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   strings.HasPrefix(s.Cfg.BaseURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(jamCookieTTL),
	})
	if d, ok := s.Sessions.Get(r); ok {
		_ = s.Store.AddJamMember(r.Context(), jam.ID, d.UserID)
		http.SetCookie(w, &http.Cookie{Name: jamCookieName, Value: "", Path: "/", MaxAge: -1})
		http.Redirect(w, r, "/jams/"+strconv.FormatInt(jam.ID, 10)+"?flash=joined+jam", http.StatusSeeOther)
		return
	}
	s.render(w, "join_jam.html", map[string]interface{}{
		"Jam": jam,
	})
}

func (s *Server) applyJamCookie(w http.ResponseWriter, r *http.Request, userID int64) *int64 {
	c, err := r.Cookie(jamCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: jamCookieName, Value: "", Path: "/", MaxAge: -1})
	jam, err := s.Store.FindJamByToken(r.Context(), c.Value)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("lookup pending jam: %v", err)
		}
		return nil
	}
	if err := s.Store.AddJamMember(r.Context(), jam.ID, userID); err != nil {
		log.Printf("add pending jam member (jam=%d user=%d): %v", jam.ID, userID, err)
		return nil
	}
	return &jam.ID
}
