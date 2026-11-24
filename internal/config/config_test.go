package config

import (
	"strings"
	"testing"
)

func TestParse_ValidConfig(t *testing.T) {
	yaml := `
proxy:
  port: 9090
  dashboard: true
servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Proxy.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Proxy.Port)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "filesystem" {
		t.Errorf("server name = %q, want filesystem", cfg.Servers[0].Name)
	}
}

func TestParse_DefaultPort(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Proxy.Port != 8090 {
		t.Errorf("default port = %d, want 8090", cfg.Proxy.Port)
	}
}

func TestParse_TransportInference(t *testing.T) {
	yaml := `
servers:
  - name: http-server
    url: http://localhost:3100/mcp
  - name: stdio-server
    command: npx
    args: ["-y", "some-mcp"]
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].Transport != "http" {
		t.Errorf("http server transport = %q, want http", cfg.Servers[0].Transport)
	}
	if cfg.Servers[1].Transport != "stdio" {
		t.Errorf("stdio server transport = %q, want stdio", cfg.Servers[1].Transport)
	}
}

func TestParse_NoServers(t *testing.T) {
	yaml := `
proxy:
  port: 8090
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Servers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(cfg.Servers))
	}
}

func TestParse_DuplicateServerNames(t *testing.T) {
	yaml := `
servers:
  - name: db
    transport: stdio
    command: echo
  - name: db
    transport: stdio
    command: echo
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for duplicate server names")
	}
	if !strings.Contains(err.Error(), "duplicate server name") {
		t.Errorf("error = %q, want 'duplicate server name'", err.Error())
	}
}

func TestParse_DuplicateAgentTokens(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
agents:
  - id: agent1
    token_hash: same-hash
  - id: agent2
    token_hash: same-hash
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for duplicate agent token_hash")
	}
	if !strings.Contains(err.Error(), "duplicate agent token_hash") {
		t.Errorf("error = %q, want 'duplicate agent token_hash'", err.Error())
	}
}

func TestParse_InvalidRuleAction(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
rules:
  - tool: "*"
    action: invalid
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid rule action")
	}
	if !strings.Contains(err.Error(), "allow/block/hide/require_approval/require_consent") {
		t.Errorf("error = %q, want valid actions", err.Error())
	}
}

func TestParse_RequireConsentAction(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
rules:
  - tool: "db_*"
    action: require_consent
    consent_form:
      title: "Confirm database write"
`
	_, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("require_consent with consent_form should be valid: %v", err)
	}
}

func TestParse_RequireConsentWithoutForm(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
rules:
  - tool: "db_*"
    action: require_consent
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("require_consent without consent_form should fail")
	}
	if !strings.Contains(err.Error(), "requires consent_form") {
		t.Errorf("error = %q, want 'requires consent_form'", err.Error())
	}
}

func TestParse_ConsentFormWithWrongAction(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
rules:
  - tool: "db_*"
    action: block
    consent_form:
      title: "Confirm"
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("consent_form with non-consent action should fail")
	}
	if !strings.Contains(err.Error(), "only valid with require_consent") {
		t.Errorf("error = %q, want 'only valid with require_consent'", err.Error())
	}
}

func TestParse_ApprovalConfig(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
proxy:
  approval:
    storage: sqlite
    path: /tmp/approvals.db
    ttl: 10m
    admin_key: my-secret
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Proxy.Approval.Storage != "sqlite" {
		t.Errorf("storage = %q, want sqlite", cfg.Proxy.Approval.Storage)
	}
	if cfg.Proxy.Approval.TTL != "10m" {
		t.Errorf("ttl = %q, want 10m", cfg.Proxy.Approval.TTL)
	}
}

func TestParse_ApprovalConfigInvalidStorage(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
proxy:
  approval:
    storage: redis
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid approval storage")
	}
	if !strings.Contains(err.Error(), "memory/sqlite") {
		t.Errorf("error = %q, want 'memory/sqlite'", err.Error())
	}
}

func TestParse_AuthServerValid(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
auth_server:
  issuer: https://auth.example.com
  audience: https://gateway.example.com/mcp
  jwks_url: https://auth.example.com/.well-known/jwks.json
  approval_secret: test-secret
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AuthServer == nil {
		t.Fatal("auth_server should be parsed")
	}
	if cfg.AuthServer.Issuer != "https://auth.example.com" {
		t.Errorf("issuer = %q, want https://auth.example.com", cfg.AuthServer.Issuer)
	}
	if cfg.AuthServer.Audience != "https://gateway.example.com/mcp" {
		t.Errorf("audience = %q", cfg.AuthServer.Audience)
	}
	if !cfg.HasExternalAS() {
		t.Error("HasExternalAS() should be true")
	}
}

func TestParse_AuthServerMissingIssuer(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
auth_server:
  audience: https://gateway.example.com/mcp
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing issuer")
	}
	if !strings.Contains(err.Error(), "issuer is required") {
		t.Errorf("error = %q, want 'issuer is required'", err.Error())
	}
}

func TestParse_AuthServerInvalidIssuerURL(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
auth_server:
  issuer: not-a-url
  audience: https://gateway.example.com/mcp
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid issuer URL")
	}
	if !strings.Contains(err.Error(), "not a valid URL") {
		t.Errorf("error = %q, want 'not a valid URL'", err.Error())
	}
}

func TestParse_AuthServerMissingAudience(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
auth_server:
  issuer: https://auth.example.com
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing audience")
	}
	if !strings.Contains(err.Error(), "audience is required") {
		t.Errorf("error = %q, want 'audience is required'", err.Error())
	}
}

func TestParse_AuthServerInvalidJWKSURL(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
auth_server:
  issuer: https://auth.example.com
  audience: https://gateway.example.com/mcp
  jwks_url: not-a-url
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid jwks_url")
	}
	if !strings.Contains(err.Error(), "jwks_url is not a valid URL") {
		t.Errorf("error = %q, want 'jwks_url is not a valid URL'", err.Error())
	}
}

func TestParse_AuthServerOptionalJWKSURL(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
auth_server:
  issuer: https://auth.example.com
  audience: https://gateway.example.com/mcp
`
	_, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("jwks_url should be optional: %v", err)
	}
}

func TestParse_NoAuthServer(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AuthServer != nil {
		t.Error("auth_server should be nil when not configured")
	}
	if cfg.HasExternalAS() {
		t.Error("HasExternalAS() should be false")
	}
}

func TestParse_ApprovalSecretEnvExpansion(t *testing.T) {
	t.Setenv("TEST_APPROVAL_SECRET", "my-secret-value")
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
auth_server:
  issuer: https://auth.example.com
  audience: https://gateway.example.com/mcp
  approval_secret: ${TEST_APPROVAL_SECRET}
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AuthServer.ApprovalSecret != "my-secret-value" {
		t.Errorf("approval_secret = %q, want my-secret-value", cfg.AuthServer.ApprovalSecret)
	}
}

func TestParse_ConnectedModeRequiresAPIKey(t *testing.T) {
	yaml := `
proxy:
  engine_url: http://localhost:8084
servers:
  - name: test
    transport: stdio
    command: echo
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing api_key in connected mode")
	}
	if !strings.Contains(err.Error(), "api_key is required") {
		t.Errorf("error = %q, want 'api_key is required'", err.Error())
	}
}

func TestParse_AgentReferencesUnknownServer(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
agents:
  - id: agent1
    token_hash: abcdef1234567890
    allowed_servers: [nonexistent]
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unknown server reference")
	}
	if !strings.Contains(err.Error(), "not found in servers") {
		t.Errorf("error = %q, want 'not found in servers'", err.Error())
	}
}

func TestParse_FullConfig(t *testing.T) {
	yaml := `
proxy:
  port: 8090
  engine_url: http://localhost:8084
  api_key: cai_rcl_sdk_dev_xxx
  dashboard: true
servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
  - name: database
    transport: http
    url: http://localhost:3100/mcp
    cost_per_call: 0.04
products:
  - name: case-intake
    api_key: cai_rcl_sdk_prod_xxx
agents:
  - id: support-bot
    token_hash: a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2
    product: case-intake
    allowed_servers: [filesystem]
rules:
  - tool: "db_delete*"
    action: block
    reason: "No destructive ops"
  - tool: "*"
    action: allow
alerts:
  - tool: "*"
    auto_detect: true
    window: "5m"
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Servers) != 2 {
		t.Errorf("servers = %d, want 2", len(cfg.Servers))
	}
	if len(cfg.Products) != 1 {
		t.Errorf("products = %d, want 1", len(cfg.Products))
	}
	if len(cfg.Agents) != 1 {
		t.Errorf("agents = %d, want 1", len(cfg.Agents))
	}
	if len(cfg.Rules) != 2 {
		t.Errorf("rules = %d, want 2", len(cfg.Rules))
	}
	if len(cfg.Alerts) != 1 {
		t.Errorf("alerts = %d, want 1", len(cfg.Alerts))
	}
	if cfg.Servers[1].CostPerCall != 0.04 {
		t.Errorf("cost_per_call = %f, want 0.04", cfg.Servers[1].CostPerCall)
	}
}

func TestAgentConfig_HasServerAccess(t *testing.T) {
	tests := []struct {
		name   string
		agent  AgentConfig
		server string
		want   bool
	}{
		{
			name:   "unrestricted agent has access to any server",
			agent:  AgentConfig{ID: "a1"},
			server: "database",
			want:   true,
		},
		{
			name:   "restricted flag with empty allowed = no access",
			agent:  AgentConfig{ID: "a2", ServerAccessRestricted: true},
			server: "database",
			want:   false,
		},
		{
			name:   "allowed server matches",
			agent:  AgentConfig{ID: "a3", AllowedServers: []string{"database", "files"}},
			server: "database",
			want:   true,
		},
		{
			name:   "allowed server does not match",
			agent:  AgentConfig{ID: "a4", AllowedServers: []string{"database"}},
			server: "comms",
			want:   false,
		},
		{
			name:   "single allowed server exact match",
			agent:  AgentConfig{ID: "a5", AllowedServers: []string{"auth-oauth"}},
			server: "auth-oauth",
			want:   true,
		},
		{
			name:   "empty server name",
			agent:  AgentConfig{ID: "a6", AllowedServers: []string{"database"}},
			server: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.agent.HasServerAccess(tt.server)
			if got != tt.want {
				t.Errorf("HasServerAccess(%q) = %v, want %v", tt.server, got, tt.want)
			}
		})
	}
}

func TestParse_AgentExpiresAt(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
agents:
  - id: bot1
    token_hash: abc123
    expires_at: "2030-01-01T00:00:00Z"
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("valid expires_at: %v", err)
	}
	if cfg.Agents[0].ExpiresAt != "2030-01-01T00:00:00Z" {
		t.Errorf("expires_at = %q", cfg.Agents[0].ExpiresAt)
	}
}

func TestParse_AgentExpiresAtInvalid(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
agents:
  - id: bot1
    token_hash: abc123
    expires_at: "not-a-date"
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("invalid expires_at should fail validation")
	}
	if !strings.Contains(err.Error(), "RFC3339") {
		t.Errorf("error = %q, want RFC3339 mention", err.Error())
	}
}
