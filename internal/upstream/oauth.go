package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

// FileTokenStore persists OAuth tokens to a JSON file.
type FileTokenStore struct {
	mu   sync.Mutex
	path string
}

func NewFileTokenStore(serverName string) *FileTokenStore {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".recall-mcp-gateway", "tokens")
	os.MkdirAll(dir, 0700)
	return &FileTokenStore{path: filepath.Join(dir, serverName+".json")}
}

func (s *FileTokenStore) GetToken(ctx context.Context) (*transport.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, transport.ErrNoToken
	}
	var tok transport.Token
	if json.Unmarshal(data, &tok) != nil {
		return nil, transport.ErrNoToken
	}
	return &tok, nil
}

func (s *FileTokenStore) SaveToken(ctx context.Context, token *transport.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == nil {
		os.Remove(s.path)
		return nil
	}
	data, _ := json.MarshalIndent(token, "", "  ")
	return os.WriteFile(s.path, data, 0600)
}

// OAuthConnector handles the OAuth browser flow for MCP servers.
type OAuthConnector struct {
	callbackPort int
}

func NewOAuthConnector(callbackPort int) *OAuthConnector {
	if callbackPort == 0 {
		callbackPort = 8091
	}
	return &OAuthConnector{callbackPort: callbackPort}
}

// Connect creates an OAuth-authenticated MCP client.
func (oc *OAuthConnector) Connect(ctx context.Context, serverName, serverURL string) (*client.Client, error) {
	tokenStore := NewFileTokenStore(serverName)
	redirectURI := fmt.Sprintf("http://localhost:%d/oauth/callback", oc.callbackPort)

	oauthCfg := transport.OAuthConfig{
		ClientID:    "recall-mcp-gateway",
		ClientURI:   redirectURI,
		RedirectURI: redirectURI,
		TokenStore:  tokenStore,
		PKCEEnabled: true,
	}

	// Check if we have a valid stored token by probing the server
	if tok, _ := tokenStore.GetToken(ctx); tok != nil && tok.AccessToken != "" {
		// Bind credentials to the issuer that issued them (RFC 9207 / SEP-2352).
		// If the token has an issuer, set CredentialsIssuer so mcp-go validates
		// that the discovered AS matches. If the AS changed, mcp-go will reject
		// the cached credentials and we'll re-register.
		if tok.Issuer != "" {
			oauthCfg.CredentialsIssuer = tok.Issuer
		}

		// Test the token with a real HTTP request
		if oc.probeWithToken(serverURL, tok.AccessToken) {
			mcpClient, err := client.NewOAuthStreamableHttpClient(serverURL, oauthCfg)
			if err == nil {
				slog.Info("connected with stored token", "component", "oauth", "server", serverName, "issuer", tok.Issuer)
				return mcpClient, nil
			}
		}
		slog.Warn("stored token invalid, need re-authorization", "component", "oauth", "server", serverName)
		tokenStore.SaveToken(ctx, nil) // clear it
		oauthCfg.CredentialsIssuer = "" // clear binding for fresh registration
	}

	// No valid token — need interactive authorization.
	// Discover the OAuth authorization server from the well-known URL
	// Extract origin (scheme + host) from the server URL
	baseOrigin := serverURL
	if idx := strings.Index(serverURL, "/v1/"); idx > 0 {
		baseOrigin = serverURL[:idx]
	} else if parts := strings.SplitN(serverURL, "://", 2); len(parts) == 2 {
		if slashIdx := strings.Index(parts[1], "/"); slashIdx > 0 {
			baseOrigin = parts[0] + "://" + parts[1][:slashIdx]
		}
	}
	authMetaURL := baseOrigin + "/.well-known/oauth-authorization-server"
	oauthCfg.AuthServerMetadataURL = authMetaURL
	slog.Info("using auth server metadata", "component", "oauth", "url", authMetaURL)

	// Create handler with the metadata URL — mcp-go will discover endpoints + register
	handler := transport.NewOAuthHandler(oauthCfg)
	handler.SetBaseURL(serverURL)

	// Discover auth server metadata
	meta, err := handler.GetServerMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("oauth: discover auth server for %s: %w", serverName, err)
	}
	slog.Info("auth server discovered", "component", "oauth",
		"authorize", meta.AuthorizationEndpoint,
		"token", meta.TokenEndpoint,
		"register", meta.RegistrationEndpoint)

	// PKCE gate: verify AS supports S256 before proceeding (MCP 2026-07-28 requirement)
	if len(meta.CodeChallengeMethodsSupported) > 0 {
		hasS256 := false
		for _, m := range meta.CodeChallengeMethodsSupported {
			if m == "S256" {
				hasS256 = true
				break
			}
		}
		if !hasS256 {
			return nil, fmt.Errorf("oauth: AS for %s does not support PKCE S256 (supported: %v)", serverName, meta.CodeChallengeMethodsSupported)
		}
	}
	// Note: if code_challenge_methods_supported is absent from metadata, we proceed
	// with S256 and let the AS reject if it doesn't support it (lenient for older AS).

	// Dynamic client registration
	if err := handler.RegisterClient(ctx, "recall-mcp-gateway"); err != nil {
		return nil, fmt.Errorf("oauth: register client for %s: %w", serverName, err)
	}
	slog.Info("registered OAuth client", "component", "oauth", "client_id", handler.GetClientID(), "server", serverName)

	// Generate PKCE
	codeVerifier, err := transport.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("oauth: %w", err)
	}
	codeChallenge := transport.GenerateCodeChallenge(codeVerifier)
	state, err := transport.GenerateState()
	if err != nil {
		return nil, fmt.Errorf("oauth: %w", err)
	}
	handler.SetExpectedState(state)

	authURL, err := handler.GetAuthorizationURL(ctx, state, codeChallenge)
	if err != nil {
		return nil, fmt.Errorf("oauth: auth URL for %s: %w", serverName, err)
	}

	// Browser flow (with issuer validation per RFC 9207)
	code, err := oc.doInteractiveAuth(ctx, serverName, authURL, state, meta.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oauth: %w", err)
	}

	if err := handler.ProcessAuthorizationResponse(ctx, code, state, codeVerifier); err != nil {
		return nil, fmt.Errorf("oauth: token exchange for %s: %w", serverName, err)
	}

	// Reconnect with the new token
	mcpClient, err := client.NewOAuthStreamableHttpClient(serverURL, oauthCfg)
	if err != nil {
		return nil, fmt.Errorf("oauth: reconnect to %s: %w", serverName, err)
	}

	slog.Info("authorized successfully", "component", "oauth", "server", serverName)
	return mcpClient, nil
}

// probe401 makes a request to the server and extracts OAuth metadata from the 401 response.
func (oc *OAuthConnector) probe401(serverURL string) (string, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Post(serverURL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		return "", fmt.Errorf("expected 401, got %d", resp.StatusCode)
	}

	// Extract resource_metadata from WWW-Authenticate header
	// Format: Bearer resource_metadata="https://..."
	wwwAuth := resp.Header.Get("WWW-Authenticate")
	if wwwAuth == "" {
		return "", fmt.Errorf("no WWW-Authenticate header in 401 response")
	}

	slog.Debug("WWW-Authenticate header received", "component", "oauth", "value", wwwAuth)

	// Parse resource_metadata parameter
	for _, part := range strings.Split(wwwAuth, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "resource_metadata=") {
			url := strings.TrimPrefix(part, "resource_metadata=")
			url = strings.Trim(url, `"`)
			return url, nil
		}
	}

	return "", nil // no resource_metadata, let mcp-go discover it
}

// probeWithToken tests if a token is valid by making a request.
func (oc *OAuthConnector) probeWithToken(serverURL, token string) bool {
	httpClient := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("POST", serverURL, strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode != http.StatusUnauthorized
}

func (oc *OAuthConnector) doInteractiveAuth(ctx context.Context, serverName, authURL, expectedState, expectedIssuer string) (string, error) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		if s := r.URL.Query().Get("state"); s != expectedState {
			http.Error(w, "Invalid state", http.StatusBadRequest)
			errCh <- fmt.Errorf("state mismatch")
			return
		}
		// RFC 9207: validate iss parameter matches expected AS issuer (exact string match)
		if iss := r.URL.Query().Get("iss"); iss != "" && iss != expectedIssuer {
			http.Error(w, "Issuer mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("iss mismatch: expected %q, got %q", expectedIssuer, iss)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			msg := r.URL.Query().Get("error") + ": " + r.URL.Query().Get("error_description")
			http.Error(w, msg, http.StatusBadRequest)
			errCh <- fmt.Errorf("authorization denied: %s", msg)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body style="font-family:system-ui;text-align:center;padding:60px">
			<h2>Authorized %s</h2><p>You can close this window.</p>
			<script>setTimeout(function(){window.close()},2000)</script>
		</body></html>`, serverName)
		codeCh <- code
	})

	srv := &http.Server{Addr: fmt.Sprintf(":%d", oc.callbackPort), Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer srv.Shutdown(context.Background())

	slog.Info("opening browser for authorization", "component", "oauth", "server", serverName, "auth_url", authURL)
	openBrowser(authURL)

	select {
	case code := <-codeCh:
		return code, nil
	case err := <-errCh:
		return "", err
	case <-time.After(2 * time.Minute):
		return "", fmt.Errorf("timed out (2 minutes)")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		cmd = exec.Command("cmd", "/c", "start", url)
	}
	cmd.Start()
}

func needsOAuth(err error) bool {
	if err == nil {
		return false
	}
	if client.IsOAuthAuthorizationRequiredError(err) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "authorization required") || strings.Contains(s, "no valid token")
}
