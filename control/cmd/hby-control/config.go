package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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
