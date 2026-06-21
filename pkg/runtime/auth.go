package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// AuthType names a provider authentication strategy.
type AuthType string

// Built-in auth types.
const (
	AuthTypeNone      AuthType = "none"
	AuthTypeAPIKey    AuthType = "api_key"
	AuthTypeOAuthPKCE AuthType = "oauth_pkce"
)

// AuthConfig describes how to obtain a bearer token or API key for a
// provider. Not all fields are used by every AuthType.
type AuthConfig struct {
	Type            AuthType `json:"type,omitempty"`
	APIKeyEnv       string   `json:"api_key_env,omitempty"`
	APIKey          string   `json:"api_key,omitempty"`
	AuthorizeURL    string   `json:"authorize_url,omitempty"`
	TokenURL        string   `json:"token_url,omitempty"`
	ClientID        string   `json:"client_id,omitempty"`
	IDP             string   `json:"idp,omitempty"`
	TokenAuthMethod string   `json:"token_auth_method,omitempty"`
}

// AuthResult is the resolved credential for a provider. It may be empty
// for providers that require no authentication.
type AuthResult struct {
	Token   string
	Headers map[string]string
}

// IsEmpty reports whether the result carries no token or headers.
func (r AuthResult) IsEmpty() bool {
	return strings.TrimSpace(r.Token) == "" && len(r.Headers) == 0
}

// AuthResolver resolves an AuthConfig into a credential at call time.
// Custom resolvers can be registered per class to handle bespoke auth
// flows (OAuth device code, short-lived token exchange, etc.).
type AuthResolver interface {
	// Resolve returns a credential for the given provider configuration.
	Resolve(ctx context.Context, cfg ProviderConfig) (AuthResult, error)
}

// AuthResolverFunc adapts a function to the AuthResolver interface.
type AuthResolverFunc func(ctx context.Context, cfg ProviderConfig) (AuthResult, error)

// Resolve implements AuthResolver.
func (f AuthResolverFunc) Resolve(ctx context.Context, cfg ProviderConfig) (AuthResult, error) {
	return f(ctx, cfg)
}

// ResolveAPIKey extracts an API key from ProviderConfig, preferring the
// environment variable named by Auth.APIKeyEnv, then a literal APIKey.
func ResolveAPIKey(cfg ProviderConfig) (string, error) {
	if env := strings.TrimSpace(cfg.Auth.APIKeyEnv); env != "" {
		if key := strings.TrimSpace(os.Getenv(env)); key != "" {
			return key, nil
		}
		if cfg.Auth.APIKey == "" {
			return "", fmt.Errorf("api_key environment variable %s is not set", env)
		}
	}
	if key := strings.TrimSpace(cfg.Auth.APIKey); key != "" {
		return key, nil
	}
	return "", errors.New("no api_key or api_key_env configured")
}

// apiKeyResolver is the built-in resolver for AuthTypeAPIKey.
type apiKeyResolver struct{}

func (apiKeyResolver) Resolve(ctx context.Context, cfg ProviderConfig) (AuthResult, error) {
	key, err := ResolveAPIKey(cfg)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{Token: key}, nil
}

// noneResolver returns an empty result.
type noneResolver struct{}

func (noneResolver) Resolve(ctx context.Context, cfg ProviderConfig) (AuthResult, error) {
	return AuthResult{}, nil
}

var (
	authMu        sync.RWMutex
	authResolvers = map[AuthType]AuthResolver{
		AuthTypeNone:      noneResolver{},
		AuthTypeAPIKey:    apiKeyResolver{},
	}
)

// RegisterAuthResolver registers a resolver for an auth type. It
// overwrites any existing resolver for that type.
func RegisterAuthResolver(t AuthType, r AuthResolver) {
	if r == nil {
		panic("runtime: RegisterAuthResolver called with nil resolver")
	}
	authMu.Lock()
	defer authMu.Unlock()
	authResolvers[t] = r
}

// GetAuthResolver returns the registered resolver for t, or false if
// none exists.
func GetAuthResolver(t AuthType) (AuthResolver, bool) {
	authMu.RLock()
	defer authMu.RUnlock()
	r, ok := authResolvers[t]
	return r, ok
}

// resolveAuth runs the registered resolver for cfg.Auth.Type.
func resolveAuth(ctx context.Context, cfg ProviderConfig) (AuthResult, error) {
	t := cfg.Auth.Type
	if t == "" {
		t = AuthTypeAPIKey
	}
	r, ok := GetAuthResolver(t)
	if !ok {
		return AuthResult{}, fmt.Errorf("no auth resolver registered for type %q", t)
	}
	return r.Resolve(ctx, cfg)
}
