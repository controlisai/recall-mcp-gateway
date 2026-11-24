package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Provider adds authentication to outgoing HTTP requests to upstream MCP servers.
// Implementations return headers that the MCP client applies per-request via WithHTTPHeaderFunc.
type Provider interface {
	// Headers returns the auth headers to apply to an outgoing request.
	Headers(ctx context.Context) map[string]string

	// Name returns the auth type for logging (e.g., "bearer", "basic", "none").
	Name() string
}

// TransportProvider is implemented by auth providers that need a custom http.Client
// (e.g., mTLS) instead of just adding headers. The upstream client checks for this
// interface and uses WithHTTPBasicClient instead of WithHTTPHeaderFunc.
type TransportProvider interface {
	Provider
	HTTPClient() *http.Client
}

// RefreshableProvider can invalidate and re-acquire credentials (e.g., OAuth token refresh).
type RefreshableProvider interface {
	Provider
	InvalidateToken()
}

// ReloadableProvider can reload credentials from disk (e.g., mTLS cert rotation).
type ReloadableProvider interface {
	Provider
	Reload() error
}

// Config defines per-server auth configuration.
type Config struct {
	Type         string            `yaml:"type" json:"type"`                                     // "none", "bearer", "basic", "headers", "oauth_client_credentials", "mtls"
	Token        string            `yaml:"token,omitempty" json:"token,omitempty"`                // bearer token (supports ${ENV_VAR})
	Username     string            `yaml:"username,omitempty" json:"username,omitempty"`          // basic auth username
	Password     string            `yaml:"password,omitempty" json:"password,omitempty"`          // basic auth password (supports ${ENV_VAR})
	Headers      map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`            // custom headers (values support ${ENV_VAR})
	TokenURL     string            `yaml:"token_url,omitempty" json:"tokenUrl,omitempty"`         // OAuth token endpoint
	ClientID     string            `yaml:"client_id,omitempty" json:"clientId,omitempty"`         // OAuth client ID
	ClientSecret string            `yaml:"client_secret,omitempty" json:"clientSecret,omitempty"` // OAuth client secret (supports ${ENV_VAR})
	Scopes       []string          `yaml:"scopes,omitempty" json:"scopes,omitempty"`              // OAuth scopes
	ClientCert   string            `yaml:"client_cert,omitempty" json:"clientCert,omitempty"`     // mTLS client certificate path
	ClientKey    string            `yaml:"client_key,omitempty" json:"clientKey,omitempty"`       // mTLS client private key path
	CACert       string            `yaml:"ca_cert,omitempty" json:"caCert,omitempty"`             // mTLS CA cert for server verification
	Issuer       string            `yaml:"issuer,omitempty" json:"issuer,omitempty"`              // expected JWT issuer for iss validation (oauth_client_credentials)
}

// NewProvider creates an auth provider from config.
// If cfg is nil or type is empty/"none", returns NoneProvider.
func NewProvider(cfg *Config) (Provider, error) {
	if cfg == nil || cfg.Type == "" || cfg.Type == "none" {
		return &NoneProvider{}, nil
	}

	switch cfg.Type {
	case "bearer":
		token := ExpandEnv(cfg.Token)
		if token == "" {
			return nil, fmt.Errorf("auth: bearer requires token")
		}
		return &BearerProvider{token: token}, nil

	case "basic":
		username := ExpandEnv(cfg.Username)
		password := ExpandEnv(cfg.Password)
		if username == "" {
			return nil, fmt.Errorf("auth: basic requires username")
		}
		return NewBasicProvider(username, password), nil

	case "headers":
		headers := make(map[string]string, len(cfg.Headers))
		for k, v := range cfg.Headers {
			headers[k] = ExpandEnv(v)
		}
		return &HeadersProvider{headers: headers}, nil

	case "oauth_client_credentials":
		tokenURL := ExpandEnv(cfg.TokenURL)
		clientID := ExpandEnv(cfg.ClientID)
		clientSecret := ExpandEnv(cfg.ClientSecret)
		if tokenURL == "" {
			return nil, fmt.Errorf("auth: oauth_client_credentials requires token_url")
		}
		if clientID == "" {
			return nil, fmt.Errorf("auth: oauth_client_credentials requires client_id")
		}
		if clientSecret == "" {
			return nil, fmt.Errorf("auth: oauth_client_credentials requires client_secret")
		}
		return NewOAuthClientCredentialsProvider(OAuthClientCredentialsConfig{
			TokenURL:       tokenURL,
			ClientID:       clientID,
			ClientSecret:   clientSecret,
			Scopes:         cfg.Scopes,
			ExpectedIssuer: cfg.Issuer,
		}), nil

	case "mtls":
		certPath := ExpandEnv(cfg.ClientCert)
		keyPath := ExpandEnv(cfg.ClientKey)
		if certPath == "" {
			return nil, fmt.Errorf("auth: mtls requires client_cert")
		}
		if keyPath == "" {
			return nil, fmt.Errorf("auth: mtls requires client_key")
		}
		caPath := ExpandEnv(cfg.CACert)
		return NewMTLSProvider(MTLSConfig{
			ClientCert: certPath,
			ClientKey:  keyPath,
			CACert:     caPath,
		})

	default:
		return nil, fmt.Errorf("auth: unsupported type %q", cfg.Type)
	}
}

// ExpandEnv replaces ${VAR} patterns in s with environment variable values.
// Only ${VAR} syntax is supported (not $VAR). Unknown variables resolve to empty string.
func ExpandEnv(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return os.Expand(s, func(key string) string {
		return os.Getenv(key)
	})
}

// Compile-time interface checks.
var (
	_ Provider = (*NoneProvider)(nil)
	_ Provider = (*BearerProvider)(nil)
	_ Provider = (*BasicProvider)(nil)
	_ Provider = (*HeadersProvider)(nil)
	_ Provider          = (*OAuthClientCredentialsProvider)(nil)
	_ Provider            = (*MTLSProvider)(nil)
	_ TransportProvider   = (*MTLSProvider)(nil)
	_ ReloadableProvider  = (*MTLSProvider)(nil)
	_ RefreshableProvider = (*OAuthClientCredentialsProvider)(nil)
)

// NoneProvider adds no authentication headers.
type NoneProvider struct{}

func (p *NoneProvider) Headers(ctx context.Context) map[string]string { return nil }
func (p *NoneProvider) Name() string                                  { return "none" }
