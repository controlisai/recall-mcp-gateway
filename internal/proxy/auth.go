package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/controlisai/recall-mcp-gateway/internal/auth"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

// Identity source constants for audit logging.
const (
	IdentityBearerToken       = "bearer-token"
	IdentityJWT               = "jwt"
	IdentityMetaClientInfo    = "meta-client-info"
	IdentityInitializeSession = "initialize-session"
	IdentityAnonymous         = "anonymous"
)

// Protocol era constants for audit logging.
const (
	ProtocolEraModern = "modern"
	ProtocolEraLegacy = "legacy"
)

// HashToken computes the SHA-256 hex hash of a bearer token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// isJWTFormat returns true if the token looks like a JWT (header.payload.signature).
func isJWTFormat(token string) bool {
	return strings.Count(token, ".") == 2
}

type contextKey string

const agentIDKey contextKey = "agentID"
const bearerTokenKey contextKey = "bearerToken"
const unauthorizedKey contextKey = "unauthorized"
const requestIDKey contextKey = "requestID"
const identitySourceKey contextKey = "identitySource"
const jwtClaimsKey contextKey = "jwtClaims"
const taskCallKey contextKey = "taskCall"

// ContextWithRequestID stores a request ID in context.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext retrieves the request ID from context.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// TokenValidator checks whether a bearer token is valid (built-in OAuth tokens).
type TokenValidator interface {
	IsValidToken(token string) bool
	AgentForToken(token string) string
	SetTokenAgent(token, agentName string)
}

// JWTTokenValidator validates JWT tokens from an external Authorization Server.
type JWTTokenValidator interface {
	Validate(token string) (*auth.JWTClaims, error)
}

// agentAuthEntry holds agent identity and optional expiry for a static token.
type agentAuthEntry struct {
	AgentID   string
	ExpiresAt time.Time // zero = never expires
}

// agentAuthData is the immutable data behind an AgentAuthenticator.
type agentAuthData struct {
	hashMap   map[string]agentAuthEntry // SHA-256(token) -> agent entry
	anonymous bool
}

// AgentAuthenticator maps Bearer tokens to agent IDs.
// Thread-safe for concurrent reads and atomic replacement via Swap.
type AgentAuthenticator struct {
	data       atomic.Pointer[agentAuthData]
	oauthCheck TokenValidator    // optional built-in OAuth token validator
	jwtCheck   JWTTokenValidator // optional external AS JWT validator (exclusive — disables hash + OAuth when set)
}

// NewAgentAuthenticator creates an authenticator from agent configs.
// If no agents are configured, all requests are attributed to "anonymous".
func NewAgentAuthenticator(agents []config.AgentConfig) *AgentAuthenticator {
	a := &AgentAuthenticator{}
	a.Swap(agents)
	return a
}

// Swap atomically replaces the auth data with a new set of agents.
// Safe to call concurrently with Authenticate.
// Agents use TokenHash (SHA-256 hex) for O(1) lookup.
func (a *AgentAuthenticator) Swap(agents []config.AgentConfig) {
	if len(agents) == 0 {
		a.data.Store(&agentAuthData{anonymous: true})
		return
	}
	hm := make(map[string]agentAuthEntry, len(agents))
	for _, ag := range agents {
		if ag.TokenHash != "" {
			var expiresAt time.Time
			if ag.ExpiresAt != "" {
				if t, err := time.Parse(time.RFC3339, ag.ExpiresAt); err == nil {
					expiresAt = t
				}
			}
			hm[ag.TokenHash] = agentAuthEntry{AgentID: ag.ID, ExpiresAt: expiresAt}
		}
	}
	a.data.Store(&agentAuthData{hashMap: hm})
}

// SetOAuthValidator sets an optional OAuth token validator. Tokens not found
// in the static agent map are checked against this validator as a fallback.
func (a *AgentAuthenticator) SetOAuthValidator(v TokenValidator) {
	a.oauthCheck = v
}

// SetJWTValidator sets an external AS JWT validator. When set, JWT validation
// is exclusive — static hash tokens and built-in OAuth are skipped. This
// prevents token confusion between the external AS and the built-in OAuth.
func (a *AgentAuthenticator) SetJWTValidator(v JWTTokenValidator) {
	a.jwtCheck = v
}

// AuthResult holds the result of authentication including optional JWT claims.
type AuthResult struct {
	AgentID  string
	OK       bool
	Source   string          // identity source constant
	Claims   *auth.JWTClaims // non-nil when authenticated via JWT
}

// Authenticate extracts the agent ID from the HTTP request.
// Returns (agentID, ok). Use AuthenticateWithClaims for JWT claim access.
func (a *AgentAuthenticator) Authenticate(r *http.Request) (string, bool) {
	res := a.AuthenticateWithClaims(r)
	return res.AgentID, res.OK
}

// AuthenticateWithClaims extracts the agent ID and optional JWT claims from the request.
// Priority: JWT (external AS, exclusive) → static hash → built-in OAuth → fail.
func (a *AgentAuthenticator) AuthenticateWithClaims(r *http.Request) AuthResult {
	d := a.data.Load()
	if d.anonymous && a.jwtCheck == nil {
		return AuthResult{AgentID: "anonymous", OK: true, Source: IdentityAnonymous}
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return AuthResult{}
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		return AuthResult{}
	}

	// Priority 0: JWT validation (external AS)
	// JWTs are tried first. If the token looks like a JWT (3 dot-separated parts)
	// and validation fails, reject it — don't fall through to static tokens.
	// Non-JWT tokens (e.g. rp_... agent tokens) skip JWT validation entirely.
	if a.jwtCheck != nil && isJWTFormat(token) {
		claims, err := a.jwtCheck.Validate(token)
		if err != nil {
			// JWT-formatted token failed validation — reject (don't fall through).
			return AuthResult{}
		}
		agentID := claims.Subject
		if agentID == "" {
			agentID = claims.ClientID // M2M client_credentials tokens use client_id
		}
		if agentID == "" {
			// JWT has no identity claim — reject to prevent unrestricted anonymous access
			return AuthResult{}
		}
		return AuthResult{AgentID: agentID, OK: true, Source: IdentityJWT, Claims: claims}
	}

	// Priority 1: Static agent tokens via SHA-256 hash lookup
	tokenHash := HashToken(token)
	if entry, ok := d.hashMap[tokenHash]; ok {
		if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
			return AuthResult{} // token expired
		}
		return AuthResult{AgentID: entry.AgentID, OK: true, Source: IdentityBearerToken}
	}

	// Priority 2: Built-in OAuth-issued tokens
	if a.oauthCheck != nil && a.oauthCheck.IsValidToken(token) {
		if agentName := a.oauthCheck.AgentForToken(token); agentName != "" {
			return AuthResult{AgentID: agentName, OK: true, Source: IdentityBearerToken}
		}
		return AuthResult{} // OAuth token with no agent mapping — reject
	}

	return AuthResult{}
}

// IsAnonymous returns true if no agent auth is configured.
func (a *AgentAuthenticator) IsAnonymous() bool {
	return a.data.Load().anonymous
}

// ContextWithAgent stores the agent ID in the request context.
func ContextWithAgent(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, agentIDKey, agentID)
}

// contextWithToken stores the bearer token in the request context.
func contextWithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, bearerTokenKey, token)
}

// tokenFromContext retrieves the bearer token from context.
func tokenFromContext(ctx context.Context) string {
	if t, ok := ctx.Value(bearerTokenKey).(string); ok {
		return t
	}
	return ""
}

// AgentFromContext retrieves the agent ID from context.
func AgentFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(agentIDKey).(string); ok {
		return id
	}
	return "anonymous"
}

// ContextWithUnauthorized marks the request as having failed auth.
func ContextWithUnauthorized(ctx context.Context) context.Context {
	return context.WithValue(ctx, unauthorizedKey, true)
}

// IsUnauthorized returns true if the request failed auth enforcement.
func IsUnauthorized(ctx context.Context) bool {
	v, _ := ctx.Value(unauthorizedKey).(bool)
	return v
}

// ContextWithIdentitySource stores the identity source in context.
func ContextWithIdentitySource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, identitySourceKey, source)
}

// IdentitySourceFromContext retrieves the identity source from context.
func IdentitySourceFromContext(ctx context.Context) string {
	if s, ok := ctx.Value(identitySourceKey).(string); ok {
		return s
	}
	return IdentityAnonymous
}

// ContextWithJWTClaims stores JWT claims in context (when authenticated via external AS).
func ContextWithJWTClaims(ctx context.Context, claims *auth.JWTClaims) context.Context {
	return context.WithValue(ctx, jwtClaimsKey, claims)
}

// JWTClaimsFromContext retrieves JWT claims from context. Returns nil if not JWT-authenticated.
func JWTClaimsFromContext(ctx context.Context) *auth.JWTClaims {
	if c, ok := ctx.Value(jwtClaimsKey).(*auth.JWTClaims); ok {
		return c
	}
	return nil
}

// rawArgsKey stores raw tool arguments extracted from HTTP body before mcp-go parsing.
// Workaround for mcp-go v1.3.0 bug where arguments are lost on MRTP retries.
type rawArgsKeyType struct{}
type rawArgsCacheKeyType struct{}

var rawArgsKey = rawArgsKeyType{}

// ContextWithRawArgs stashes raw tool arguments in context (extracted from HTTP body).
func ContextWithRawArgs(ctx context.Context, args map[string]any) context.Context {
	return context.WithValue(ctx, rawArgsKey, args)
}

// RawArgsFromContext retrieves raw tool arguments stashed before mcp-go parsing.
func RawArgsFromContext(ctx context.Context) map[string]any {
	if args, ok := ctx.Value(rawArgsKey).(map[string]any); ok {
		return args
	}
	return nil
}

// StashRawArgs stores raw tool arguments in the global cache, keyed by a caller-provided key.
// Used by the HTTP handler wrapper in cmd/gateway/main.go to preserve args before mcp-go parsing.
func StashRawArgs(key string, args map[string]any) {
	rawArgsCache.Store(key, args)
	go func() {
		time.Sleep(60 * time.Second)
		rawArgsCache.Delete(key)
	}()
}

// AgentResolution holds the result of unified agent identity resolution.
type AgentResolution struct {
	AgentID        string
	IdentitySource string
	ProtocolEra    string
}

// resolveAgent resolves agent identity from all available sources.
// Priority: Bearer token → _meta.clientInfo.name (modern) → session lookup (legacy) → anonymous.
// This is the single point of truth for agent identity — all handlers call this.
func resolveAgent(ctx context.Context, sessionClients *sync.Map) AgentResolution {
	agentID := AgentFromContext(ctx)
	identitySource := IdentitySourceFromContext(ctx)

	// Detect protocol era from mcp-go context (set by the library based on request format)
	era := ProtocolEraLegacy
	if server.IsModernRequest(ctx) {
		era = ProtocolEraModern
	}

	// Priority 1: Bearer token (already resolved in httpContextFunc)
	if agentID != "" && agentID != "anonymous" {
		return AgentResolution{
			AgentID:        agentID,
			IdentitySource: identitySource,
			ProtocolEra:    era,
		}
	}

	// Priority 2: _meta.clientInfo.name (modern 2026-07-28 clients)
	if era == ProtocolEraModern {
		info := server.RequestProtocolInfoFromContext(ctx)
		if info != nil && info.ClientInfo != nil && info.ClientInfo.Name != "" {
			return AgentResolution{
				AgentID:        info.ClientInfo.Name,
				IdentitySource: IdentityMetaClientInfo,
				ProtocolEra:    ProtocolEraModern,
			}
		}
	}

	// Priority 3: Session lookup (legacy clients that went through initialize)
	if session := server.ClientSessionFromContext(ctx); session != nil {
		if name, ok := sessionClients.Load(session.SessionID()); ok {
			return AgentResolution{
				AgentID:        name.(string),
				IdentitySource: IdentityInitializeSession,
				ProtocolEra:    ProtocolEraLegacy,
			}
		}
	}

	// Priority 4: Anonymous fallback
	return AgentResolution{
		AgentID:        "anonymous",
		IdentitySource: IdentityAnonymous,
		ProtocolEra:    era,
	}
}
