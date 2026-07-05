package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

func (a *app) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if a.cfg.Username == "" || a.cfg.Password == "" {
		writeError(w, http.StatusNotFound, "password login is not enabled")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	if !constantStringEqual(r.Form.Get("username"), a.cfg.Username) || !constantStringEqual(r.Form.Get("password"), a.cfg.Password) {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	a.sessions.Set(w, r, a.cfg.Username, a.cookieSecure(r))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.sessions.Clear(w, r, a.cookieSecure(r))
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (a *app) cookieSecure(r *http.Request) bool {
	return a.cfg.SessionCookieSecure || r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

type sessionStore struct {
	mu     sync.Mutex
	key    []byte
	values map[string]session
}

type session struct {
	User      string
	ExpiresAt time.Time
}

func newSessionStore(key []byte) *sessionStore {
	return &sessionStore{key: key, values: map[string]session{}}
}

func (s *sessionStore) Set(w http.ResponseWriter, r *http.Request, user string, secure bool) {
	token := randomToken(32)
	expires := time.Now().Add(24 * time.Hour)
	s.mu.Lock()
	s.values[token] = session{User: user, ExpiresAt: expires}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "hby_control_session",
		Value:    token + "." + s.sign(token),
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func (s *sessionStore) Clear(w http.ResponseWriter, r *http.Request, secure bool) {
	if c, err := r.Cookie("hby_control_session"); err == nil {
		token, ok := splitSigned(c.Value)
		if ok {
			s.mu.Lock()
			delete(s.values, token)
			s.mu.Unlock()
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "hby_control_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func (s *sessionStore) User(r *http.Request) (string, bool) {
	c, err := r.Cookie("hby_control_session")
	if err != nil {
		return "", false
	}
	token, sig, ok := splitSignedValue(c.Value)
	if !ok || !constantStringEqual(sig, s.sign(token)) {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.values[token]
	if !ok || time.Now().After(sess.ExpiresAt) {
		delete(s.values, token)
		return "", false
	}
	return sess.User, true
}

func (s *sessionStore) sign(v string) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(v))
	return hex.EncodeToString(mac.Sum(nil))
}

func splitSigned(v string) (string, bool) {
	token, _, ok := strings.Cut(v, ".")
	return token, ok
}

func splitSignedValue(v string) (string, string, bool) {
	token, sig, ok := strings.Cut(v, ".")
	return token, sig, ok
}
