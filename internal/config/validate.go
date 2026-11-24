package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Validate checks config for required fields, valid formats, and duplicate entries.
func Validate(cfg *Config) error {
	// Validate servers (empty is allowed — servers can be added via dashboard)
	serverNames := make(map[string]bool)
	for i, s := range cfg.Servers {
		if s.Name == "" {
			return fmt.Errorf("config: servers[%d].name is required", i)
		}
		if serverNames[s.Name] {
			return fmt.Errorf("config: duplicate server name %q", s.Name)
		}
		serverNames[s.Name] = true

		switch s.Transport {
		case "http":
			if s.URL == "" {
				return fmt.Errorf("config: servers[%d] (%s): url is required for http transport", i, s.Name)
			}
			if _, err := url.ParseRequestURI(s.URL); err != nil {
				return fmt.Errorf("config: servers[%d] (%s): invalid url: %w", i, s.Name, err)
			}
		case "stdio":
			if s.Command == "" {
				return fmt.Errorf("config: servers[%d] (%s): command is required for stdio transport", i, s.Name)
			}
		case "oauth":
			if s.URL == "" {
				return fmt.Errorf("config: servers[%d] (%s): url is required for oauth transport", i, s.Name)
			}
			if _, err := url.ParseRequestURI(s.URL); err != nil {
				return fmt.Errorf("config: servers[%d] (%s): invalid url: %w", i, s.Name, err)
			}
		default:
			return fmt.Errorf("config: servers[%d] (%s): transport must be \"http\", \"stdio\", or \"oauth\", got %q", i, s.Name, s.Transport)
		}

		// Validate auth config
		if s.Auth != nil {
			validAuthTypes := map[string]bool{"none": true, "bearer": true, "basic": true, "headers": true, "oauth_client_credentials": true, "mtls": true}
			if s.Auth.Type != "" && !validAuthTypes[s.Auth.Type] {
				return fmt.Errorf("config: servers[%d] (%s): auth.type must be none/bearer/basic/headers/oauth_client_credentials/mtls, got %q", i, s.Name, s.Auth.Type)
			}
			switch s.Auth.Type {
			case "bearer":
				if s.Auth.Token == "" {
					return fmt.Errorf("config: servers[%d] (%s): auth.token is required for bearer auth", i, s.Name)
				}
			case "basic":
				if s.Auth.Username == "" {
					return fmt.Errorf("config: servers[%d] (%s): auth.username is required for basic auth", i, s.Name)
				}
			case "oauth_client_credentials":
				if s.Auth.TokenURL == "" {
					return fmt.Errorf("config: servers[%d] (%s): auth.token_url is required for oauth_client_credentials", i, s.Name)
				}
				if _, err := url.ParseRequestURI(s.Auth.TokenURL); err != nil {
					return fmt.Errorf("config: servers[%d] (%s): invalid auth.token_url: %w", i, s.Name, err)
				}
				if s.Auth.ClientID == "" {
					return fmt.Errorf("config: servers[%d] (%s): auth.client_id is required for oauth_client_credentials", i, s.Name)
				}
				if s.Auth.ClientSecret == "" {
					return fmt.Errorf("config: servers[%d] (%s): auth.client_secret is required for oauth_client_credentials", i, s.Name)
				}
			case "mtls":
				if s.Auth.ClientCert == "" {
					return fmt.Errorf("config: servers[%d] (%s): auth.client_cert is required for mtls", i, s.Name)
				}
				if s.Auth.ClientKey == "" {
					return fmt.Errorf("config: servers[%d] (%s): auth.client_key is required for mtls", i, s.Name)
				}
			}
		}
	}

	// Validate agents
	agentIDs := make(map[string]bool)
	agentTokens := make(map[string]bool)
	for i, a := range cfg.Agents {
		if a.ID == "" {
			return fmt.Errorf("config: agents[%d].id is required", i)
		}
		if agentIDs[a.ID] {
			return fmt.Errorf("config: duplicate agent id %q", a.ID)
		}
		agentIDs[a.ID] = true

		// token_hash is optional when auth_server (JWT) is configured — agents
		// authenticate via JWT claims, not static tokens
		if a.TokenHash == "" && !cfg.HasExternalAS() {
			return fmt.Errorf("config: agents[%d] (%s): token_hash is required (SHA-256 hex) unless auth_server is configured", i, a.ID)
		}
		if a.TokenHash != "" {
			if agentTokens[a.TokenHash] {
				return fmt.Errorf("config: duplicate agent token_hash for agent %q", a.ID)
			}
			agentTokens[a.TokenHash] = true
		}

		// Validate expires_at format (ISO 8601)
		if a.ExpiresAt != "" {
			if _, err := time.Parse(time.RFC3339, a.ExpiresAt); err != nil {
				return fmt.Errorf("config: agents[%d] (%s): invalid expires_at %q (use RFC3339 format)", i, a.ID, a.ExpiresAt)
			}
		}

		// Validate allowed_servers reference existing servers
		for _, sn := range a.AllowedServers {
			if !serverNames[sn] {
				return fmt.Errorf("config: agents[%d] (%s): allowed_server %q not found in servers", i, a.ID, sn)
			}
		}
	}

	// Validate products
	productNames := make(map[string]bool)
	for i, p := range cfg.Products {
		if p.Name == "" {
			return fmt.Errorf("config: products[%d].name is required", i)
		}
		if productNames[p.Name] {
			return fmt.Errorf("config: duplicate product name %q", p.Name)
		}
		productNames[p.Name] = true
		if p.APIKey == "" {
			return fmt.Errorf("config: products[%d] (%s): api_key is required", i, p.Name)
		}
	}

	// Validate auth_server
	if cfg.AuthServer != nil {
		as := cfg.AuthServer
		if as.Issuer == "" {
			return fmt.Errorf("config: auth_server.issuer is required")
		}
		if _, err := url.ParseRequestURI(as.Issuer); err != nil {
			return fmt.Errorf("config: auth_server.issuer is not a valid URL: %w", err)
		}
		if as.Audience == "" {
			return fmt.Errorf("config: auth_server.audience is required when auth_server is set")
		}
		if as.JWKSURL != "" {
			if _, err := url.ParseRequestURI(as.JWKSURL); err != nil {
				return fmt.Errorf("config: auth_server.jwks_url is not a valid URL: %w", err)
			}
		}
	}

	// Validate rules
	validActions := map[string]bool{"allow": true, "block": true, "hide": true, "require_approval": true, "require_consent": true}
	for i, r := range cfg.Rules {
		if r.Tool == "" {
			return fmt.Errorf("config: rules[%d].tool is required", i)
		}
		// Accept either single action or per-archetype actions map
		hasAction := r.Action != ""
		hasActions := len(r.Actions) > 0
		if !hasAction && !hasActions {
			return fmt.Errorf("config: rules[%d]: action or actions map is required", i)
		}
		if hasAction && !validActions[r.Action] {
			return fmt.Errorf("config: rules[%d]: action must be allow/block/hide/require_approval/require_consent, got %q", i, r.Action)
		}
		// Validate each archetype action in the map
		validArchetypes := map[string]bool{ArchetypeAutonomous: true, ArchetypeUserEmbedded: true, ArchetypeDelegated: true}
		for arch, act := range r.Actions {
			if !validArchetypes[arch] {
				return fmt.Errorf("config: rules[%d]: unknown archetype %q in actions map", i, arch)
			}
			if !validActions[act] {
				return fmt.Errorf("config: rules[%d]: invalid action %q for archetype %q", i, act, arch)
			}
		}
		// Consent form validation — check both single action and actions map
		needsConsent := r.Action == "require_consent"
		for _, act := range r.Actions {
			if act == "require_consent" {
				needsConsent = true
			}
		}
		if needsConsent && r.ConsentForm == nil {
			return fmt.Errorf("config: rules[%d]: require_consent action requires consent_form", i)
		}
		hasConsentAction := r.Action == "require_consent"
		for _, act := range r.Actions {
			if act == "require_consent" {
				hasConsentAction = true
			}
		}
		if r.ConsentForm != nil && !hasConsentAction {
			return fmt.Errorf("config: rules[%d]: consent_form is only valid with require_consent action", i)
		}
		if r.DelegationNotify != "" {
			if u, err := url.ParseRequestURI(r.DelegationNotify); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return fmt.Errorf("config: rules[%d]: delegation_notify must be a valid http/https URL", i)
			}
		}
	}

	// Validate alerts
	for i, a := range cfg.Alerts {
		if a.Tool == "" {
			return fmt.Errorf("config: alerts[%d].tool is required", i)
		}
		if a.Threshold <= 0 && !a.AutoDetect {
			return fmt.Errorf("config: alerts[%d]: threshold must be > 0 (or set auto_detect: true)", i)
		}
		if a.Window != "" {
			if !isValidWindow(a.Window) {
				return fmt.Errorf("config: alerts[%d]: invalid window %q (use 1m, 5m, 1h, etc.)", i, a.Window)
			}
		}
	}

	// Connected mode validation
	if cfg.IsConnectedMode() {
		if cfg.Proxy.APIKey == "" {
			return fmt.Errorf("config: proxy.api_key is required when engine_url is set")
		}
		if _, err := url.ParseRequestURI(cfg.Proxy.EngineURL); err != nil {
			return fmt.Errorf("config: invalid proxy.engine_url: %w", err)
		}
	}

	// Audit config validation
	validStorage := map[string]bool{"sqlite": true, "db": true, "jsonl": true, "memory": true}
	if cfg.Proxy.Audit.Storage != "" && !validStorage[cfg.Proxy.Audit.Storage] {
		return fmt.Errorf("config: proxy.audit.storage must be sqlite/jsonl/memory, got %q", cfg.Proxy.Audit.Storage)
	}
	if cfg.Proxy.Audit.Retention != "" {
		if _, err := ParseRetention(cfg.Proxy.Audit.Retention); err != nil {
			return fmt.Errorf("config: proxy.audit.retention: %w", err)
		}
	}

	// Approval config validation
	validApprovalStorage := map[string]bool{"memory": true, "sqlite": true, "db": true}
	if !validApprovalStorage[cfg.Proxy.Approval.Storage] {
		return fmt.Errorf("config: proxy.approval.storage must be memory/sqlite/db, got %q", cfg.Proxy.Approval.Storage)
	}
	if cfg.Proxy.Approval.TTL != "" {
		if _, err := ParseRetention(cfg.Proxy.Approval.TTL); err != nil {
			return fmt.Errorf("config: proxy.approval.ttl: %w", err)
		}
	}

	// Dashboard auth validation: user requires pass_hash (or defaults will be applied)
	// No strict mutual requirement — defaults handle the common cases

	// Agent product references
	for i, a := range cfg.Agents {
		if a.Product != "" && !productNames[a.Product] {
			return fmt.Errorf("config: agents[%d] (%s): product %q not found in products", i, a.ID, a.Product)
		}
	}

	return nil
}

func isValidWindow(w string) bool {
	if len(w) < 2 {
		return false
	}
	suffix := w[len(w)-1:]
	if suffix != "s" && suffix != "m" && suffix != "h" && suffix != "d" {
		return false
	}
	num := strings.TrimRight(w[:len(w)-1], "0123456789")
	return num == ""
}
