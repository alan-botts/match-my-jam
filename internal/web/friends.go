package web

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/alan-botts/match-my-jam/internal/store"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleFriends(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, _ := s.Store.GetUser(r.Context(), uid)
	friends, _ := s.Store.Friends(r.Context(), uid)
	incoming, _ := s.Store.IncomingRequests(r.Context(), uid)
	outgoing, _ := s.Store.OutgoingRequests(r.Context(), uid)
	s.render(w, "friends.html", map[string]interface{}{
		"User":     user,
		"Friends":  friends,
		"Incoming": incoming,
		"Outgoing": outgoing,
		"Flash":    r.URL.Query().Get("flash"),
	})
}

func (s *Server) handleFriendRequest(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		http.Redirect(w, r, "/friends?flash=enter+an+email", http.StatusSeeOther)
		return
	}
	target, err := s.Store.FindUserByEmail(r.Context(), email)
	if err != nil {
		http.Redirect(w, r, "/friends?flash=no+user+with+that+email", http.StatusSeeOther)
		return
	}
	if target.ID == uid {
		http.Redirect(w, r, "/friends?flash=cannot+friend+yourself", http.StatusSeeOther)
		return
	}
	if _, err := s.Store.SendFriendRequest(r.Context(), uid, target.ID); err != nil {
		if err == store.ErrSelfFriend {
			http.Redirect(w, r, "/friends?flash=cannot+friend+yourself", http.StatusSeeOther)
			return
		}
		log.Printf("send friend request: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/friends?flash=request+sent", http.StatusSeeOther)
}

func (s *Server) handleFriendRespond(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	accept := r.FormValue("action") == "accept"
	if err := s.Store.RespondToFriendRequest(r.Context(), id, uid, accept); err != nil {
		log.Printf("respond friend request: %v", err)
		http.Redirect(w, r, "/friends?flash=request+not+found", http.StatusSeeOther)
		return
	}
	flash := "request+declined"
	if accept {
		flash = "friend+added"
	}
	http.Redirect(w, r, "/friends?flash="+flash, http.StatusSeeOther)
}

func (s *Server) handleOverlap(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	friendID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	ok, err := s.Store.AreFriends(r.Context(), uid, friendID)
	if err != nil || !ok {
		http.Error(w, "not friends", http.StatusForbidden)
		return
	}
	me, _ := s.Store.GetUser(r.Context(), uid)
	friend, err := s.Store.GetUser(r.Context(), friendID)
	if err != nil {
		http.Error(w, "friend not found", http.StatusNotFound)
		return
	}
	overlap, err := s.Store.ComputeOverlap(r.Context(), uid, friendID)
	if err != nil {
		log.Printf("compute overlap: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	s.render(w, "overlap.html", map[string]interface{}{
		"User":    me,
		"Friend":  friend,
		"Overlap": overlap,
	})
}
