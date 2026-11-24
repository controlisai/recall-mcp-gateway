package proxy

import (
	"context"
	"net/http"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

const orgIDKey contextKey = "orgID"

// OrgIDHeader is the HTTP header for per-request org routing.
const OrgIDHeader = "X-Recall-Org-Id"

// OrgResolver resolves the org_id for a request using 3-level priority:
//  1. Request header: X-Recall-Org-Id
//  2. Agent config: org_id field
//  3. Default: proxy.default_org_id
type OrgResolver struct {
	agentOrgs    map[string]string // agentID -> org_id from config
	defaultOrgID string
}

// NewOrgResolver creates a resolver from agent configs and default org.
func NewOrgResolver(agents []config.AgentConfig, defaultOrgID string) *OrgResolver {
	orgs := make(map[string]string, len(agents))
	for _, a := range agents {
		if a.OrgID != "" {
			orgs[a.ID] = a.OrgID
		}
	}
	return &OrgResolver{
		agentOrgs:    orgs,
		defaultOrgID: defaultOrgID,
	}
}

// Resolve returns the org_id for a request, using 3-level priority.
func (r *OrgResolver) Resolve(req *http.Request, agentID string) string {
	// Priority 1: Request header
	if req != nil {
		if headerOrg := req.Header.Get(OrgIDHeader); headerOrg != "" {
			return headerOrg
		}
	}

	// Priority 2: Agent config
	if orgID, ok := r.agentOrgs[agentID]; ok {
		return orgID
	}

	// Priority 3: Default
	return r.defaultOrgID
}

// ResolveFromContext returns the org_id stored in context (set during HTTP context injection).
func (r *OrgResolver) ResolveFromContext(ctx context.Context, agentID string) string {
	// Check if org was already resolved and stored in context (from header)
	if orgID, ok := ctx.Value(orgIDKey).(string); ok && orgID != "" {
		return orgID
	}

	// Fall back to agent config → default
	if orgID, ok := r.agentOrgs[agentID]; ok {
		return orgID
	}
	return r.defaultOrgID
}

// ContextWithOrg stores the resolved org_id in context.
func ContextWithOrg(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, orgIDKey, orgID)
}

// OrgFromContext retrieves the org_id from context.
func OrgFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(orgIDKey).(string); ok {
		return id
	}
	return ""
}
