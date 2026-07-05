package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

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
