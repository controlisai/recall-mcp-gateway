package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	proxyauth "github.com/controlisai/recall-mcp-gateway/internal/auth"
	"github.com/controlisai/recall-mcp-gateway/internal/store"
)

// OAuthServer implements a minimal OAuth 2.0 authorization server for MCP
// client compatibility (e.g., Claude Code). Since the proxy is a local dev
// tool, it auto-approves all authorization requests and issues opaque tokens
// that map to the "anonymous" agent.
// jwtValidator validates JWTs for token exchange.
type jwtValidator interface {
	Validate(token string) (*proxyauth.JWTClaims, error)
}

type OAuthServer struct {
	issuer          string // e.g. "http://localhost:8090"
	externalIssuer  string // when set, PRM points to this external AS instead of the built-in one
	persistPath     string // path to oauth-tokens.json (empty = no persistence)
	persistDB       *sql.DB // gateway.db connection (nil = use file)
	tokenIssuer     *proxyauth.TokenIssuer // for signing delegated JWTs (RFC 8693)
	jwtVal          jwtValidator // for validating subject/actor tokens

	mu          sync.Mutex
	clients     map[string]*oauthClient       // client_id -> client
	codes       map[string]*authorizationCode // code -> auth code
	tokens      map[string]bool               // issued access tokens
	tokenAgents map[string]string             // token -> discovered agent name
}

const maxPersistedTokens = 1000

// hashOAuthToken returns the SHA-256 hex hash of an OAuth token.
// Tokens are stored as hashes, never as plaintext.
func hashOAuthToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

type persistedTokenData struct {
	Tokens      []string          `json:"tokens"`
	TokenAgents map[string]string `json:"tokenAgents,omitempty"`
}

type oauthClient struct {
	ClientID        string   `json:"client_id"`
	ClientName      string   `json:"client_name,omitempty"`
	RedirectURIs    []string `json:"redirect_uris"`
	ApplicationType string   `json:"application_type,omitempty"` // "native" or "web"
}

type authorizationCode struct {
	code                string
	clientID            string
	redirectURI         string
	codeChallenge       string // PKCE S256 challenge
	codeChallengeMethod string // "S256" or "plain"
	expiresAt           time.Time
}

// NewOAuthServer creates a minimal OAuth server.
func NewOAuthServer(issuer string) *OAuthServer {
	return &OAuthServer{
		issuer:  issuer,
		clients: make(map[string]*oauthClient),
		codes:   make(map[string]*authorizationCode),
		tokens:  make(map[string]bool),
	}
}

// SetPersistPath enables token persistence to a JSON file. Loads existing tokens on call.
func (s *OAuthServer) SetPersistPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistPath = path

	data, err := os.ReadFile(path)
	if err != nil {
		return // no file yet — normal on first run
	}
	var stored persistedTokenData
	if err := json.Unmarshal(data, &stored); err != nil {
		slog.Warn("failed to load persisted OAuth tokens", "component", "oauth", "error", err)
		return
	}
	for _, tok := range stored.Tokens {
		s.tokens[tok] = true
	}
	if stored.TokenAgents != nil {
		if s.tokenAgents == nil {
			s.tokenAgents = make(map[string]string)
		}
		for k, v := range stored.TokenAgents {
			s.tokenAgents[k] = v
		}
	}
	slog.Info("loaded persisted OAuth tokens", "component", "oauth", "count", len(stored.Tokens))
}

// SetPersistDB enables token persistence via gateway.db. Loads existing tokens on call.
func (s *OAuthServer) SetPersistDB(db *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistDB = db
	rows, err := db.Query("SELECT token, agent_id FROM oauth_tokens")
	if err != nil {
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var tok, agent string
		if rows.Scan(&tok, &agent) == nil {
			s.tokens[tok] = true
			if agent != "" {
				if s.tokenAgents == nil {
					s.tokenAgents = make(map[string]string)
				}
				s.tokenAgents[tok] = agent
			}
			count++
		}
	}
	if count > 0 {
		slog.Info("loaded OAuth tokens from DB", "component", "oauth", "count", count)
	}
}

func (s *OAuthServer) persistTokens() {
	if s.persistPath == "" && s.persistDB == nil {
		return
	}
	// Collect tokens, cap at maxPersistedTokens (keep most recent by map iteration — acceptable)
	tokens := make([]string, 0, len(s.tokens))
	for tok := range s.tokens {
		tokens = append(tokens, tok)
		if len(tokens) >= maxPersistedTokens {
			break
		}
	}
	// Write to DB if available
	if s.persistDB != nil {
		s.persistDB.Exec("DELETE FROM oauth_tokens") //nolint:errcheck
		for _, tok := range tokens {
			agent := ""
			if s.tokenAgents != nil {
				agent = s.tokenAgents[tok]
			}
			store.ExecDialect(s.persistDB, //nolint:errcheck
				"INSERT OR REPLACE INTO oauth_tokens (token, agent_id) VALUES (?, ?)",
				"INSERT INTO oauth_tokens (token, agent_id) VALUES ($1, $2) ON CONFLICT (token) DO UPDATE SET agent_id = $2",
				tok, agent)
		}
		return
	}
	// Fall back to file
	stored := persistedTokenData{Tokens: tokens, TokenAgents: s.tokenAgents}
	data, err := json.Marshal(stored)
	if err != nil {
		return
	}
	if s.persistPath != "" {
		os.MkdirAll(filepath.Dir(s.persistPath), 0700)
		os.WriteFile(s.persistPath, data, 0600)
	}
}

// SetExternalAS configures the PRM endpoint to point to an external Authorization Server.
// When set, handleProtectedResource returns the external issuer instead of the built-in one.
func (s *OAuthServer) SetExternalAS(issuer string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.externalIssuer = issuer
}

// SetTokenIssuer configures JWT signing for token exchange (RFC 8693).
func (s *OAuthServer) SetTokenIssuer(issuer *proxyauth.TokenIssuer) {
	s.tokenIssuer = issuer
}

// SetJWTValidator configures JWT validation for subject/actor token verification.
func (s *OAuthServer) SetJWTValidator(validator jwtValidator) {
	s.jwtVal = validator
}

// RegisterPRM registers only the Protected Resource Metadata endpoint on the mux.
// Use this instead of RegisterRoutes when an external AS handles OAuth flows.
func (s *OAuthServer) RegisterPRM(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-protected-resource", s.handleProtectedResource)
}

// IsValidToken checks whether a bearer token was issued by this server.
func (s *OAuthServer) IsValidToken(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens[hashOAuthToken(token)]
}

// AgentForToken returns the agent name mapped to a token, or empty string.
func (s *OAuthServer) AgentForToken(token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokenAgents == nil {
		return ""
	}
	return s.tokenAgents[hashOAuthToken(token)]
}

// SetTokenAgent maps an OAuth token to a discovered agent name.
func (s *OAuthServer) SetTokenAgent(token, agentName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokenAgents == nil {
		s.tokenAgents = make(map[string]string)
	}
	s.tokenAgents[hashOAuthToken(token)] = agentName
}

// RegisterRoutes mounts all OAuth endpoints on the given mux.
func (s *OAuthServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.handleMetadata)
	mux.HandleFunc("/.well-known/oauth-protected-resource", s.handleProtectedResource)
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/authorize", s.handleAuthorize)
	mux.HandleFunc("/token", s.handleToken)
}

func (s *OAuthServer) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		corsHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	corsHeaders(w)
	metadata := map[string]any{
		"issuer":                 s.issuer,
		"authorization_endpoint": s.issuer + "/authorize",
		"token_endpoint":         s.issuer + "/token",
		"registration_endpoint":  s.issuer + "/register",
		"response_types_supported":            []string{"code"},
		"grant_types_supported":               []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:token-exchange"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":     []string{"S256"},
		"scopes_supported":                                  []string{"mcp:tools"},
		"authorization_response_iss_parameter_supported": true,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(metadata)
}

func (s *OAuthServer) handleProtectedResource(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		corsHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	corsHeaders(w)

	// When an external AS is configured, point clients there instead of the built-in one
	s.mu.Lock()
	authServers := []string{s.issuer}
	if s.externalIssuer != "" {
		authServers = []string{s.externalIssuer}
	}
	s.mu.Unlock()

	meta := map[string]any{
		"resource":                 s.issuer + "/mcp",
		"authorization_servers":    authServers,
		"scopes_supported":        []string{"mcp:tools"},
		"bearer_methods_supported": []string{"header"},
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(meta)
}

func (s *OAuthServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	corsHeaders(w)

	var req struct {
		ClientName      string   `json:"client_name"`
		RedirectURIs    []string `json:"redirect_uris"`
		ApplicationType string   `json:"application_type,omitempty"` // "native" or "web" (MCP clients are typically "native")
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}

	clientID := randomHex(16)
	appType := req.ApplicationType
	if appType == "" {
		appType = "native" // default for MCP clients (CLI/desktop)
	}
	client := &oauthClient{
		ClientID:        clientID,
		ClientName:      req.ClientName,
		RedirectURIs:    req.RedirectURIs,
		ApplicationType: appType,
	}

	s.mu.Lock()
	s.clients[clientID] = client
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(client)
}

func (s *OAuthServer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	codeChallenge := r.URL.Query().Get("code_challenge")
	codeChallengeMethod := r.URL.Query().Get("code_challenge_method")
	if codeChallengeMethod == "" && codeChallenge != "" {
		codeChallengeMethod = "S256" // default per RFC 7636
	}
	if codeChallenge != "" && codeChallengeMethod != "S256" && codeChallengeMethod != "plain" {
		http.Error(w, `{"error":"invalid_request","error_description":"unsupported code_challenge_method"}`, http.StatusBadRequest)
		return
	}

	if clientID == "" || redirectURI == "" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}

	// Auto-approve: generate auth code and redirect immediately
	code := randomHex(32)
	s.mu.Lock()
	s.codes[code] = &authorizationCode{
		code:                code,
		clientID:            clientID,
		redirectURI:         redirectURI,
		codeChallenge:       codeChallenge,
		codeChallengeMethod: codeChallengeMethod,
		expiresAt:           time.Now().Add(5 * time.Minute),
	}
	s.mu.Unlock()

	location := fmt.Sprintf("%s?code=%s&iss=%s", redirectURI, code, url.QueryEscape(s.issuer))
	if state != "" {
		location += "&state=" + state
	}
	http.Redirect(w, r, location, http.StatusFound)
}

func (s *OAuthServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	corsHeaders(w)

	if err := r.ParseForm(); err != nil {
		jsonError(w, "invalid_request", http.StatusBadRequest)
		return
	}

	grantType := r.FormValue("grant_type")
	switch grantType {
	case "authorization_code":
		s.handleTokenExchange(w, r)
	case "refresh_token":
		s.handleRefreshToken(w, r)
	case "urn:ietf:params:oauth:grant-type:token-exchange":
		s.handleRFC8693TokenExchange(w, r)
	default:
		jsonError(w, "unsupported_grant_type", http.StatusBadRequest)
	}
}

func (s *OAuthServer) handleTokenExchange(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	codeVerifier := r.FormValue("code_verifier")

	s.mu.Lock()
	ac, ok := s.codes[code]
	if ok {
		delete(s.codes, code) // one-time use
	}
	s.mu.Unlock()

	if !ok || time.Now().After(ac.expiresAt) {
		jsonError(w, "invalid_grant", http.StatusBadRequest)
		return
	}

	// PKCE validation: if a challenge was stored, verifier is required
	if ac.codeChallenge != "" {
		if codeVerifier == "" {
			jsonError(w, "invalid_grant", http.StatusBadRequest)
			return
		}
		if !verifyPKCE(ac.codeChallenge, ac.codeChallengeMethod, codeVerifier) {
			jsonError(w, "invalid_grant", http.StatusBadRequest)
			return
		}
	}

	accessToken := randomHex(32)
	refreshToken := randomHex(32)

	s.mu.Lock()
	s.tokens[hashOAuthToken(accessToken)] = true
	s.persistTokens()
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    86400,
		"refresh_token": refreshToken,
		"scope":         "mcp:tools",
	})
}

func (s *OAuthServer) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	// Accept any refresh token — issue a new access token
	accessToken := randomHex(32)
	refreshToken := randomHex(32)

	s.mu.Lock()
	s.tokens[hashOAuthToken(accessToken)] = true
	s.persistTokens()
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    86400,
		"refresh_token": refreshToken,
		"scope":         "mcp:tools",
	})
}

// verifyPKCE validates a code_verifier against the stored code_challenge.
func verifyPKCE(challenge, method, verifier string) bool {
	switch method {
	case "S256":
		h := sha256.Sum256([]byte(verifier))
		computed := base64.RawURLEncoding.EncodeToString(h[:])
		return computed == challenge
	case "plain":
		return verifier == challenge
	default:
		return false
	}
}

func corsHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func jsonError(w http.ResponseWriter, errorCode string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": errorCode})
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// handleRFC8693TokenExchange implements RFC 8693 token exchange for delegation.
// Accepts a subject_token (user's JWT) and optional actor_token (service's JWT),
// validates both, checks may_act authorization, and issues a delegated JWT.
func (s *OAuthServer) handleRFC8693TokenExchange(w http.ResponseWriter, r *http.Request) {
	if s.tokenIssuer == nil || s.jwtVal == nil {
		jsonError(w, "unsupported_grant_type", http.StatusBadRequest)
		return
	}

	subjectToken := r.FormValue("subject_token")
	subjectTokenType := r.FormValue("subject_token_type")
	actorToken := r.FormValue("actor_token")
	scope := r.FormValue("scope")

	if subjectToken == "" {
		jsonError(w, "invalid_request", http.StatusBadRequest)
		return
	}
	if subjectTokenType != "" && subjectTokenType != "urn:ietf:params:oauth:token-type:jwt" {
		jsonError(w, "invalid_request", http.StatusBadRequest)
		return
	}

	// Validate subject token (the user whose authority is being delegated)
	subjectClaims, err := s.jwtVal.Validate(subjectToken)
	if err != nil {
		slog.Warn("token exchange: invalid subject_token", "component", "oauth", "error", err)
		jsonError(w, "invalid_grant", http.StatusBadRequest)
		return
	}

	// Validate actor token if provided (the service requesting delegation)
	var actorSub string
	if actorToken != "" {
		actorClaims, err := s.jwtVal.Validate(actorToken)
		if err != nil {
			slog.Warn("token exchange: invalid actor_token", "component", "oauth", "error", err)
			jsonError(w, "invalid_grant", http.StatusBadRequest)
			return
		}
		actorSub = actorClaims.Subject
		if actorSub == "" {
			actorSub = actorClaims.ClientID
		}
	}

	if actorSub == "" {
		jsonError(w, "invalid_request", http.StatusBadRequest)
		return
	}

	// Check may_act authorization: actor must be allowed by subject's may_act claim
	if subjectClaims.MayAct != nil && subjectClaims.MayAct.Subject != actorSub {
		slog.Warn("token exchange: actor not authorized by may_act",
			"component", "oauth", "subject", subjectClaims.Subject, "actor", actorSub,
			"may_act", subjectClaims.MayAct.Subject)
		jsonError(w, "invalid_grant", http.StatusForbidden)
		return
	}

	// Issue delegated JWT
	audience := s.issuer
	if len(subjectClaims.Audience) > 0 {
		audience = subjectClaims.Audience[0]
	}
	delegatedToken, err := s.tokenIssuer.IssueDelegatedToken(proxyauth.DelegatedTokenClaims{
		Subject:  subjectClaims.Subject,
		Actor:    actorSub,
		Audience: audience,
		Scopes:   scope,
		TTL:      time.Hour,
	})
	if err != nil {
		slog.Error("token exchange: failed to issue delegated token", "component", "oauth", "error", err)
		jsonError(w, "server_error", http.StatusInternalServerError)
		return
	}

	slog.Info("token exchange: delegated token issued",
		"component", "oauth", "subject", subjectClaims.Subject, "actor", actorSub)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token":        delegatedToken,
		"issued_token_type":   "urn:ietf:params:oauth:token-type:jwt",
		"token_type":          "Bearer",
		"expires_in":          3600,
		"scope":               scope,
	})
}
