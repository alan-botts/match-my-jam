package session

import (
	"net/http"
	"time"

	"github.com/gorilla/securecookie"
)

const cookieName = "mmj_session"

type Manager struct {
	sc     *securecookie.SecureCookie
	secure bool
}

type Data struct {
	UserID int64 `json:"user_id"`
}

func New(hashKey, blockKey []byte, secure bool) *Manager {
	return &Manager{sc: securecookie.New(hashKey, blockKey), secure: secure}
}

func (m *Manager) Set(w http.ResponseWriter, d Data) error {
	encoded, err := m.sc.Encode(cookieName, d)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})
	return nil
}

func (m *Manager) Get(r *http.Request) (Data, bool) {
	var d Data
	c, err := r.Cookie(cookieName)
	if err != nil {
		return d, false
	}
	if err := m.sc.Decode(cookieName, c.Value, &d); err != nil {
		return d, false
	}
	if d.UserID == 0 {
		return d, false
	}
	return d, true
}

func (m *Manager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
