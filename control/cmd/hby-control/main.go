package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const version = "0.1.0"

func main() {
	log.SetFlags(0)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("hby-control: %v", err)
	}

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "run":
		cmdline := commandArgs(os.Args[2:])
		if len(cmdline) == 0 {
			log.Fatal("hby-control: run requires a command")
		}
		if !cfg.Enabled {
			os.Exit(runDirect(cfg, cmdline))
		}
		os.Exit(runSupervisor(cfg, cmdline))
	case "serve":
		if !cfg.Enabled {
			log.Print("hby-control: disabled by HBY_CONTROL_ENABLED=false")
			return
		}
		os.Exit(runSupervisor(cfg, nil))
	case "version":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  hby-control run -- <command> [args...]")
	fmt.Fprintln(os.Stderr, "  hby-control serve")
	fmt.Fprintln(os.Stderr, "  hby-control version")
}

func commandArgs(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}

type config struct {
	Enabled             bool
	Addr                string
	Root                string
	RootReal            string
	TLSCertFile         string
	TLSKeyFile          string
	Username            string
	Password            string
	SessionKey          []byte
	OIDC                oidcConfig
	StopTimeout         time.Duration
	AutoStart           bool
	MaxReadBytes        int64
	MaxUploadBytes      int64
	LogBytes            int
	SessionCookieSecure bool
}

type oidcConfig struct {
	IssuerURL      string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	Scopes         []string
	AuthMethod     string
	AllowedEmails  map[string]struct{}
	AllowedDomains map[string]struct{}
	AllowedGroups  map[string]struct{}
}

type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

func loadConfig() (config, error) {
	root := firstEnv("/home/container", "HBY_CONTROL_ROOT", "HBY_WEBUI_ROOT")
	if _, err := os.Stat(root); err != nil {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			root = cwd
		}
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return config{}, err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		rootReal = rootAbs
	}

	stopTimeout, err := time.ParseDuration(firstEnv("30s", "HBY_CONTROL_STOP_TIMEOUT"))
	if err != nil {
		return config{}, fmt.Errorf("invalid HBY_CONTROL_STOP_TIMEOUT: %w", err)
	}

	sessionKey := []byte(firstEnv("", "HBY_CONTROL_SESSION_KEY"))
	if len(sessionKey) == 0 {
		sessionKey = randomBytes(32)
	}

	scopes := strings.Fields(firstEnv("openid profile email", "HBY_CONTROL_OIDC_SCOPES"))
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}

	cfg := config{
		Enabled:             boolEnv(true, "HBY_CONTROL_ENABLED"),
		Addr:                firstEnv(":8080", "HBY_CONTROL_ADDR", "HBY_WEBUI_ADDR"),
		Root:                rootAbs,
		RootReal:            rootReal,
		TLSCertFile:         firstEnv("", "HBY_CONTROL_TLS_CERT_FILE", "HBY_WEBUI_TLS_CERT_FILE"),
		TLSKeyFile:          firstEnv("", "HBY_CONTROL_TLS_KEY_FILE", "HBY_WEBUI_TLS_KEY_FILE"),
		Username:            firstEnv("", "HBY_CONTROL_USERNAME", "HBY_WEBUI_USERNAME"),
		Password:            firstEnv("", "HBY_CONTROL_PASSWORD", "HBY_WEBUI_PASSWORD"),
		SessionKey:          sessionKey,
		StopTimeout:         stopTimeout,
		AutoStart:           boolEnv(true, "HBY_CONTROL_AUTO_START"),
		MaxReadBytes:        int64Env(1024*1024, "HBY_CONTROL_MAX_READ_BYTES"),
		MaxUploadBytes:      int64Env(64*1024*1024, "HBY_CONTROL_MAX_UPLOAD_BYTES"),
		LogBytes:            int(int64Env(512*1024, "HBY_CONTROL_LOG_BYTES")),
		SessionCookieSecure: boolEnv(false, "HBY_CONTROL_SECURE_COOKIES"),
		OIDC: oidcConfig{
			IssuerURL:      strings.TrimRight(firstEnv("", "HBY_CONTROL_OIDC_ISSUER_URL"), "/"),
			ClientID:       firstEnv("", "HBY_CONTROL_OIDC_CLIENT_ID"),
			ClientSecret:   firstEnv("", "HBY_CONTROL_OIDC_CLIENT_SECRET"),
			RedirectURL:    firstEnv("", "HBY_CONTROL_OIDC_REDIRECT_URL"),
			Scopes:         scopes,
			AuthMethod:     firstEnv("client_secret_basic", "HBY_CONTROL_OIDC_AUTH_METHOD"),
			AllowedEmails:  csvSet(firstEnv("", "HBY_CONTROL_OIDC_ALLOWED_EMAILS")),
			AllowedDomains: csvSet(firstEnv("", "HBY_CONTROL_OIDC_ALLOWED_DOMAINS")),
			AllowedGroups:  csvSet(firstEnv("", "HBY_CONTROL_OIDC_ALLOWED_GROUPS")),
		},
	}

	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		return config{}, errors.New("both HBY_CONTROL_TLS_CERT_FILE and HBY_CONTROL_TLS_KEY_FILE must be set for HTTPS")
	}
	return cfg, nil
}

func firstEnv(def string, names ...string) string {
	for _, name := range names {
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
	}
	return def
}

func boolEnv(def bool, names ...string) bool {
	v := strings.ToLower(strings.TrimSpace(firstEnv("", names...)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func int64Env(def int64, names ...string) int64 {
	v := strings.TrimSpace(firstEnv("", names...))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func csvSet(v string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(v, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			out[part] = struct{}{}
		}
	}
	return out
}

func runDirect(cfg config, cmdline []string) int {
	if runtime.GOOS != "windows" {
		if err := os.Chdir(cfg.Root); err != nil {
			log.Printf("hby-control: %v", err)
			return 1
		}
		resolved, err := exec.LookPath(cmdline[0])
		if err != nil {
			log.Printf("hby-control: %v", err)
			return 127
		}
		if err := syscall.Exec(resolved, cmdline, os.Environ()); err != nil {
			log.Printf("hby-control: %v", err)
			return 1
		}
		return 0
	}

	cmd := exec.Command(cmdline[0], cmdline[1:]...)
	cmd.Dir = cfg.Root
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		log.Printf("hby-control: %v", err)
		return 1
	}
	return 0
}

func runSupervisor(cfg config, cmdline []string) int {
	supervisor := newSupervisor(cmdline, cfg.Root, cfg.StopTimeout, cfg.LogBytes)
	app := newApp(cfg, supervisor)

	if len(cmdline) > 0 && cfg.AutoStart {
		if err := supervisor.Start(); err != nil {
			log.Printf("hby-control: unable to start server command: %v", err)
		}
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.routes(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		proto := "http"
		if cfg.TLSCertFile != "" {
			proto = "https"
		}
		log.Printf("hby-control: listening on %s://%s, root=%s", proto, cfg.Addr, cfg.Root)
		if cfg.TLSCertFile != "" {
			errCh <- srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
			return
		}
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("hby-control: received %s, shutting down", sig)
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("hby-control: http server failed: %v", err)
			return 1
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	_ = supervisor.Stop()
	return 0
}

type supervisor struct {
	mu          sync.Mutex
	cmdline     []string
	dir         string
	stopTimeout time.Duration
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	cleanup     func()
	waitCh      chan struct{}
	running     bool
	startedAt   time.Time
	stoppedAt   time.Time
	exitCode    int
	exitError   string
	logs        *logHub
}

func newSupervisor(cmdline []string, dir string, stopTimeout time.Duration, maxLogBytes int) *supervisor {
	return &supervisor{
		cmdline:     append([]string(nil), cmdline...),
		dir:         dir,
		stopTimeout: stopTimeout,
		exitCode:    -1,
		logs:        newLogHub(maxLogBytes),
	}
}

func (s *supervisor) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.cmdline) == 0 {
		return errors.New("no server command configured")
	}
	if s.running {
		return errors.New("server command is already running")
	}

	cmd := exec.Command(s.cmdline[0], s.cmdline[1:]...)
	cmd.Dir = s.dir
	cmd.Env = os.Environ()

	processIO, err := setupProcessIO(cmd)
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		processIO.cleanup()
		return err
	}
	processIO.afterStart()

	waitCh := make(chan struct{})
	s.cmd = cmd
	s.stdin = processIO.stdin
	s.cleanup = processIO.cleanup
	s.waitCh = waitCh
	s.running = true
	s.startedAt = time.Now()
	s.stoppedAt = time.Time{}
	s.exitCode = -1
	s.exitError = ""
	s.logs.Append([]byte(fmt.Sprintf("\n[hby-control] started server: %s\n", shellJoin(s.cmdline))))

	for _, reader := range processIO.readers {
		go s.copyOutput(reader)
	}
	go s.wait(cmd, waitCh)
	return nil
}

func (s *supervisor) Stop() error {
	s.mu.Lock()
	if !s.running || s.cmd == nil || s.cmd.Process == nil {
		s.mu.Unlock()
		return nil
	}
	cmd := s.cmd
	waitCh := s.waitCh
	s.mu.Unlock()

	s.logs.Append([]byte("\n[hby-control] stopping server\n"))
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Signal(os.Interrupt)
	} else {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}

	select {
	case <-waitCh:
		return nil
	case <-time.After(s.stopTimeout):
		s.logs.Append([]byte("\n[hby-control] stop timeout reached, killing server\n"))
		if runtime.GOOS == "windows" {
			_ = cmd.Process.Kill()
		} else {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-waitCh
		return nil
	}
}

func (s *supervisor) Restart() error {
	if err := s.Stop(); err != nil {
		return err
	}
	return s.Start()
}

func (s *supervisor) SendInput(data string) error {
	s.mu.Lock()
	stdin := s.stdin
	running := s.running
	s.mu.Unlock()

	if !running || stdin == nil {
		return errors.New("server command is not running")
	}
	_, err := io.WriteString(stdin, data)
	return err
}

func (s *supervisor) Status() processStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return processStatus{
		Configured: len(s.cmdline) > 0,
		Running:    s.running,
		Command:    shellJoin(s.cmdline),
		StartedAt:  s.startedAt,
		StoppedAt:  s.stoppedAt,
		ExitCode:   s.exitCode,
		ExitError:  s.exitError,
	}
}

func (s *supervisor) copyOutput(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.logs.Append(chunk)
		}
		if err != nil {
			return
		}
	}
}

func (s *supervisor) wait(cmd *exec.Cmd, waitCh chan struct{}) {
	err := cmd.Wait()
	exitCode := 0
	exitText := ""
	if err != nil {
		exitText = err.Error()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	s.mu.Lock()
	var cleanup func()
	if s.cmd == cmd {
		cleanup = s.cleanup
		s.running = false
		s.cmd = nil
		s.stdin = nil
		s.cleanup = nil
		s.stoppedAt = time.Now()
		s.exitCode = exitCode
		s.exitError = exitText
	}
	s.mu.Unlock()

	if cleanup != nil {
		cleanup()
	}
	s.logs.Append([]byte(fmt.Sprintf("\n[hby-control] server exited with code %d\n", exitCode)))
	close(waitCh)
}

type processIO struct {
	stdin      io.WriteCloser
	readers    []io.Reader
	afterStart func()
	cleanup    func()
}

func setupPipeProcessIO(cmd *exec.Cmd) (*processIO, error) {
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	return &processIO{
		stdin:      stdin,
		readers:    []io.Reader{stdout, stderr},
		afterStart: func() {},
		cleanup:    func() {},
	}, nil
}

type processStatus struct {
	Configured bool      `json:"configured"`
	Running    bool      `json:"running"`
	Command    string    `json:"command"`
	StartedAt  time.Time `json:"startedAt,omitempty"`
	StoppedAt  time.Time `json:"stoppedAt,omitempty"`
	ExitCode   int       `json:"exitCode"`
	ExitError  string    `json:"exitError,omitempty"`
}

type logHub struct {
	mu   sync.Mutex
	max  int
	buf  []byte
	subs map[chan []byte]struct{}
}

func newLogHub(max int) *logHub {
	if max <= 0 {
		max = 512 * 1024
	}
	return &logHub{max: max, subs: map[chan []byte]struct{}{}}
}

func (h *logHub) Append(data []byte) {
	if len(data) == 0 {
		return
	}
	h.mu.Lock()
	h.buf = append(h.buf, data...)
	if len(h.buf) > h.max {
		h.buf = append([]byte(nil), h.buf[len(h.buf)-h.max:]...)
	}
	for ch := range h.subs {
		select {
		case ch <- append([]byte(nil), data...):
		default:
		}
	}
	h.mu.Unlock()
}

func (h *logHub) Snapshot() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]byte(nil), h.buf...)
}

func (h *logHub) Subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 32)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		close(ch)
		h.mu.Unlock()
	}
}

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

	mux.Handle("/", a.withAuth(http.HandlerFunc(a.handleIndex)))
	mux.Handle("/api/status", a.withAuth(http.HandlerFunc(a.handleStatus)))
	mux.Handle("/api/fs/list", a.withAuth(http.HandlerFunc(a.handleFSList)))
	mux.Handle("/api/fs/read", a.withAuth(http.HandlerFunc(a.handleFSRead)))
	mux.Handle("/api/fs/write", a.withAuth(http.HandlerFunc(a.handleFSWrite)))
	mux.Handle("/api/fs/delete", a.withAuth(http.HandlerFunc(a.handleFSDelete)))
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

func (a *app) handleFSList(w http.ResponseWriter, r *http.Request) {
	full, display, err := a.resolveExisting(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !info.IsDir() {
		writeJSON(w, http.StatusOK, map[string]any{"path": display, "entries": []fileEntry{entryFor(display, info)}})
		return
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	files := make([]fileEntry, 0, len(entries))
	for _, ent := range entries {
		info, err := ent.Info()
		if err != nil {
			continue
		}
		p := path.Join(display, ent.Name())
		if display == "/" {
			p = "/" + ent.Name()
		}
		files = append(files, entryFor(p, info))
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	writeJSON(w, http.StatusOK, map[string]any{"path": display, "entries": files})
}

func (a *app) handleFSRead(w http.ResponseWriter, r *http.Request) {
	full, display, err := a.resolveExisting(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "cannot read a directory")
		return
	}
	if info.Size() > a.cfg.MaxReadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large for the editor; use download instead")
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	encoding := "utf-8"
	content := string(data)
	if !utf8.Valid(data) {
		encoding = "base64"
		content = base64.StdEncoding.EncodeToString(data)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":     display,
		"name":     filepath.Base(full),
		"size":     info.Size(),
		"mode":     info.Mode().Perm().String(),
		"encoding": encoding,
		"content":  content,
	})
}

func (a *app) handleFSWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	full, display, err := a.resolveForWrite(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data := []byte(req.Content)
	if req.Encoding == "base64" {
		data, err = base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid base64 content")
			return
		}
	}
	mode := os.FileMode(0o640)
	if info, err := os.Stat(full); err == nil {
		if info.IsDir() {
			writeError(w, http.StatusBadRequest, "cannot write a directory")
			return
		}
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(full, data, mode); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": display, "size": len(data)})
}

func (a *app) handleFSDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	full, display, err := a.resolveExisting(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if display == "/" {
		writeError(w, http.StatusBadRequest, "refusing to delete the root directory")
		return
	}
	if err := os.RemoveAll(full); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": display})
}

func (a *app) handleFSMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	full, display, err := a.resolveForWrite(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(full, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": display})
}

func (a *app) handleFSUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxUploadBytes)
	if err := r.ParseMultipartForm(a.cfg.MaxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid upload")
		return
	}
	dir, _, err := a.resolveExisting(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "upload path must be a directory")
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "missing upload file")
		return
	}
	uploaded := make([]string, 0, len(files))
	for _, fh := range files {
		if err := a.saveUploadedFile(dir, fh); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		uploaded = append(uploaded, fh.Filename)
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploaded": uploaded})
}

func (a *app) saveUploadedFile(dir string, fh *multipart.FileHeader) error {
	name := filepath.Base(fh.Filename)
	if name == "." || name == string(filepath.Separator) {
		return errors.New("invalid file name")
	}
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dstPath := filepath.Join(dir, name)
	if err := a.ensureWithinRootForExistingOrParent(dstPath); err != nil {
		return err
	}
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

func (a *app) handleFSDownload(w http.ResponseWriter, r *http.Request) {
	full, _, err := a.resolveExisting(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "cannot download a directory")
		return
	}
	http.ServeFile(w, r, full)
}

func (a *app) handleProcessStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := a.sup.Start(); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.sup.Status())
}

func (a *app) handleProcessStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := a.sup.Stop(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.sup.Status())
}

func (a *app) handleProcessRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := a.sup.Restart(); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.sup.Status())
}

func (a *app) handleProcessInput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.sup.SendInput(req.Data); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": len(req.Data)})
}

func (a *app) handleConsoleWS(w http.ResponseWriter, r *http.Request) {
	conn, reader, err := upgradeWebSocket(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer conn.Close()

	writer := &wsWriter{conn: conn}
	if snapshot := a.sup.logs.Snapshot(); len(snapshot) > 0 {
		_ = writer.WriteJSON(map[string]string{"type": "output", "data": string(snapshot)})
	}

	ch, unsubscribe := a.sup.logs.Subscribe()
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for data := range ch {
			if err := writer.WriteJSON(map[string]string{"type": "output", "data": string(data)}); err != nil {
				return
			}
		}
	}()

	for {
		op, payload, err := readWSFrame(reader)
		if err != nil {
			return
		}
		switch op {
		case wsOpcodeClose:
			_ = writer.WriteClose()
			return
		case wsOpcodePing:
			_ = writer.WritePong(payload)
		case wsOpcodeText:
			var msg struct {
				Type string `json:"type"`
				Data string `json:"data"`
			}
			if json.Unmarshal(payload, &msg) == nil && msg.Type == "input" {
				if err := a.sup.SendInput(msg.Data); err != nil {
					_ = writer.WriteJSON(map[string]string{"type": "error", "data": err.Error()})
				}
			}
		}
		select {
		case <-done:
			return
		default:
		}
	}
}

type fileEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"modTime"`
}

func entryFor(display string, info os.FileInfo) fileEntry {
	return fileEntry{
		Name:    info.Name(),
		Path:    display,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		Mode:    info.Mode().Perm().String(),
		ModTime: info.ModTime(),
	}
}

func (a *app) resolveExisting(input string) (string, string, error) {
	full, display, err := a.resolveClean(input)
	if err != nil {
		return "", "", err
	}
	if err := a.ensureWithinRootExisting(full); err != nil {
		return "", "", err
	}
	return full, display, nil
}

func (a *app) resolveForWrite(input string) (string, string, error) {
	full, display, err := a.resolveClean(input)
	if err != nil {
		return "", "", err
	}
	if display == "/" {
		return "", "", errors.New("refusing to write the root directory")
	}
	if err := a.ensureWithinRootForExistingOrParent(full); err != nil {
		return "", "", err
	}
	return full, display, nil
}

func (a *app) resolveClean(input string) (string, string, error) {
	input = strings.ReplaceAll(input, "\\", "/")
	clean := path.Clean("/" + strings.TrimPrefix(input, "/"))
	if clean == "." {
		clean = "/"
	}
	rel := strings.TrimPrefix(clean, "/")
	full := filepath.Clean(filepath.Join(a.cfg.Root, filepath.FromSlash(rel)))
	rootClean := filepath.Clean(a.cfg.Root)
	if full != rootClean {
		relToRoot, err := filepath.Rel(rootClean, full)
		if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
			return "", "", errors.New("path escapes configured root")
		}
	}
	return full, clean, nil
}

func (a *app) ensureWithinRootExisting(full string) error {
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return err
	}
	return a.ensureWithinRootReal(real)
}

func (a *app) ensureWithinRootForExistingOrParent(full string) error {
	if _, err := os.Lstat(full); err == nil {
		return a.ensureWithinRootExisting(full)
	}
	parent := filepath.Dir(full)
	real, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	return a.ensureWithinRootReal(real)
}

func (a *app) ensureWithinRootReal(real string) error {
	root := filepath.Clean(a.cfg.RootReal)
	real = filepath.Clean(real)
	if real == root {
		return nil
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("path escapes configured root")
	}
	return nil
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

type stateStore struct {
	mu     sync.Mutex
	values map[string]oidcState
}

type oidcState struct {
	RedirectURL string
	ExpiresAt   time.Time
}

func newStateStore() *stateStore {
	return &stateStore{values: map[string]oidcState{}}
}

func (s *stateStore) Put(redirect string) string {
	token := randomToken(32)
	s.mu.Lock()
	s.values[token] = oidcState{RedirectURL: redirect, ExpiresAt: time.Now().Add(10 * time.Minute)}
	s.mu.Unlock()
	return token
}

func (s *stateStore) Take(token string) (oidcState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.values[token]
	delete(s.values, token)
	if !ok || time.Now().After(state.ExpiresAt) {
		return oidcState{}, false
	}
	return state, true
}

func (a *app) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if !a.oidcEnabled() {
		writeError(w, http.StatusNotFound, "OIDC is not enabled")
		return
	}
	discovery, err := a.discoverOIDC(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	redirectURL := a.oidcRedirectURL(r)
	state := a.states.Put("/")
	values := url.Values{}
	values.Set("client_id", a.cfg.OIDC.ClientID)
	values.Set("redirect_uri", redirectURL)
	values.Set("response_type", "code")
	values.Set("scope", strings.Join(a.cfg.OIDC.Scopes, " "))
	values.Set("state", state)
	http.Redirect(w, r, discovery.AuthorizationEndpoint+"?"+values.Encode(), http.StatusFound)
}

func (a *app) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if !a.oidcEnabled() {
		writeError(w, http.StatusNotFound, "OIDC is not enabled")
		return
	}
	state, ok := a.states.Take(r.URL.Query().Get("state"))
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid OIDC state")
		return
	}
	if errText := r.URL.Query().Get("error"); errText != "" {
		writeError(w, http.StatusUnauthorized, errText)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing OIDC code")
		return
	}
	discovery, err := a.discoverOIDC(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	token, err := a.exchangeOIDCCode(r.Context(), discovery, a.oidcRedirectURL(r), code)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	user, err := a.fetchOIDCUser(r.Context(), discovery, token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	a.sessions.Set(w, r, user, a.cookieSecure(r))
	if state.RedirectURL == "" {
		state.RedirectURL = "/"
	}
	http.Redirect(w, r, state.RedirectURL, http.StatusFound)
}

func (a *app) discoverOIDC(ctx context.Context) (oidcDiscovery, error) {
	a.oidcMu.Lock()
	if a.oidcDiscovery.AuthorizationEndpoint != "" {
		discovery := a.oidcDiscovery
		a.oidcMu.Unlock()
		return discovery, nil
	}
	a.oidcMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.OIDC.IssuerURL+"/.well-known/openid-configuration", nil)
	if err != nil {
		return oidcDiscovery{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oidcDiscovery{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return oidcDiscovery{}, fmt.Errorf("OIDC discovery failed: %s", resp.Status)
	}
	var discovery oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return oidcDiscovery{}, err
	}
	if discovery.AuthorizationEndpoint == "" || discovery.TokenEndpoint == "" || discovery.UserinfoEndpoint == "" {
		return oidcDiscovery{}, errors.New("OIDC discovery document is missing required endpoints")
	}

	a.oidcMu.Lock()
	a.oidcDiscovery = discovery
	a.oidcMu.Unlock()
	return discovery, nil
}

func (a *app) exchangeOIDCCode(ctx context.Context, discovery oidcDiscovery, redirectURL, code string) (string, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", redirectURL)
	values.Set("client_id", a.cfg.OIDC.ClientID)
	if a.cfg.OIDC.ClientSecret != "" && a.cfg.OIDC.AuthMethod == "client_secret_post" {
		values.Set("client_secret", a.cfg.OIDC.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, discovery.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if a.cfg.OIDC.ClientSecret != "" && a.cfg.OIDC.AuthMethod != "client_secret_post" {
		req.SetBasicAuth(a.cfg.OIDC.ClientID, a.cfg.OIDC.ClientSecret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("OIDC token exchange failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.AccessToken == "" {
		return "", errors.New("OIDC token response did not include an access token")
	}
	return payload.AccessToken, nil
}

func (a *app) fetchOIDCUser(ctx context.Context, discovery oidcDiscovery, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discovery.UserinfoEndpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("OIDC userinfo failed: %s", resp.Status)
	}
	var claims map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return "", err
	}

	email := claimString(claims, "email")
	user := firstNonEmpty(claimString(claims, "preferred_username"), email, claimString(claims, "name"), claimString(claims, "sub"))
	if user == "" {
		return "", errors.New("OIDC userinfo did not include a usable user identifier")
	}
	if err := a.checkOIDCAllowed(email, claims); err != nil {
		return "", err
	}
	return user, nil
}

func (a *app) checkOIDCAllowed(email string, claims map[string]any) error {
	emailLower := strings.ToLower(email)
	if len(a.cfg.OIDC.AllowedEmails) > 0 {
		if _, ok := a.cfg.OIDC.AllowedEmails[emailLower]; !ok {
			return errors.New("OIDC user email is not allowed")
		}
	}
	if len(a.cfg.OIDC.AllowedDomains) > 0 {
		_, domain, ok := strings.Cut(emailLower, "@")
		if !ok {
			return errors.New("OIDC user email does not include a domain")
		}
		if _, allowed := a.cfg.OIDC.AllowedDomains[domain]; !allowed {
			return errors.New("OIDC user email domain is not allowed")
		}
	}
	if len(a.cfg.OIDC.AllowedGroups) > 0 {
		for _, group := range claimStringSlice(claims, "groups") {
			if _, ok := a.cfg.OIDC.AllowedGroups[strings.ToLower(group)]; ok {
				return nil
			}
		}
		return errors.New("OIDC user group is not allowed")
	}
	return nil
}

func (a *app) oidcRedirectURL(r *http.Request) string {
	if a.cfg.OIDC.RedirectURL != "" {
		return a.cfg.OIDC.RedirectURL
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = forwardedHost
	}
	return scheme + "://" + host + "/auth/oidc/callback"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func claimString(claims map[string]any, key string) string {
	v, _ := claims[key].(string)
	return v
}

func claimStringSlice(claims map[string]any, key string) []string {
	raw, ok := claims[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func constantStringEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func randomToken(n int) string {
	return base64.RawURLEncoding.EncodeToString(randomBytes(n))
}

func shellJoin(args []string) string {
	var b strings.Builder
	for i, arg := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		if arg == "" || strings.ContainsAny(arg, " \t\n\"'\\$`!*?[]{}();&|<>") {
			b.WriteByte('\'')
			b.WriteString(strings.ReplaceAll(arg, "'", "'\"'\"'"))
			b.WriteByte('\'')
			continue
		}
		b.WriteString(arg)
	}
	return b.String()
}

const (
	wsGUID        = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	wsOpcodeText  = 1
	wsOpcodeClose = 8
	wsOpcodePing  = 9
	wsOpcodePong  = 10
)

func upgradeWebSocket(w http.ResponseWriter, r *http.Request) (net.Conn, *bufio.Reader, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, nil, errors.New("missing websocket upgrade")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, nil, errors.New("missing websocket key")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("websocket hijacking is not supported")
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	headers := bytes.NewBuffer(nil)
	fmt.Fprintf(headers, "HTTP/1.1 101 Switching Protocols\r\n")
	fmt.Fprintf(headers, "Upgrade: websocket\r\n")
	fmt.Fprintf(headers, "Connection: Upgrade\r\n")
	fmt.Fprintf(headers, "Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	if _, err := conn.Write(headers.Bytes()); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, rw.Reader, nil
}

func readWSFrame(r *bufio.Reader) (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(r, ext); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(r, ext); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}
	if length > 1024*1024 {
		return 0, nil, errors.New("websocket frame too large")
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

type wsWriter struct {
	mu   sync.Mutex
	conn net.Conn
}

func (w *wsWriter) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.writeFrame(wsOpcodeText, data)
}

func (w *wsWriter) WritePong(payload []byte) error {
	return w.writeFrame(wsOpcodePong, payload)
}

func (w *wsWriter) WriteClose() error {
	return w.writeFrame(wsOpcodeClose, nil)
}

func (w *wsWriter) writeFrame(opcode byte, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	header := []byte{0x80 | opcode}
	l := len(payload)
	switch {
	case l < 126:
		header = append(header, byte(l))
	case l <= 65535:
		header = append(header, 126, byte(l>>8), byte(l))
	default:
		header = append(header, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(l))
		header = append(header, ext[:]...)
	}
	if _, err := w.conn.Write(header); err != nil {
		return err
	}
	_, err := w.conn.Write(payload)
	return err
}

const loginHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Server Control</title>
<style>
:root{color-scheme:light dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#f6f7f9;color:#111827}
body{margin:0;min-height:100vh;display:grid;place-items:center;background:linear-gradient(180deg,#eef2f7,#f9fafb)}
.panel{width:min(420px,calc(100vw - 32px));background:#fff;border:1px solid #d7dce3;border-radius:8px;box-shadow:0 18px 45px rgba(15,23,42,.13);padding:24px}
h1{font-size:20px;line-height:1.2;margin:0 0 18px}
label{display:block;font-size:13px;font-weight:650;margin:14px 0 6px}
input{width:100%;box-sizing:border-box;border:1px solid #c9d1dc;border-radius:6px;padding:10px 12px;font:inherit}
button,a.button{display:inline-flex;align-items:center;justify-content:center;gap:8px;box-sizing:border-box;height:40px;border-radius:6px;border:1px solid #111827;background:#111827;color:white;text-decoration:none;font-weight:650;font-size:14px;padding:0 14px;cursor:pointer}
.row{display:flex;gap:10px;align-items:center;margin-top:18px;flex-wrap:wrap}.muted{color:#5b6472;font-size:13px;margin-top:16px}
[hidden]{display:none!important}@media (prefers-color-scheme:dark){:root{background:#111827;color:#e5e7eb}body{background:#0f172a}.panel{background:#172033;border-color:#2d374b}input{background:#111827;border-color:#374151;color:#e5e7eb}button,a.button{background:#e5e7eb;color:#111827;border-color:#e5e7eb}.muted{color:#aab3c2}}
</style>
</head>
<body>
<main class="panel">
<h1>Server Control</h1>
<form method="post" action="/auth/login" id="passwordForm">
<label for="username">Username</label>
<input id="username" name="username" autocomplete="username" required>
<label for="password">Password</label>
<input id="password" name="password" type="password" autocomplete="current-password" required>
<div class="row"><button type="submit">Log In</button></div>
</form>
<div class="row" id="oidcRow"><a class="button" href="/auth/oidc/login">Log In With OIDC</a></div>
<p class="muted">Authentication is enabled by the container environment.</p>
</main>
<script>
if ("{{PASSWORD_ENABLED}}" !== "true") document.getElementById("passwordForm").hidden = true;
if ("{{OIDC_ENABLED}}" !== "true") document.getElementById("oidcRow").hidden = true;
</script>
</body>
</html>`

const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Server Control</title>
<script>
try{const m=localStorage.getItem("hby-control-theme-mode")||localStorage.getItem("hby-control-theme")||"system";const q=matchMedia("(prefers-color-scheme: dark)");document.documentElement.dataset.themeMode=m;document.documentElement.dataset.theme=m==="system"?(q.matches?"dark":"light"):m}catch{}
</script>
<style>
:root{color-scheme:light;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;--bg:#eef2f6;--header:#ffffff;--sidebar:#f8fafc;--surface:#ffffff;--editor:#ffffff;--console:#0b1020;--consoleText:#e5e7eb;--text:#111827;--muted:#5d6876;--border:#d6dee8;--button:#ffffff;--buttonText:#111827;--hover:#e7edf5;--active:#111827;--activeText:#ffffff;--primary:#0f73d9;--danger:#a61b1b;--accent:#19c7c7;--shadow:rgba(15,23,42,.08)}
:root[data-theme="dark"]{color-scheme:dark;--bg:#2f4050;--header:#19232d;--sidebar:#23313e;--surface:#1d2834;--editor:#19222e;--console:#0b1020;--consoleText:#e6edf3;--text:#e6edf3;--muted:#a2b0bd;--border:#3b4d5f;--button:#2b3b4c;--buttonText:#e6edf3;--hover:#304354;--active:#111827;--activeText:#ffffff;--primary:#1687e0;--danger:#fca5a5;--accent:#2dd4d4;--shadow:rgba(0,0,0,.22)}
*{box-sizing:border-box}body{margin:0;height:100vh;overflow:hidden;background:var(--bg);color:var(--text)}.app{display:grid;grid-template-rows:56px 1fr;height:100vh}
header{display:flex;align-items:center;justify-content:space-between;padding:0 16px;border-bottom:1px solid var(--border);background:var(--header);box-shadow:0 1px 0 var(--shadow)}
h1{font-size:16px;margin:0;font-weight:750}.headerActions{display:flex;align-items:center;gap:14px}.status{display:flex;align-items:center;gap:8px;font-size:13px;color:var(--muted)}.dot{width:9px;height:9px;border-radius:50%;background:#9ca3af}.dot.running{background:#22c55e}
.themeControl{display:flex;align-items:center;gap:2px;padding:2px;border:1px solid var(--border);border-radius:6px;background:var(--sidebar)}.themeControl label{position:relative}.themeControl input{position:absolute;opacity:0;pointer-events:none}.themeControl span{height:28px;min-width:52px;border-radius:4px;color:var(--muted);display:flex;align-items:center;justify-content:center;font-size:12px;font-weight:700;padding:0 8px;cursor:pointer}.themeControl input:checked+span{background:var(--active);color:var(--activeText)}.themeControl input:focus-visible+span{outline:2px solid var(--accent);outline-offset:2px}
.main{display:grid;grid-template-columns:minmax(280px,30%) 1fr;min-height:0}.files{border-right:1px solid var(--border);background:var(--sidebar);display:grid;grid-template-rows:auto auto auto 1fr;min-width:0}.modeTabs{display:flex;gap:6px;padding:10px 10px 8px;border-bottom:1px solid var(--border)}.modeTab{flex:1;height:34px}.modeTab.active{border-color:var(--accent);color:var(--text);box-shadow:inset 0 -2px 0 var(--accent)}
.toolbar{display:flex;gap:6px;align-items:center;padding:8px 10px;border-bottom:1px solid var(--border);flex-wrap:wrap}
button,.button{height:32px;min-width:32px;border:1px solid var(--border);background:var(--button);border-radius:6px;color:var(--buttonText);font:inherit;font-size:13px;font-weight:650;padding:0 9px;cursor:pointer;text-decoration:none;display:inline-flex;align-items:center;justify-content:center}
button:hover,.button:hover{background:var(--hover)}button.icon{font-size:16px;padding:0;width:32px}.button.danger,button.danger{border-color:color-mix(in srgb,var(--danger),var(--border) 55%);color:var(--danger)}.button.primary,button.primary{background:var(--primary);color:white;border-color:var(--primary)}
button:disabled{opacity:.45;cursor:not-allowed}.path{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;color:var(--muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding:9px 10px;border-bottom:1px solid var(--border)}
.list{overflow:auto}.entry{display:grid;grid-template-columns:24px 1fr auto;gap:8px;align-items:center;width:100%;height:36px;padding:0 10px;border:0;border-radius:0;background:transparent;text-align:left;font-weight:500}.entry:hover,.entry.active{background:var(--hover)}.entry.active{box-shadow:inset 3px 0 0 var(--accent)}.entry .name{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.entry .size{font-size:12px;color:var(--muted)}
.workspace{display:grid;grid-template-rows:1fr;min-width:0;min-height:0;padding:24px;background:var(--bg)}.editorWrap{min-height:0;display:grid;grid-template-rows:auto 1fr;border:1px solid var(--border);background:var(--surface);box-shadow:0 10px 24px var(--shadow)}.editorHead{display:flex;align-items:center;justify-content:space-between;gap:8px;padding:10px 12px;border-bottom:1px solid var(--border);background:var(--surface)}.filename{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted)}
textarea{width:100%;height:100%;resize:none;border:0;outline:0;padding:14px;font:13px/1.5 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;background:var(--editor);color:var(--text)}.empty{display:grid;place-items:center;color:var(--muted);font-size:14px;background:var(--editor)}
.console{min-height:0;border:1px solid var(--border);background:var(--console);color:var(--consoleText);display:grid;grid-template-rows:auto 1fr auto;box-shadow:0 10px 24px var(--shadow)}.consoleHead{display:flex;align-items:center;justify-content:space-between;gap:8px;padding:10px 12px;border-bottom:1px solid #1f2937}.processControls{display:flex;gap:6px;flex-wrap:wrap}.processControls button{background:#111827;color:#e5e7eb;border-color:#374151}.processControls button.stop{color:#fecaca}
pre{margin:0;padding:12px;overflow:auto;white-space:pre-wrap;word-break:break-word;font:12px/1.45 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}.consoleForm{display:grid;grid-template-columns:1fr auto;gap:8px;padding:10px;border-top:1px solid #1f2937}.consoleForm input{border:1px solid #374151;background:#111827;color:#e5e7eb;border-radius:6px;padding:0 10px;font:13px ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;min-width:0}
input[type=file]{display:none}.toast{position:fixed;right:14px;bottom:14px;background:var(--active);color:var(--activeText);border:1px solid var(--border);border-radius:6px;padding:10px 12px;font-size:13px;box-shadow:0 10px 30px var(--shadow)}.hide{display:none!important}
@media (max-width:760px){body{overflow:auto}.app{height:auto;min-height:100vh}.main{grid-template-columns:1fr;grid-template-rows:42vh minmax(420px,1fr)}.files{border-right:0;border-bottom:1px solid var(--border)}.workspace{padding:10px}.toolbar{gap:5px}button,.button{font-size:12px;padding:0 7px}}
</style>
</head>
<body>
<div class="app">
<header><h1>Server Control</h1><div class="headerActions"><div class="themeControl" role="radiogroup" aria-label="Theme"><label><input id="themeSystem" name="themeMode" type="radio" value="system"><span>System</span></label><label><input id="themeLight" name="themeMode" type="radio" value="light"><span>Light</span></label><label><input id="themeDark" name="themeMode" type="radio" value="dark"><span>Dark</span></label></div><div class="status"><span id="statusDot" class="dot"></span><span id="statusText">Loading</span><form action="/auth/logout" method="post"><button class="icon" title="Log out">⎋</button></form></div></div></header>
<main class="main">
<section class="files">
<div class="modeTabs"><button class="modeTab active" id="editorTab">Editor</button><button class="modeTab" id="consoleTab">Console</button></div>
<div><div class="toolbar">
<button class="icon" id="upBtn" title="Up">↑</button>
<button class="icon" id="refreshBtn" title="Refresh">⟳</button>
<button id="newFileBtn" title="New file">File</button>
<button id="newDirBtn" title="New folder">Folder</button>
<button id="uploadBtn" title="Upload">Upload</button>
<input type="file" id="uploadInput" multiple>
</div><div class="path" id="cwd">/</div></div>
<div class="list" id="fileList"></div>
</section>
<section class="workspace">
<div class="editorWrap" id="editorPane">
<div class="editorHead"><div class="filename" id="filename">No file selected</div><div><a class="button" id="downloadBtn" href="#" download>Download</a> <button class="danger" id="deleteBtn">Delete</button> <button class="primary" id="saveBtn">Save</button></div></div>
<textarea id="editor" spellcheck="false" disabled></textarea>
<div class="empty hide" id="emptyState">Select a text file to view or edit it.</div>
</div>
<section class="console" id="consolePane">
<div class="consoleHead"><strong>Console</strong><div class="processControls"><button id="startBtn">Start</button><button id="restartBtn">Restart</button><button class="stop" id="stopBtn">Stop</button></div></div>
<pre id="consoleOutput"></pre>
<form class="consoleForm" id="consoleForm"><input id="consoleInput" autocomplete="off" placeholder="Send command to server"><button type="submit">Send</button></form>
</section>
</section>
</main>
</div>
<div class="toast hide" id="toast"></div>
<script>
const state={cwd:"/",selected:null,encoding:"utf-8",ws:null};
const $=id=>document.getElementById(id);
const osTheme=window.matchMedia?matchMedia("(prefers-color-scheme: dark)"):{matches:false};
function normalizeThemeMode(mode){return mode==="light"||mode==="dark"||mode==="system"?mode:"system"}
function storedThemeMode(){try{return normalizeThemeMode(localStorage.getItem("hby-control-theme-mode")||localStorage.getItem("hby-control-theme")||"system")}catch{return "system"}}
function resolvedTheme(mode){return mode==="system"?(osTheme.matches?"dark":"light"):mode}
function applyThemeMode(mode){mode=normalizeThemeMode(mode);document.documentElement.dataset.themeMode=mode;document.documentElement.dataset.theme=resolvedTheme(mode);document.querySelectorAll('input[name="themeMode"]').forEach(input=>input.checked=input.value===mode)}
function saveThemeMode(mode){mode=normalizeThemeMode(mode);try{localStorage.setItem("hby-control-theme-mode",mode);localStorage.removeItem("hby-control-theme")}catch{}applyThemeMode(mode)}
applyThemeMode(storedThemeMode());
document.querySelectorAll('input[name="themeMode"]').forEach(input=>input.onchange=e=>{if(e.target.checked)saveThemeMode(e.target.value)});
if(osTheme.addEventListener)osTheme.addEventListener("change",()=>{if(document.documentElement.dataset.themeMode==="system")applyThemeMode("system")});else if(osTheme.addListener)osTheme.addListener(()=>{if(document.documentElement.dataset.themeMode==="system")applyThemeMode("system")});
function toast(msg){const t=$("toast");t.textContent=msg;t.classList.remove("hide");setTimeout(()=>t.classList.add("hide"),2600)}
async function api(path,opts={}){const res=await fetch(path,{headers:{"Content-Type":"application/json",...(opts.headers||{})},...opts});if(res.status===401){location.href="/login";return}const txt=await res.text();let data={};try{data=txt?JSON.parse(txt):{}}catch{}if(!res.ok)throw new Error(data.error||res.statusText);return data}
function fmtSize(n){if(n<1024)return n+" B";if(n<1048576)return (n/1024).toFixed(1)+" KB";return (n/1048576).toFixed(1)+" MB"}
function parentPath(p){if(p==="/")return "/";const parts=p.split("/").filter(Boolean);parts.pop();return "/"+parts.join("/")}
async function loadStatus(){const s=await api("/api/status");const p=s.process;$("statusDot").classList.toggle("running",p.running);$("statusText").textContent=p.running?"Running":"Stopped";$("startBtn").disabled=p.running||!p.configured;$("stopBtn").disabled=!p.running;$("restartBtn").disabled=!p.configured}
async function loadFiles(p=state.cwd){const data=await api("/api/fs/list?path="+encodeURIComponent(p));state.cwd=data.path;$("cwd").textContent=data.path;$("upBtn").disabled=data.path==="/";const list=$("fileList");list.innerHTML="";for(const ent of data.entries){const b=document.createElement("button");b.className="entry"+(state.selected===ent.path?" active":"");b.innerHTML="<span>"+(ent.isDir?"▸":"•")+'</span><span class="name"></span><span class="size">'+(ent.isDir?"":fmtSize(ent.size))+"</span>";b.querySelector(".name").textContent=ent.name;b.onclick=()=>ent.isDir?loadFiles(ent.path):openFile(ent.path);list.appendChild(b)}}
async function openFile(p){show("editor");const data=await api("/api/fs/read?path="+encodeURIComponent(p));state.selected=data.path;state.encoding=data.encoding;$("filename").textContent=data.path;$("downloadBtn").href="/api/fs/download?path="+encodeURIComponent(data.path);$("editor").disabled=data.encoding!=="utf-8";$("editor").value=data.encoding==="utf-8"?data.content:"Binary file. Use Download to inspect it.";$("emptyState").classList.add("hide");$("editor").classList.remove("hide");await loadFiles(state.cwd)}
async function saveFile(){if(!state.selected)return;await api("/api/fs/write",{method:"POST",body:JSON.stringify({path:state.selected,content:$("editor").value,encoding:state.encoding})});toast("Saved");await loadFiles(state.cwd)}
async function deleteSelected(){if(!state.selected)return;if(!confirm("Delete "+state.selected+"?"))return;await api("/api/fs/delete",{method:"POST",body:JSON.stringify({path:state.selected})});state.selected=null;$("editor").value="";$("editor").disabled=true;$("filename").textContent="No file selected";toast("Deleted");await loadFiles(state.cwd)}
async function newFile(){const name=prompt("File name");if(!name)return;const p=(state.cwd==="/" ? "/" : state.cwd+"/")+name;await api("/api/fs/write",{method:"POST",body:JSON.stringify({path:p,content:"",encoding:"utf-8"})});await loadFiles(state.cwd);await openFile(p)}
async function newDir(){const name=prompt("Folder name");if(!name)return;await api("/api/fs/mkdir",{method:"POST",body:JSON.stringify({path:(state.cwd==="/" ? "/" : state.cwd+"/")+name})});await loadFiles(state.cwd)}
async function uploadFiles(files){const fd=new FormData();for(const f of files)fd.append("file",f);const res=await fetch("/api/fs/upload?path="+encodeURIComponent(state.cwd),{method:"POST",body:fd});if(!res.ok){const d=await res.json().catch(()=>({error:res.statusText}));throw new Error(d.error)}toast("Uploaded");await loadFiles(state.cwd)}
async function proc(action){await api("/api/process/"+action,{method:"POST",body:"{}"});await loadStatus()}
function connectConsole(){const proto=location.protocol==="https:"?"wss":"ws";const ws=new WebSocket(proto+"://"+location.host+"/api/console/ws");state.ws=ws;ws.onmessage=e=>{const msg=JSON.parse(e.data);if(msg.type==="output"||msg.type==="error"){const out=$("consoleOutput");out.textContent+=msg.data;out.scrollTop=out.scrollHeight}};ws.onclose=()=>setTimeout(connectConsole,1500)}
$("upBtn").onclick=()=>loadFiles(parentPath(state.cwd));$("refreshBtn").onclick=()=>loadFiles(state.cwd);$("newFileBtn").onclick=()=>newFile().catch(e=>toast(e.message));$("newDirBtn").onclick=()=>newDir().catch(e=>toast(e.message));$("saveBtn").onclick=()=>saveFile().catch(e=>toast(e.message));$("deleteBtn").onclick=()=>deleteSelected().catch(e=>toast(e.message));$("uploadBtn").onclick=()=>$("uploadInput").click();$("uploadInput").onchange=e=>uploadFiles(e.target.files).catch(err=>toast(err.message));
$("startBtn").onclick=()=>proc("start").catch(e=>toast(e.message));$("stopBtn").onclick=()=>proc("stop").catch(e=>toast(e.message));$("restartBtn").onclick=()=>proc("restart").catch(e=>toast(e.message));$("consoleForm").onsubmit=e=>{e.preventDefault();const input=$("consoleInput");if(input.value&&state.ws&&state.ws.readyState===1){state.ws.send(JSON.stringify({type:"input",data:input.value+"\n"}));input.value=""}};
function show(which){const consoleOn=which==="console";$("consolePane").style.display=consoleOn?"grid":"none";$("editorPane").style.display=consoleOn?"none":"grid";$("consoleTab").classList.toggle("active",consoleOn);$("editorTab").classList.toggle("active",!consoleOn)}$("editorTab").onclick=()=>show("editor");$("consoleTab").onclick=()=>show("console");show("editor");
loadFiles("/").catch(e=>toast(e.message));loadStatus().catch(e=>toast(e.message));setInterval(()=>loadStatus().catch(()=>{}),3000);connectConsole();
</script>
</body>
</html>`
