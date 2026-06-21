// Package runtime provides built-in OAuth PKCE authentication for providers.
//
// This implementation is written from scratch using only the Go standard
// library. It performs a local-callback PKCE flow and caches the resulting
// access token on disk. Callers must supply OpenBrowser if they want the
// system browser opened automatically; otherwise the authorization URL is
// printed to stderr.
package runtime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/browser"
)

const (
	oauthDefaultTimeout = 5 * time.Minute
	tokenCacheDir        = "oauth"
)

// OAuthPKCEResolver is the built-in AuthResolver for AuthTypeOAuthPKCE.
// It is registered by RegisterBuiltinClasses so that providers configured
// with auth.type = "oauth_pkce" can obtain bearer tokens automatically.
type OAuthPKCEResolver struct {
	// CacheDir overrides the directory used to store access tokens.
	// Defaults to os.UserConfigDir()/ai-sdk/oauth.
	CacheDir string

	// OpenBrowser wraps browser.OpenURL. Tests can override it.
	OpenBrowser func(url string) error
}

// Resolve implements AuthResolver.
func (r *OAuthPKCEResolver) Resolve(ctx context.Context, cfg ProviderConfig) (AuthResult, error) {
	if cfg.Auth.AuthorizeURL == "" {
		return AuthResult{}, fmt.Errorf("oauth_pkce: authorize_url is required for provider %q", cfg.ID)
	}
	if cfg.Auth.TokenURL == "" {
		return AuthResult{}, fmt.Errorf("oauth_pkce: token_url is required for provider %q", cfg.ID)
	}
	if cfg.Auth.ClientID == "" {
		return AuthResult{}, fmt.Errorf("oauth_pkce: client_id is required for provider %q", cfg.ID)
	}

	cached, err := r.loadCachedToken(cfg)
	if err == nil && cached.AccessToken != "" && time.Now().Before(cached.Expiry) {
		slog.Debug("using cached OAuth token", "provider", cfg.ID, "expires", cached.Expiry.Format(time.RFC3339))
		return AuthResult{Token: cached.AccessToken}, nil
	}

	token, err := r.runBrowserAuthFlow(ctx, cfg)
	if err != nil {
		return AuthResult{}, err
	}

	if err := r.saveCachedToken(cfg, token); err != nil {
		slog.Warn("could not cache OAuth token", "provider", cfg.ID, "err", err)
	}
	return AuthResult{Token: token}, nil
}

func (r *OAuthPKCEResolver) runBrowserAuthFlow(ctx context.Context, cfg ProviderConfig) (string, error) {
	pkce, err := newPKCEPair()
	if err != nil {
		return "", fmt.Errorf("oauth_pkce: generating PKCE: %w", err)
	}
	state, err := randomURLSafe(16)
	if err != nil {
		return "", fmt.Errorf("oauth_pkce: generating state: %w", err)
	}

	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("oauth_pkce: binding callback listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	params := url.Values{
		"client_id":             {cfg.Auth.ClientID},
		"response_type":         {"code"},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {"S256"},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
	}
	if idp := strings.TrimSpace(cfg.Auth.IDP); idp != "" {
		params.Set("idp", idp)
	}
	authURL := strings.TrimSpace(cfg.Auth.AuthorizeURL) + "?" + params.Encode()

	open := r.OpenBrowser
	if open == nil {
		open = browser.OpenURL
	}
	slog.Debug("opening browser for OAuth login", "provider", cfg.ID)
	if err := open(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser. Visit:\n  %s\n", authURL)
	}

	type callbackResult struct {
		code string
		err  error
	}
	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, req *http.Request) {
		query := req.URL.Query()
		code := query.Get("code")
		callbackState := query.Get("state")
		errDesc := query.Get("error_description")
		if errDesc == "" {
			errDesc = query.Get("error")
		}

		if code != "" && callbackState == state {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><h2>Login complete — you can close this window.</h2><script>window.close();</script></body></html>`)
			resultCh <- callbackResult{code: code}
			return
		}

		msg := "state mismatch"
		if errDesc != "" {
			msg = errDesc
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `<html><body><h2>Login failed: %s</h2></body></html>`, msg)
		resultCh <- callbackResult{err: fmt.Errorf("OAuth callback error: %s", msg)}
	})

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("callback server stopped unexpectedly", "err", err)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, oauthDefaultTimeout)
	defer cancel()

	var result callbackResult
	select {
	case result = <-resultCh:
	case <-ctx.Done():
		return "", fmt.Errorf("oauth_pkce: login timed out (waited %s)", oauthDefaultTimeout)
	}
	_ = server.Close()

	if result.err != nil {
		return "", result.err
	}
	return r.exchangeCode(ctx, cfg, result.code, redirectURI, pkce.Verifier)
}

func (r *OAuthPKCEResolver) exchangeCode(ctx context.Context, cfg ProviderConfig, code, redirectURI, verifier string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}

	if cfg.Auth.TokenAuthMethod != "basic" {
		form.Set("client_id", cfg.Auth.ClientID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Auth.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("oauth_pkce: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	if cfg.Auth.TokenAuthMethod == "basic" {
		req.SetBasicAuth(cfg.Auth.ClientID, "")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth_pkce: token exchange: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("oauth_pkce: reading token response: %w", err)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("oauth_pkce: decoding token response: %w", err)
	}

	if tokenResp.Error != "" {
		return "", fmt.Errorf("oauth_pkce: token exchange returned %s: %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return "", errors.New("oauth_pkce: token exchange returned no access_token")
	}

	slog.Debug("OAuth token obtained", "provider", cfg.ID)
	return tokenResp.AccessToken, nil
}

type tokenCache struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

func (r *OAuthPKCEResolver) cacheDir() string {
	if r.CacheDir != "" {
		return r.CacheDir
	}
	dir, _ := os.UserConfigDir()
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "ai-sdk")
	}
	return filepath.Join(dir, tokenCacheDir)
}

func (r *OAuthPKCEResolver) cacheFilePath(cfg ProviderConfig) string {
	key := cfg.ID + "\x00" + cfg.Auth.AuthorizeURL + "\x00" + cfg.Auth.TokenURL + "\x00" + cfg.Auth.ClientID
	hash := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(hash[:16]) + ".json"
	return filepath.Join(r.cacheDir(), name)
}

func (r *OAuthPKCEResolver) loadCachedToken(cfg ProviderConfig) (*tokenCache, error) {
	data, err := os.ReadFile(r.cacheFilePath(cfg))
	if err != nil {
		return nil, err
	}
	var cache tokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func (r *OAuthPKCEResolver) saveCachedToken(cfg ProviderConfig, accessToken string) error {
	dir := r.cacheDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	cache := &tokenCache{
		AccessToken: accessToken,
		Expiry:      time.Now().Add(23 * time.Hour),
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(r.cacheFilePath(cfg), data, 0o600)
}

func newPKCEPair() (*pkcePair, error) {
	verifier, err := randomURLSafe(32)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	return &pkcePair{Verifier: verifier, Challenge: challenge}, nil
}

func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type pkcePair struct {
	Verifier  string
	Challenge string
}
