package main

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type app struct {
	cfg           config
	sup           *supervisor
	sessions      *sessionStore
	states        *stateStore
	oidcMu        sync.Mutex
	oidcDiscovery oidcDiscovery
}

func newApp(cfg config, sup *supervisor) *app {
	return &app{
		cfg:      cfg,
		sup:      sup,
		sessions: newSessionStore(cfg.SessionKey),
		states:   newStateStore(),
	}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", a.handleLoginPage)
	mux.HandleFunc("/auth/login", a.handlePasswordLogin)
	mux.HandleFunc("/auth/logout", a.handleLogout)
	mux.HandleFunc("/auth/oidc/login", a.handleOIDCLogin)
	mux.HandleFunc("/auth/oidc/callback", a.handleOIDCCallback)

	mux.Handle("/assets/", a.withAuth(http.HandlerFunc(a.handleAsset)))
	mux.Handle("/", a.withAuth(http.HandlerFunc(a.handleIndex)))
	mux.Handle("/api/status", a.withAuth(http.HandlerFunc(a.handleStatus)))
	mux.Handle("/api/fs/list", a.withAuth(http.HandlerFunc(a.handleFSList)))
	mux.Handle("/api/fs/read", a.withAuth(http.HandlerFunc(a.handleFSRead)))
	mux.Handle("/api/fs/write", a.withAuth(http.HandlerFunc(a.handleFSWrite)))
	mux.Handle("/api/fs/delete", a.withAuth(http.HandlerFunc(a.handleFSDelete)))
	mux.Handle("/api/fs/move", a.withAuth(http.HandlerFunc(a.handleFSMove)))
	mux.Handle("/api/fs/archive", a.withAuth(http.HandlerFunc(a.handleFSArchive)))
	mux.Handle("/api/fs/extract", a.withAuth(http.HandlerFunc(a.handleFSExtract)))
	mux.Handle("/api/fs/mkdir", a.withAuth(http.HandlerFunc(a.handleFSMkdir)))
	mux.Handle("/api/fs/upload", a.withAuth(http.HandlerFunc(a.handleFSUpload)))
	mux.Handle("/api/fs/download", a.withAuth(http.HandlerFunc(a.handleFSDownload)))
	mux.Handle("/api/process/start", a.withAuth(http.HandlerFunc(a.handleProcessStart)))
	mux.Handle("/api/process/stop", a.withAuth(http.HandlerFunc(a.handleProcessStop)))
	mux.Handle("/api/process/restart", a.withAuth(http.HandlerFunc(a.handleProcessRestart)))
	mux.Handle("/api/process/input", a.withAuth(http.HandlerFunc(a.handleProcessInput)))
	mux.Handle("/api/console/ws", a.withAuth(http.HandlerFunc(a.handleConsoleWS)))
	return secureHeaders(mux)
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (a *app) authEnabled() bool {
	return (a.cfg.Username != "" && a.cfg.Password != "") || a.oidcEnabled()
}

func (a *app) oidcEnabled() bool {
	return a.cfg.OIDC.IssuerURL != "" && a.cfg.OIDC.ClientID != ""
}

func (a *app) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authEnabled() {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := a.sessions.User(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		if a.checkBasicAuth(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

func (a *app) checkBasicAuth(r *http.Request) bool {
	if a.cfg.Username == "" || a.cfg.Password == "" {
		return false
	}
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	return constantStringEqual(user, a.cfg.Username) && constantStringEqual(pass, a.cfg.Password)
}

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, indexHTML)
}

func (a *app) handleAsset(w http.ResponseWriter, r *http.Request) {
	var body string
	switch r.URL.Path {
	case "/assets/index.js":
		body = indexJS
	case "/assets/theme.js":
		body = themeJS
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = io.WriteString(w, body)
}

func (a *app) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if !a.authEnabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.ReplaceAll(loginHTML, "{{PASSWORD_ENABLED}}", strconv.FormatBool(a.cfg.Username != "" && a.cfg.Password != ""))
	page = strings.ReplaceAll(page, "{{OIDC_ENABLED}}", strconv.FormatBool(a.oidcEnabled()))
	_, _ = io.WriteString(w, page)
}

func (a *app) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"process": a.sup.Status(),
		"root":    "/",
		"auth": map[string]any{
			"enabled": a.authEnabled(),
			"oidc":    a.oidcEnabled(),
		},
	})
}
