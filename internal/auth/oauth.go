package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/sanitize"
)

const tokenRefreshBuffer = 30 * time.Second

// OAuthClientCredentialsProvider fetches and caches OAuth2 client_credentials tokens.
type OAuthClientCredentialsProvider struct {
	tokenURL       string
	clientID       string
	clientSecret   string
	scopes         []string
	expectedIssuer string // optional: validate iss claim in JWT access tokens
	httpClient     *http.Client

	mu          sync.RWMutex
	accessToken string
	expiresAt   time.Time
}

// OAuthClientCredentialsConfig configures the OAuth provider.
type OAuthClientCredentialsConfig struct {
	TokenURL       string
	ClientID       string
	ClientSecret   string
	Scopes         []string
	ExpectedIssuer string // optional: if set, validate iss claim in JWT access tokens
}

// NewOAuthClientCredentialsProvider creates an OAuth client_credentials provider.
func NewOAuthClientCredentialsProvider(cfg OAuthClientCredentialsConfig) *OAuthClientCredentialsProvider {
	return &OAuthClientCredentialsProvider{
		tokenURL:       cfg.TokenURL,
		clientID:       cfg.ClientID,
		clientSecret:   cfg.ClientSecret,
		scopes:         cfg.Scopes,
		expectedIssuer: cfg.ExpectedIssuer,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Headers returns the Authorization header with a cached or freshly fetched token.
func (p *OAuthClientCredentialsProvider) Headers(ctx context.Context) map[string]string {
	token, err := p.getToken(ctx)
	if err != nil {
		slog.Warn("oauth token error", "component", "auth", "error", sanitize.Error(err.Error()))
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func (p *OAuthClientCredentialsProvider) Name() string { return "oauth_client_credentials" }

// InvalidateToken clears the cached token, forcing a refresh on the next call.
func (p *OAuthClientCredentialsProvider) InvalidateToken() {
	p.mu.Lock()
	p.accessToken = ""
	p.expiresAt = time.Time{}
	p.mu.Unlock()
	slog.Info("oauth token invalidated — will refresh on next request", "component", "auth")
}

// getToken returns a cached token or fetches a new one if expired/missing.
func (p *OAuthClientCredentialsProvider) getToken(ctx context.Context) (string, error) {
	p.mu.RLock()
	if p.accessToken != "" && time.Now().Before(p.expiresAt.Add(-tokenRefreshBuffer)) {
		token := p.accessToken
		p.mu.RUnlock()
		return token, nil
	}
	p.mu.RUnlock()

	return p.refreshToken(ctx)
}

// refreshToken fetches a new token from the token endpoint.
func (p *OAuthClientCredentialsProvider) refreshToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check: another goroutine may have refreshed while we waited for the lock
	if p.accessToken != "" && time.Now().Before(p.expiresAt.Add(-tokenRefreshBuffer)) {
		return p.accessToken, nil
	}

	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
	}
	if len(p.scopes) > 0 {
		data.Set("scope", strings.Join(p.scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request to %s: %w", p.tokenURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16)) // 64KB limit
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token response missing access_token")
	}

	// Validate iss claim if the token is a JWT and expectedIssuer is configured
	if p.expectedIssuer != "" {
		if iss := extractJWTIssuer(tokenResp.AccessToken); iss != "" && iss != p.expectedIssuer {
			slog.Warn("oauth token iss mismatch", "component", "auth", "expected", p.expectedIssuer, "got", iss)
		}
	}

	p.accessToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		p.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	} else {
		// No expiry info — assume 1 hour
		p.expiresAt = time.Now().Add(1 * time.Hour)
	}

	slog.Info("oauth token refreshed", "component", "auth", "expires_in_s", tokenResp.ExpiresIn)
	return p.accessToken, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

// extractJWTIssuer extracts the "iss" claim from a JWT access token without signature validation.
// Returns empty string if the token is not a JWT or has no iss claim.
func extractJWTIssuer(token string) string {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return "" // not a JWT
	}
	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Iss string `json:"iss"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Iss
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
