package proxy

import (
	"context"
	"net/http"
	"testing"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

func TestOrgResolver_HeaderPriority(t *testing.T) {
	resolver := NewOrgResolver([]config.AgentConfig{
		{ID: "agent1", OrgID: "config-org"},
	}, "default-org")

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Recall-Org-Id", "header-org")

	orgID := resolver.Resolve(req, "agent1")
	if orgID != "header-org" {
		t.Errorf("orgID = %q, want header-org (header has priority)", orgID)
	}
}

func TestOrgResolver_AgentConfigPriority(t *testing.T) {
	resolver := NewOrgResolver([]config.AgentConfig{
		{ID: "agent1", OrgID: "config-org"},
	}, "default-org")

	req, _ := http.NewRequest("GET", "/", nil)
	// No header set

	orgID := resolver.Resolve(req, "agent1")
	if orgID != "config-org" {
		t.Errorf("orgID = %q, want config-org", orgID)
	}
}

func TestOrgResolver_DefaultFallback(t *testing.T) {
	resolver := NewOrgResolver([]config.AgentConfig{
		{ID: "agent1"}, // no org_id
	}, "default-org")

	req, _ := http.NewRequest("GET", "/", nil)

	orgID := resolver.Resolve(req, "agent1")
	if orgID != "default-org" {
		t.Errorf("orgID = %q, want default-org", orgID)
	}
}

func TestOrgResolver_UnknownAgent(t *testing.T) {
	resolver := NewOrgResolver(nil, "default-org")

	req, _ := http.NewRequest("GET", "/", nil)

	orgID := resolver.Resolve(req, "unknown-agent")
	if orgID != "default-org" {
		t.Errorf("orgID = %q, want default-org", orgID)
	}
}

func TestOrgResolver_NilRequest(t *testing.T) {
	resolver := NewOrgResolver([]config.AgentConfig{
		{ID: "agent1", OrgID: "config-org"},
	}, "default-org")

	orgID := resolver.Resolve(nil, "agent1")
	if orgID != "config-org" {
		t.Errorf("orgID = %q, want config-org (nil request skips header)", orgID)
	}
}

func TestOrgResolver_EmptyHeaderFallsThrough(t *testing.T) {
	resolver := NewOrgResolver([]config.AgentConfig{
		{ID: "agent1", OrgID: "config-org"},
	}, "default-org")

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Recall-Org-Id", "") // empty header

	orgID := resolver.Resolve(req, "agent1")
	if orgID != "config-org" {
		t.Errorf("orgID = %q, want config-org (empty header falls through)", orgID)
	}
}

func TestOrgResolver_MultiTenantAgent(t *testing.T) {
	// Agent with no org_id — relies on header per request
	resolver := NewOrgResolver([]config.AgentConfig{
		{ID: "multi-tenant"}, // no org_id
	}, "default-org")

	// Request for hospital-a
	req1, _ := http.NewRequest("GET", "/", nil)
	req1.Header.Set("X-Recall-Org-Id", "hospital-a")
	if org := resolver.Resolve(req1, "multi-tenant"); org != "hospital-a" {
		t.Errorf("org = %q, want hospital-a", org)
	}

	// Request for clinic-b
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.Header.Set("X-Recall-Org-Id", "clinic-b")
	if org := resolver.Resolve(req2, "multi-tenant"); org != "clinic-b" {
		t.Errorf("org = %q, want clinic-b", org)
	}

	// Request with no header — falls to default
	req3, _ := http.NewRequest("GET", "/", nil)
	if org := resolver.Resolve(req3, "multi-tenant"); org != "default-org" {
		t.Errorf("org = %q, want default-org", org)
	}
}

func TestOrgResolver_ResolveFromContext(t *testing.T) {
	resolver := NewOrgResolver([]config.AgentConfig{
		{ID: "agent1", OrgID: "config-org"},
	}, "default-org")

	// Context with org set (simulating header resolution)
	ctx := ContextWithOrg(context.Background(), "header-org")
	orgID := resolver.ResolveFromContext(ctx, "agent1")
	if orgID != "header-org" {
		t.Errorf("orgID = %q, want header-org from context", orgID)
	}

	// Context without org — falls to agent config
	ctx2 := context.Background()
	orgID2 := resolver.ResolveFromContext(ctx2, "agent1")
	if orgID2 != "config-org" {
		t.Errorf("orgID = %q, want config-org", orgID2)
	}

	// Unknown agent, no context — falls to default
	orgID3 := resolver.ResolveFromContext(ctx2, "unknown")
	if orgID3 != "default-org" {
		t.Errorf("orgID = %q, want default-org", orgID3)
	}
}

func TestContextWithOrg_RoundTrip(t *testing.T) {
	ctx := ContextWithOrg(context.Background(), "test-org")
	got := OrgFromContext(ctx)
	if got != "test-org" {
		t.Errorf("org = %q, want test-org", got)
	}
}

func TestOrgFromContext_Default(t *testing.T) {
	got := OrgFromContext(context.Background())
	if got != "" {
		t.Errorf("org = %q, want empty", got)
	}
}
