package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level proxy configuration.
type Config struct {
	Proxy    ProxyConfig    `yaml:"proxy"`
	Servers  []ServerConfig `yaml:"servers"`
	Products []ProductConfig `yaml:"products,omitempty"`
	Agents   []AgentConfig  `yaml:"agents,omitempty"`
	Rules    []RuleConfig        `yaml:"rules,omitempty"`
	Alerts   []AlertConfig       `yaml:"alerts,omitempty"`
	LLM      *LLMConfig          `yaml:"llm,omitempty"`
	AuthServer *AuthServerConfig  `yaml:"auth_server,omitempty"`
	Redis    *RedisConfig         `yaml:"redis,omitempty"`
	Postgres *PostgresConfig      `yaml:"postgres,omitempty"`
}

// PostgresConfig configures optional PostgreSQL for shared durable state.
// When set, PostgreSQL replaces SQLite as the gateway store.
// Use for single-pod (PG durability) or multi-pod (shared state) deployments.
type PostgresConfig struct {
	URL      string `yaml:"url"`                    // postgres://user:pass@host:5432/recall_gateway
	MaxConns int    `yaml:"max_conns,omitempty"`     // connection pool size (default 25)
}

// HasPostgres returns true when PostgreSQL is configured as the store backend.
func (c *Config) HasPostgres() bool {
	return c.Postgres != nil && c.Postgres.URL != ""
}

// RedisConfig configures optional Redis for multi-pod shared state.
// When absent, the gateway runs in standalone mode (SQLite + memory).
type RedisConfig struct {
	URL      string `yaml:"url"`                     // redis://host:6379 or rediss:// for TLS
	PoolSize int    `yaml:"pool_size,omitempty"`      // connection pool size (default 10)
	Password string `yaml:"password,omitempty"`       // auth password (supports ${ENV_VAR})
	TLS      bool   `yaml:"tls,omitempty"`            // enable TLS
}

// AuthServerConfig configures an external OAuth Authorization Server for JWT validation.
// When set, the gateway validates JWTs from this AS instead of using the built-in OAuth server.
// When nil, the built-in OAuth AS is used (zero-config default for local dev).
type AuthServerConfig struct {
	Issuer             string `yaml:"issuer" json:"issuer"`                                    // AS issuer URL (required)
	Audience           string `yaml:"audience" json:"audience"`                                // expected JWT audience — typically the gateway's MCP endpoint URL (required)
	JWKSURL            string `yaml:"jwks_url,omitempty" json:"jwks_url,omitempty"`            // JWKS endpoint — auto-discovered from issuer if omitted
	ApprovalSecret     string `yaml:"approval_secret,omitempty" json:"approval_secret,omitempty"` // key for requestState AEAD encryption
	MaxDelegationDepth int    `yaml:"max_delegation_depth,omitempty" json:"maxDelegationDepth,omitempty"` // max JWT act nesting depth (0 = unlimited, which is the default)
	SigningKeyFile     string `yaml:"signing_key_file,omitempty" json:"signingKeyFile,omitempty"`         // PEM-encoded RSA private key for token exchange (ephemeral if omitted)
}

// HasExternalAS returns true when an external Authorization Server is configured.
func (c *Config) HasExternalAS() bool {
	return c.AuthServer != nil && c.AuthServer.Issuer != ""
}

// LLMConfig configures optional LLM-powered recommendations.
type LLMConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Provider  string `yaml:"provider"`     // openai, anthropic, ollama
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
	URL       string `yaml:"url,omitempty"` // for ollama
}

// ProxyConfig controls the proxy's own behavior.
type ProxyConfig struct {
	Port             int    `yaml:"port"`
	EngineURL     string `yaml:"engine_url,omitempty"`
	APIKey        string `yaml:"api_key,omitempty"`
	ConsoleAPIURL string `yaml:"console_api_url,omitempty"`
	ServiceToken  string `yaml:"service_token,omitempty"`
	Dashboard     bool   `yaml:"dashboard"`
	DashboardUser     string `yaml:"dashboard_user,omitempty"`      // basic auth username
	DashboardPassHash string `yaml:"dashboard_pass_hash,omitempty"` // bcrypt hash of dashboard password
	MaxBodySize       string `yaml:"max_body_size,omitempty"`       // max HTTP request body size (default "10MB")
	RateLimit         int    `yaml:"rate_limit,omitempty"`          // global HTTP rate limit per IP (req/s, 0=disabled)
	LogLevel          string `yaml:"log_level,omitempty"`           // debug, info, warn, error (default "info")
	MetricsEnabled    *bool  `yaml:"metrics_enabled,omitempty"`     // enable /metrics endpoint (default true)
	SkipLocalRules    *bool  `yaml:"skip_local_rules,omitempty"`   // skip proxy rules when engine connected (default true)
	Product           string `yaml:"product,omitempty"`             // informational product name (shows in logs, audit, dashboard)
	TLSCert       string `yaml:"tls_cert,omitempty"`       // TLS certificate path
	TLSKey        string `yaml:"tls_key,omitempty"`        // TLS private key path
	RulesFile     string `yaml:"rules_file,omitempty"`     // rules persistence path
	AuditLog      string `yaml:"audit_log,omitempty"`      // legacy JSONL path (deprecated, use Audit)
	Audit         AuditConfig `yaml:"audit,omitempty"`
	DefaultOrgID      string `yaml:"default_org_id,omitempty"`
	MultiTenant       bool   `yaml:"multi_tenant,omitempty"`
	OAuthCallbackPort int    `yaml:"oauth_callback_port,omitempty"` // default 8091
	ClientTTLMs       int64         `yaml:"client_ttl_ms,omitempty"`       // TTL in ms for ListTools response to clients (default 30000)
	DefaultAction     string        `yaml:"default_action,omitempty"`      // "allow" (default) or "block" — action when no rule matches
	McpHeaderMode     string        `yaml:"mcp_header_mode,omitempty"`     // "passthrough" (default), "generate", "managed" — x-mcp-header handling mode
	Tracing           TracingConfig `yaml:"tracing,omitempty"`
	Approval          ApprovalConfig `yaml:"approval,omitempty"`
	AllowedOrigins    []string      `yaml:"allowed_origins,omitempty"` // allowed Origin headers (empty = allow all)
	TrustedProxies    []string      `yaml:"trusted_proxies,omitempty"` // CIDR ranges to trust X-Forwarded-For from (empty = never trust)
}

// ApprovalConfig controls the approval persistence store.
type ApprovalConfig struct {
	Storage  string `yaml:"storage,omitempty"`   // "memory" (default), "db" (shared gateway DB), or "sqlite" (alias for db)
	Path     string `yaml:"path,omitempty"`      // SQLite DB path (for sqlite storage)
	TTL      string `yaml:"ttl,omitempty"`       // pending entry TTL, e.g. "5m" (default)
	AdminKey          string `yaml:"admin_key,omitempty"` // shared secret for approval endpoints (supports ${ENV_VAR})
	SlackSigningSecret string `yaml:"slack_signing_secret,omitempty"` // Slack app signing secret for interactive callbacks (supports ${ENV_VAR})
}

// ConsentFormConfig defines fields shown in a consent/elicitation form.
type ConsentFormConfig struct {
	Title       string   `yaml:"title,omitempty" json:"title,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Fields      []string `yaml:"fields,omitempty" json:"fields,omitempty"`
}

// TracingConfig controls OpenTelemetry trace export.
type TracingConfig struct {
	Enabled     bool   `yaml:"enabled,omitempty"`      // enable OTel tracing (default false)
	Endpoint    string `yaml:"endpoint,omitempty"`      // OTLP collector endpoint (e.g. "localhost:4317")
	ServiceName string `yaml:"service_name,omitempty"`  // service name in traces (default "recall-mcp-gateway")
}

// ResolveHeaderMode returns the effective mcp_header_mode for a server.
// Resolution: per-server → global → default "passthrough".
func ResolveHeaderMode(serverMode, globalMode string) string {
	if serverMode != "" {
		return serverMode
	}
	if globalMode != "" {
		return globalMode
	}
	return "passthrough"
}

// AuditConfig controls audit entry storage.
type AuditConfig struct {
	Storage    string `yaml:"storage,omitempty"`     // "db" (default, shared gateway DB), "jsonl", "memory". "sqlite" is alias for "db".
	Path       string `yaml:"path,omitempty"`        // file path (env: RECALL_AUDIT_PATH)
	Retention  string `yaml:"retention,omitempty"`   // auto-purge duration, e.g. "30d" (default)
	MaxEntries int    `yaml:"max_entries,omitempty"` // hard cap, 0 = unlimited
}

// ServerConfig defines an upstream MCP server.
type ServerConfig struct {
	Name        string            `yaml:"name" json:"name"`
	URL         string            `yaml:"url,omitempty" json:"url,omitempty"`
	Transport   string            `yaml:"transport" json:"transport"`
	Command     string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args        []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env         map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Headers     map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	CostPerCall float64           `yaml:"cost_per_call,omitempty" json:"cost_per_call,omitempty"`
	Auth        *ServerAuthConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
	ServerType      string                   `yaml:"server_type,omitempty" json:"server_type,omitempty"`       // baseline profile: filesystem, github, database, slack, web-search, generic
	McpHeaderMode   string                   `yaml:"mcp_header_mode,omitempty" json:"mcpHeaderMode,omitempty"` // per-server: passthrough, generate, managed (overrides global)
	ToolAnnotations []ToolAnnotationOverride  `yaml:"tool_annotations,omitempty" json:"toolAnnotations,omitempty"` // per-tool annotation overrides
}

// ToolAnnotationOverride lets operators override MCP tool annotation hints.
// Nil pointer fields mean "keep upstream value". Supports glob patterns in Tool field.
type ToolAnnotationOverride struct {
	Tool            string `yaml:"tool" json:"tool"`                                               // tool name or glob pattern
	Title           string `yaml:"title,omitempty" json:"title,omitempty"`                         // override annotation title
	ReadOnlyHint    *bool  `yaml:"read_only_hint,omitempty" json:"readOnlyHint,omitempty"`         // override readOnlyHint
	DestructiveHint *bool  `yaml:"destructive_hint,omitempty" json:"destructiveHint,omitempty"`    // override destructiveHint
	IdempotentHint  *bool  `yaml:"idempotent_hint,omitempty" json:"idempotentHint,omitempty"`      // override idempotentHint
	OpenWorldHint   *bool  `yaml:"open_world_hint,omitempty" json:"openWorldHint,omitempty"`       // override openWorldHint
}

// ServerAuthConfig defines per-server authentication for upstream connections.
type ServerAuthConfig struct {
	Type         string            `yaml:"type" json:"type"`                                     // "none", "bearer", "basic", "headers", "oauth_client_credentials", "mtls"
	Token        string            `yaml:"token,omitempty" json:"token,omitempty"`                // bearer token (supports ${ENV_VAR})
	Username     string            `yaml:"username,omitempty" json:"username,omitempty"`          // basic auth
	Password     string            `yaml:"password,omitempty" json:"password,omitempty"`          // basic auth (supports ${ENV_VAR})
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

// ProductConfig maps a logical product name to an SDK API key (connected mode).
type ProductConfig struct {
	Name   string `yaml:"name"`
	APIKey string `yaml:"api_key"`
}

// AgentConfig defines an agent identity and its access scope.
type AgentConfig struct {
	ID             string   `yaml:"id" json:"id"`
	TokenHash      string   `yaml:"token_hash" json:"tokenHash,omitempty"` // SHA-256 hex of bearer token
	Product        string   `yaml:"product,omitempty" json:"product,omitempty"`
	AllowedServers          []string `yaml:"allowed_servers,omitempty" json:"allowedServers,omitempty"`
	ServerAccessRestricted  bool     `yaml:"server_access_restricted,omitempty" json:"serverAccessRestricted,omitempty"` // true = user explicitly configured access; [] means no access, not unrestricted
	BlockedTools      []string `yaml:"blocked_tools,omitempty" json:"blockedTools,omitempty"`           // tools this agent cannot call or see
	HiddenTools       []string `yaml:"hidden_tools,omitempty" json:"hiddenTools,omitempty"`             // tools hidden from listing but still callable
	AcknowledgedTools []string `yaml:"acknowledged_tools,omitempty" json:"acknowledgedTools,omitempty"` // tools where risk is explicitly accepted
	OrgID          string   `yaml:"org_id,omitempty" json:"orgId,omitempty"`
	ExpiresAt      string   `yaml:"expires_at,omitempty" json:"expiresAt,omitempty"` // ISO 8601 token expiry (empty = never expires)
	Archetype      string   `yaml:"archetype,omitempty" json:"archetype,omitempty"` // autonomous (default), user_embedded, delegated
	Source         string   `yaml:"-" json:"source,omitempty"` // "config", "dashboard", "auto-discovered"
}

// Archetype constants for agent classification.
const (
	ArchetypeAutonomous   = "autonomous"
	ArchetypeUserEmbedded = "user_embedded"
	ArchetypeDelegated    = "delegated"
)

// HasServerAccess checks if this agent has access to a given server.
// Unrestricted agents (no AllowedServers, not ServerAccessRestricted) can access all servers.
func (a AgentConfig) HasServerAccess(server string) bool {
	if len(a.AllowedServers) == 0 {
		if a.ServerAccessRestricted {
			return false
		}
		return true
	}
	for _, s := range a.AllowedServers {
		if s == server {
			return true
		}
	}
	return false
}

// RuleConfig defines a governance rule for tool calls.
type RuleConfig struct {
	Tool           string             `yaml:"tool" json:"tool"`
	Action         string             `yaml:"action,omitempty" json:"action,omitempty"`             // single action (deprecated — use Actions)
	Actions        map[string]string  `yaml:"actions,omitempty" json:"actions,omitempty"`           // per-archetype: autonomous, user_embedded, delegated → action
	Reason         string             `yaml:"reason,omitempty" json:"reason,omitempty"`
	Agents         []string           `yaml:"agents,omitempty" json:"agents,omitempty"`
	Server         string             `yaml:"server,omitempty" json:"server,omitempty"`
	Classification string             `yaml:"classification,omitempty" json:"classification,omitempty"`
	RateLimit      string             `yaml:"rate_limit,omitempty" json:"rate_limit,omitempty"`
	Notify         string             `yaml:"notify,omitempty" json:"notify,omitempty"`
	Schedule       *ScheduleConfig    `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	ParamBlocks    []ParamBlockConfig `yaml:"param_blocks,omitempty" json:"param_blocks,omitempty"`
	RetryMethod    string             `yaml:"retry_method,omitempty" json:"retryMethod,omitempty"`       // "poll" (default) or "callback" — for require_approval rules
	RetryHintMs    int64              `yaml:"retry_hint_ms,omitempty" json:"retryHintMs,omitempty"`      // suggested retry interval in ms (poll mode, default 30000)
	StatusWebhook  string             `yaml:"status_webhook,omitempty" json:"statusWebhook,omitempty"`   // webhook URL fired on approval resolution (callback mode)
	ConsentForm      *ConsentFormConfig `yaml:"consent_form,omitempty" json:"consentForm,omitempty"`       // form definition for require_consent rules
	DelegationNotify string              `yaml:"delegation_notify,omitempty" json:"delegationNotify,omitempty"` // webhook URL for delegated user notification (in addition to Notify for admin)
	RequiredScopes   []string            `yaml:"required_scopes,omitempty" json:"requiredScopes,omitempty"`     // OAuth scopes required for this tool — triggers 403 scope step-up if insufficient
}

// ScheduleConfig defines time-based rule activation.
// ResolveAction returns the action for the given archetype.
// Checks Actions map first, falls back to Action field, then defaults to "block".
func (r *RuleConfig) ResolveAction(archetype string) string {
	if r.Actions != nil {
		if a, ok := r.Actions[archetype]; ok {
			return a
		}
	}
	if r.Action != "" {
		return r.Action // fallback for legacy single-action rules
	}
	return "block" // fail closed
}

type ScheduleConfig struct {
	Cron     string `yaml:"cron" json:"cron"`
	Timezone string `yaml:"timezone,omitempty" json:"timezone,omitempty"`
	Inside   bool   `yaml:"inside" json:"inside"`
}

// ParamBlockConfig defines argument-level blocking patterns.
type ParamBlockConfig struct {
	Param   string `yaml:"param" json:"param"`
	Pattern string `yaml:"pattern" json:"pattern"`
	Reason  string `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// AlertConfig defines anomaly alerting thresholds.
type AlertConfig struct {
	Tool       string `yaml:"tool"`
	Threshold  int    `yaml:"threshold"`
	Window     string `yaml:"window"`
	Notify     string `yaml:"notify,omitempty"`
	AutoDetect bool   `yaml:"auto_detect,omitempty"`
}

// Load reads a YAML config file and returns a validated Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return Parse(data)
}

// Parse unmarshals YAML bytes into a Config and applies defaults.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&cfg)
	expandSecrets(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// expandSecrets applies os.ExpandEnv to sensitive config fields that may
// reference environment variables via ${VAR} syntax.
func expandSecrets(cfg *Config) {
	cfg.Proxy.APIKey = os.ExpandEnv(cfg.Proxy.APIKey)
	cfg.Proxy.ServiceToken = os.ExpandEnv(cfg.Proxy.ServiceToken)
	// Note: DashboardPassHash is NOT expanded — bcrypt hashes contain $ which
	// conflicts with os.ExpandEnv. Use RECALL_DASHBOARD_PASS_HASH env var instead.
	for i := range cfg.Products {
		cfg.Products[i].APIKey = os.ExpandEnv(cfg.Products[i].APIKey)
	}
	if cfg.AuthServer != nil {
		cfg.AuthServer.ApprovalSecret = os.ExpandEnv(cfg.AuthServer.ApprovalSecret)
	}
	cfg.Proxy.Approval.AdminKey = os.ExpandEnv(cfg.Proxy.Approval.AdminKey)
	cfg.Proxy.Approval.SlackSigningSecret = os.ExpandEnv(cfg.Proxy.Approval.SlackSigningSecret)
	if cfg.Postgres != nil {
		cfg.Postgres.URL = os.ExpandEnv(cfg.Postgres.URL)
	}
}

func applyDefaults(cfg *Config) {
	if cfg.Proxy.Port == 0 {
		cfg.Proxy.Port = 8090
	}
	if cfg.Proxy.OAuthCallbackPort == 0 {
		cfg.Proxy.OAuthCallbackPort = 8091
	}
	if cfg.Proxy.MaxBodySize == "" {
		cfg.Proxy.MaxBodySize = "10MB"
	}
	// RateLimit: 0 = disabled (default). Set explicitly in config to enable.
	// Audit defaults: backward compat with legacy audit_log field
	if cfg.Proxy.Audit.Storage == "" {
		if cfg.Proxy.AuditLog != "" {
			cfg.Proxy.Audit.Storage = "jsonl"
			if cfg.Proxy.Audit.Path == "" {
				cfg.Proxy.Audit.Path = cfg.Proxy.AuditLog
			}
		} else {
			cfg.Proxy.Audit.Storage = "sqlite"
		}
	}
	if cfg.Proxy.Audit.Retention == "" {
		cfg.Proxy.Audit.Retention = "30d"
	}
	if cfg.Proxy.Audit.MaxEntries == 0 {
		cfg.Proxy.Audit.MaxEntries = 100000
	}
	if cfg.Proxy.Approval.Storage == "" {
		cfg.Proxy.Approval.Storage = "memory"
	}
	if cfg.Proxy.Approval.TTL == "" {
		cfg.Proxy.Approval.TTL = "5m"
	}
	for i := range cfg.Servers {
		if cfg.Servers[i].Transport == "" {
			if cfg.Servers[i].URL != "" {
				cfg.Servers[i].Transport = "http"
			} else if cfg.Servers[i].Command != "" {
				cfg.Servers[i].Transport = "stdio"
			}
		}
	}
}

// ParseRetention parses a retention string like "30d", "24h", "60m" into a time.Duration.
func ParseRetention(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid retention %q", s)
	}
	unit := s[len(s)-1:]
	val, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return 0, fmt.Errorf("invalid retention %q: %w", s, err)
	}
	switch unit {
	case "m":
		return time.Duration(val) * time.Minute, nil
	case "h":
		return time.Duration(val) * time.Hour, nil
	case "d":
		return time.Duration(val) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid retention unit %q (use m, h, d)", unit)
	}
}

// ParseBodySize parses a body size string like "10MB", "1GB", "512KB" into bytes.
func ParseBodySize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 10 << 20, nil // default 10MB
	}
	multipliers := map[string]int64{"KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30}
	for suffix, mult := range multipliers {
		if strings.HasSuffix(s, suffix) {
			val, err := strconv.ParseInt(s[:len(s)-len(suffix)], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid body size %q: %w", s, err)
			}
			return val * mult, nil
		}
	}
	// Try plain number (bytes)
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid body size %q (use KB, MB, GB suffix)", s)
	}
	return val, nil
}

// IsMetricsEnabled returns true if the /metrics endpoint should be registered.
func (c *Config) IsMetricsEnabled() bool {
	if c.Proxy.MetricsEnabled == nil {
		return true // default: enabled
	}
	return *c.Proxy.MetricsEnabled
}

// ShouldSkipLocalRules returns true if local rules should be skipped when engine is connected.
func (c *Config) ShouldSkipLocalRules() bool {
	if c.Proxy.SkipLocalRules == nil {
		return true // default: skip local rules when engine connected
	}
	return *c.Proxy.SkipLocalRules
}

// IsConnectedMode returns true if the proxy is configured to use the Recall engine.
func (c *Config) IsConnectedMode() bool {
	return c.Proxy.EngineURL != ""
}


