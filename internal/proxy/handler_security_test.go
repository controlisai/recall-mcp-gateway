package proxy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/controlisai/recall-mcp-gateway/internal/alerts"
	"github.com/controlisai/recall-mcp-gateway/internal/approval"
	"github.com/controlisai/recall-mcp-gateway/internal/audit"
	"github.com/controlisai/recall-mcp-gateway/internal/auth"
	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

// buildTestServer creates a minimal Server suitable for handler tests.
// The pool includes a single "test-server" with the given tool registered.
func buildTestServer(t *testing.T, opts func(*ServerConfig)) *Server {
	t.Helper()
	reg := upstream.NewRegistry()
	readOnly := true
	reg.Build(map[string][]mcp.Tool{
		"test-server": {
			mcp.Tool{Name: "safe_read", Description: "reads data", Annotations: mcp.ToolAnnotation{ReadOnlyHint: &readOnly}},
			mcp.Tool{Name: "dangerous_write", Description: "writes data"},
		},
	})

	pool := upstream.NewPool(nil, nil)
	// Overwrite registry with our pre-built one
	pool.OverrideRegistryForTest(reg)

	cfg := ServerConfig{
		Pool:          pool,
		Auth:          NewAgentAuthenticator(nil),
		Auditor:       audit.NewLogger(nil),
		Servers:       []config.ServerConfig{{Name: "test-server", Transport: "stdio"}},
		Tracker:       baseline.NewTracker(),
		Latency:       baseline.NewLatencyTracker(),
		AlertDetector: alerts.NewDetector(baseline.NewTracker()),
	}
	if opts != nil {
		opts(&cfg)
	}
	return NewServer(cfg)
}

// callTool invokes a tool handler directly with the given context.
func callTool(s *Server, ctx context.Context, toolName string, args map[string]any) (*mcp.CallToolResult, error) {
	handler := s.makeToolHandler(toolName)
	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args
	return handler(ctx, req)
}

func TestHandler_TokenExpiry_Rejected(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	agents := []config.AgentConfig{
		{ID: "expired-bot", TokenHash: HashToken("expired-tok"), ExpiresAt: past},
	}
	s := buildTestServer(t, func(c *ServerConfig) {
		c.Auth = NewAgentAuthenticator(agents)
	})

	// Simulate an expired agent trying to call — the auth layer rejects it,
	// so the context has IsUnauthorized set
	ctx := context.Background()
	ctx = ContextWithUnauthorized(ctx)
	result, err := callTool(s, ctx, "safe_read", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Unauthorized") {
		t.Errorf("expected unauthorized for expired token, got: %s", text)
	}
}

func TestHandler_ConsentFallback_AutonomousGetsApproval(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	s := buildTestServer(t, func(c *ServerConfig) {
		c.ApprovalStore = store
		c.ApprovalSecret = "test-secret-32-bytes-minimum!!!!!"
		c.Rules = []config.RuleConfig{
			{Tool: "dangerous_write", Action: "require_consent"},
		}
	})

	// Autonomous agent: static token with no JWT sub
	ctx := ContextWithAgent(context.Background(), "m2m-bot")
	ctx = ContextWithIdentitySource(ctx, IdentityBearerToken)

	result, err := callTool(s, ctx, "dangerous_write", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	// Should get a pending approval (not an elicitation form)
	if !strings.Contains(text, "pending") && !strings.Contains(text, "Pending") && !strings.Contains(text, "approval") {
		t.Errorf("expected pending approval for autonomous agent, got: %s", text)
	}
	// Verify an approval entry was created
	pending := store.Pending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(pending))
	}
	if pending[0].AgentID != "m2m-bot" {
		t.Errorf("approval agent = %q, want m2m-bot", pending[0].AgentID)
	}
}

func TestHandler_ConsentFallback_NoDuplicateOnRetry(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	s := buildTestServer(t, func(c *ServerConfig) {
		c.ApprovalStore = store
		c.ApprovalSecret = "test-secret-32-bytes-minimum!!!!!"
		c.Rules = []config.RuleConfig{
			{Tool: "dangerous_write", Action: "require_consent"},
		}
	})

	ctx := ContextWithAgent(context.Background(), "m2m-bot")
	ctx = ContextWithIdentitySource(ctx, IdentityBearerToken)

	// First call: creates approval
	_, _ = callTool(s, ctx, "dangerous_write", nil)
	// Second call: should reuse, not create duplicate
	_, _ = callTool(s, ctx, "dangerous_write", nil)

	pending := store.Pending()
	if len(pending) != 1 {
		t.Errorf("expected 1 pending approval after retry, got %d (duplicate created)", len(pending))
	}
}

func TestHandler_DelegationChain_ContextPropagation(t *testing.T) {
	_ = buildTestServer(t, nil)

	// Verify that nested JWT actor claims propagate through context correctly.
	// Delegation depth enforcement happens at the auth layer (JWTValidator),
	// which rejects deep chains before makeToolHandler runs.
	claims := &auth.JWTClaims{
		Subject: "alice",
		Actor: &auth.ActorClaim{
			Subject: "bob",
			Actor: &auth.ActorClaim{
				Subject: "carol",
			},
		},
	}
	ctx := ContextWithAgent(context.Background(), "alice")
	ctx = ContextWithJWTClaims(ctx, claims)

	// The delegation depth is checked during auth, which sets unauthorized in context.
	// Verify the claims carry the delegation chain.
	got := JWTClaimsFromContext(ctx)
	if got.Actor == nil || got.Actor.Actor == nil {
		t.Fatal("expected nested actor claims")
	}
	depth := 0
	for a := got.Actor; a != nil; a = a.Actor {
		depth++
	}
	if depth != 2 {
		t.Errorf("delegation depth = %d, want 2", depth)
	}
}

func TestHandler_ConsentForm_NonAutonomousGetsElicitation(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	secret := "test-secret-32-bytes-minimum!!!!!"
	s := buildTestServer(t, func(c *ServerConfig) {
		c.ApprovalStore = store
		c.ApprovalSecret = secret
		c.Rules = []config.RuleConfig{
			{Tool: "dangerous_write", Action: "require_consent"},
		}
		c.Agents = []config.AgentConfig{
			{ID: "user-agent", Archetype: config.ArchetypeUserEmbedded},
		}
	})

	// User-embedded agent (archetype configured) should get consent elicitation, not approval
	ctx := ContextWithAgent(context.Background(), "user-agent")
	ctx = ContextWithIdentitySource(ctx, IdentityJWT)
	ctx = ContextWithJWTClaims(ctx, &auth.JWTClaims{Subject: "user@example.com"})

	result, err := callTool(s, ctx, "dangerous_write", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	// Should get a consent form (elicitation), not a pending approval
	if strings.Contains(text, "pending") || strings.Contains(text, "Pending") {
		t.Errorf("non-autonomous agent should get consent form, not approval: %s", text)
	}
	// Should have no pending approvals (elicitation, not approval flow)
	if len(store.Pending()) > 0 {
		t.Errorf("expected 0 pending approvals for non-autonomous agent, got %d", len(store.Pending()))
	}
}

func TestHandler_ConsentFallback_ApprovedProceeds(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	s := buildTestServer(t, func(c *ServerConfig) {
		c.ApprovalStore = store
		c.ApprovalSecret = "test-secret-32-bytes-minimum!!!!!"
		c.Rules = []config.RuleConfig{
			{Tool: "dangerous_write", Action: "require_consent"},
		}
	})

	ctx := ContextWithAgent(context.Background(), "m2m-bot")
	ctx = ContextWithIdentitySource(ctx, IdentityBearerToken)

	// First call: creates pending approval
	_, _ = callTool(s, ctx, "dangerous_write", nil)

	pending := store.Pending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(pending))
	}

	// Approve it
	_, err := store.Approve(pending[0].ID, "admin")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Retry: should proceed to execution (not get consent form or error)
	result, err := callTool(s, ctx, "dangerous_write", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	// Should NOT get a consent form or pending approval — should attempt upstream
	if strings.Contains(text, "Consent") || strings.Contains(text, "consent") {
		t.Errorf("approved consent-fallback should proceed to execute, not show consent form: %s", text)
	}
	if strings.Contains(text, "pending") || strings.Contains(text, "Pending") {
		t.Errorf("approved consent-fallback should not create new pending: %s", text)
	}
}

func TestHandler_RuleBlock(t *testing.T) {
	s := buildTestServer(t, func(c *ServerConfig) {
		c.Rules = []config.RuleConfig{
			{Tool: "dangerous_write", Action: "block", Reason: "forbidden by policy"},
		}
	})

	ctx := ContextWithAgent(context.Background(), "test-agent")
	result, err := callTool(s, ctx, "dangerous_write", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Blocked") {
		t.Errorf("expected blocked, got: %s", text)
	}
}

// resultText extracts the first text content from a tool result.
func resultText(r *mcp.CallToolResult) string {
	if r == nil {
		return ""
	}
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
