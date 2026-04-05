package web

import (
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, _ := s.Store.GetUser(r.Context(), uid)
	accts, _ := s.Store.AuthAccounts(r.Context(), uid)
	s.render(w, "settings.html", map[string]interface{}{
		"User":     user,
		"Accounts": accts,
		"Flash":    r.URL.Query().Get("flash"),
	})
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	acctID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := s.Store.DeleteAuthAccount(r.Context(), uid, acctID); err != nil {
		http.Redirect(w, r, "/settings?flash="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?flash=disconnected", http.StatusSeeOther)
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	if err := s.Store.DeleteUser(r.Context(), uid); err != nil {
		log.Printf("delete user: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	s.Sessions.Clear(w)
	http.Redirect(w, r, "/?flash=account+deleted", http.StatusSeeOther)
}
