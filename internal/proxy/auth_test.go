package proxy

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/auth"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

func TestAgentAuthenticator_Anonymous(t *testing.T) {
	auth := NewAgentAuthenticator(nil)

	if !auth.IsAnonymous() {
		t.Error("expected anonymous mode")
	}

	req, _ := http.NewRequest("GET", "/", nil)
	id, ok := auth.Authenticate(req)
	if !ok {
		t.Error("anonymous auth should succeed")
	}
	if id != "anonymous" {
		t.Errorf("id = %q, want anonymous", id)
	}
}

func TestAgentAuthenticator_ValidToken(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "bot-1", TokenHash: HashToken("tok-abc")},
		{ID: "bot-2", TokenHash: HashToken("tok-def")},
	}
	auth := NewAgentAuthenticator(agents)

	if auth.IsAnonymous() {
		t.Error("should not be anonymous")
	}

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer tok-abc")

	id, ok := auth.Authenticate(req)
	if !ok {
		t.Error("expected auth success")
	}
	if id != "bot-1" {
		t.Errorf("id = %q, want bot-1", id)
	}
}

func TestAgentAuthenticator_InvalidToken(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "bot-1", TokenHash: HashToken("tok-abc")},
	}
	auth := NewAgentAuthenticator(agents)

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	_, ok := auth.Authenticate(req)
	if ok {
		t.Error("expected auth failure")
	}
}

func TestAgentAuthenticator_NoHeader(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "bot-1", TokenHash: HashToken("tok-abc")},
	}
	auth := NewAgentAuthenticator(agents)

	req, _ := http.NewRequest("GET", "/", nil)

	_, ok := auth.Authenticate(req)
	if ok {
		t.Error("expected auth failure with no header")
	}
}

func TestAgentAuthenticator_NoBearerPrefix(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "bot-1", TokenHash: HashToken("tok-abc")},
	}
	auth := NewAgentAuthenticator(agents)

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "tok-abc") // missing "Bearer "

	_, ok := auth.Authenticate(req)
	if ok {
		t.Error("expected auth failure without Bearer prefix")
	}
}

func TestHashToken(t *testing.T) {
	// SHA-256 of empty string is well-known
	hash := HashToken("")
	if hash != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("unexpected hash of empty string: %s", hash)
	}
	// Deterministic
	if HashToken("test") != HashToken("test") {
		t.Error("hash should be deterministic")
	}
	// Different inputs → different hashes
	if HashToken("a") == HashToken("b") {
		t.Error("different inputs should produce different hashes")
	}
}

func TestContextWithAgent(t *testing.T) {
	ctx := context.Background()
	ctx = ContextWithAgent(ctx, "my-agent")

	got := AgentFromContext(ctx)
	if got != "my-agent" {
		t.Errorf("agent = %q, want my-agent", got)
	}
}

func TestAgentFromContext_Default(t *testing.T) {
	got := AgentFromContext(context.Background())
	if got != "anonymous" {
		t.Errorf("default agent = %q, want anonymous", got)
	}
}

// mockJWTValidator implements JWTTokenValidator for testing.
type mockJWTValidator struct {
	claims *auth.JWTClaims
	err    error
}

func (m *mockJWTValidator) Validate(token string) (*auth.JWTClaims, error) {
	return m.claims, m.err
}

func TestAgentAuthenticator_JWTPriority(t *testing.T) {
	// JWT-formatted token matches both static hash and JWT validator — JWT should win
	jwtToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJqd3QtdXNlciJ9.fakesig"
	agents := []config.AgentConfig{
		{ID: "static-bot", TokenHash: HashToken(jwtToken)},
	}
	a := NewAgentAuthenticator(agents)
	a.SetJWTValidator(&mockJWTValidator{
		claims: &auth.JWTClaims{Subject: "jwt-user"},
	})

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	res := a.AuthenticateWithClaims(req)
	if !res.OK {
		t.Fatal("expected auth success")
	}
	if res.AgentID != "jwt-user" {
		t.Errorf("agentID = %q, want jwt-user (JWT should take priority over static hash)", res.AgentID)
	}
	if res.Source != IdentityJWT {
		t.Errorf("source = %q, want %q", res.Source, IdentityJWT)
	}
	if res.Claims == nil {
		t.Error("claims should be non-nil for JWT auth")
	}
}

func TestAgentAuthenticator_JWTExclusiveForJWTTokens(t *testing.T) {
	// JWT-formatted token fails JWT validation — should NOT fall through to static hash
	jwtToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ0ZXN0In0.badsig"
	agents := []config.AgentConfig{
		{ID: "static-bot", TokenHash: HashToken(jwtToken)},
	}
	a := NewAgentAuthenticator(agents)
	a.SetJWTValidator(&mockJWTValidator{
		err: auth.ErrInvalidSignature,
	})

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	res := a.AuthenticateWithClaims(req)
	if res.OK {
		t.Fatal("JWT-formatted token that fails JWT validation should not fall through to static hash")
	}
}

func TestAgentAuthenticator_StaticTokenWithJWTConfigured(t *testing.T) {
	// Non-JWT token (rp_...) should bypass JWT validation and use static hash
	agents := []config.AgentConfig{
		{ID: "static-bot", TokenHash: HashToken("rp_abc123")},
	}
	a := NewAgentAuthenticator(agents)
	a.SetJWTValidator(&mockJWTValidator{
		err: auth.ErrInvalidSignature, // would reject if tried
	})

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer rp_abc123")

	res := a.AuthenticateWithClaims(req)
	if !res.OK {
		t.Fatal("non-JWT token should fall through to static hash even with JWT validator configured")
	}
	if res.AgentID != "static-bot" {
		t.Errorf("agentID = %q, want static-bot", res.AgentID)
	}
	if res.Source != IdentityBearerToken {
		t.Errorf("source = %q, want %q", res.Source, IdentityBearerToken)
	}
}

func TestAgentAuthenticator_JWTClientID(t *testing.T) {
	// M2M token: empty sub, has client_id
	jwtToken := "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9.eyJjbGllbnRfaWQiOiJzZXJ2aWNlLWFiYyJ9.fakesig"
	a := NewAgentAuthenticator(nil)
	a.SetJWTValidator(&mockJWTValidator{
		claims: &auth.JWTClaims{ClientID: "service-abc"},
	})

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	res := a.AuthenticateWithClaims(req)
	if !res.OK {
		t.Fatal("expected auth success")
	}
	if res.AgentID != "service-abc" {
		t.Errorf("agentID = %q, want service-abc (should use ClientID when sub is empty)", res.AgentID)
	}
}

func TestAgentAuthenticator_JWTNoSubNoClientID(t *testing.T) {
	// JWT with no sub and no client_id — rejected (no identity = no access)
	a := NewAgentAuthenticator(nil)
	a.SetJWTValidator(&mockJWTValidator{
		claims: &auth.JWTClaims{},
	})

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer eyJ0eXAiOiJKV1QifQ.eyJ9.nosig")

	res := a.AuthenticateWithClaims(req)
	if res.OK {
		t.Fatal("expected auth rejection for JWT with no identity claims")
	}
}

func TestContextWithJWTClaims(t *testing.T) {
	claims := &auth.JWTClaims{Subject: "alice", Issuer: "https://auth.example.com"}
	ctx := ContextWithJWTClaims(context.Background(), claims)

	got := JWTClaimsFromContext(ctx)
	if got == nil {
		t.Fatal("expected claims in context")
	}
	if got.Subject != "alice" {
		t.Errorf("subject = %q, want alice", got.Subject)
	}
}

func TestJWTClaimsFromContext_Nil(t *testing.T) {
	got := JWTClaimsFromContext(context.Background())
	if got != nil {
		t.Error("expected nil claims from empty context")
	}
}

func TestAgentAuthenticator_TokenExpiry(t *testing.T) {
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	agents := []config.AgentConfig{
		{ID: "valid-agent", TokenHash: HashToken("valid-token"), ExpiresAt: future},
		{ID: "expired-agent", TokenHash: HashToken("expired-token"), ExpiresAt: past},
		{ID: "no-expiry", TokenHash: HashToken("no-expiry-token")},
	}
	a := NewAgentAuthenticator(agents)

	// Valid token with future expiry → succeeds
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	res := a.AuthenticateWithClaims(req)
	if !res.OK || res.AgentID != "valid-agent" {
		t.Errorf("valid token: OK=%v, agent=%q, want OK=true, agent=valid-agent", res.OK, res.AgentID)
	}

	// Expired token → fails
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.Header.Set("Authorization", "Bearer expired-token")
	res2 := a.AuthenticateWithClaims(req2)
	if res2.OK {
		t.Error("expired token should fail authentication")
	}

	// Token without expiry → succeeds (never expires)
	req3, _ := http.NewRequest("GET", "/", nil)
	req3.Header.Set("Authorization", "Bearer no-expiry-token")
	res3 := a.AuthenticateWithClaims(req3)
	if !res3.OK || res3.AgentID != "no-expiry" {
		t.Errorf("no-expiry token: OK=%v, agent=%q, want OK=true, agent=no-expiry", res3.OK, res3.AgentID)
	}
}

func TestAgentAuthenticator_InvalidExpiresAtIgnored(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "bad-date", TokenHash: HashToken("tok"), ExpiresAt: "not-a-date"},
	}
	a := NewAgentAuthenticator(agents)

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer tok")
	res := a.AuthenticateWithClaims(req)
	// Invalid date = treated as no expiry = succeeds
	if !res.OK {
		t.Error("invalid expires_at should be treated as no expiry")
	}
}
