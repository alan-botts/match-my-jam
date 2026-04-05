package web

import (
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// adminAuth is a simple middleware that gates admin routes behind the
// session hash key (first 16 hex chars). This isn't a login; it's a
// URL-based shared secret for one-off ops.
func (s *Server) adminAuth(next http.Handler) http.Handler {
	secret := hex.EncodeToString(s.Cfg.SessionHashKey)[:16]
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != secret {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := s.Store.DeleteUser(r.Context(), id); err != nil {
		log.Printf("admin delete user %d: %v", id, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "user %d deleted\n", id)
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.DB.QueryContext(r.Context(),
		`SELECT u.id, u.display_name, u.email, u.invite_token,
		        (SELECT COUNT(*) FROM auth_accounts WHERE user_id = u.id) AS accts
		 FROM users u ORDER BY u.id`,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	w.Header().Set("Content-Type", "text/plain")
	for rows.Next() {
		var id int64
		var name, email, token string
		var accts int
		if err := rows.Scan(&id, &name, &email, &token, &accts); err != nil {
			fmt.Fprintf(w, "scan error: %v\n", err)
			return
		}
		fmt.Fprintf(w, "id=%d name=%q email=%q token=%q accts=%d\n", id, name, email, token, accts)
	}
}
