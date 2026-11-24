package upstream

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/controlisai/recall-mcp-gateway/internal/auth"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/detect"
)

// Client wraps an mcp-go Client with server metadata.
type Client struct {
	ServerName       string
	mcpClient        *client.Client
	tools            []mcp.Tool
	toolsFreshUntil  time.Time           // tools cache valid until this time; zero = always stale
	AuthProvider     auth.Provider       // stored for refresh/reload on auth errors
	ServerEra        string              // "modern" (2026-07-28+) or "legacy" (pre-2026-07-28), detected at connection time
	HeaderMap        detect.ToolHeaderMap // x-mcp-header annotations per tool (parsed from schemas)
	McpHeaderMode    string              // effective mode for this server: passthrough, generate, managed
	OnToolsChanged   func()              // callback when upstream sends notifications/tools/list_changed
	stopListen       func()              // stops the upstream subscriptions/listen stream
}

// NewClient creates and initializes an MCP client connection for the given server config.
// For OAuth servers, pass an OAuthConnector; pass nil for non-OAuth transports.
func NewClient(ctx context.Context, cfg config.ServerConfig, oauth *OAuthConnector) (*Client, error) {
	var mcpClient *client.Client
	var err error
	var provider auth.Provider = &auth.NoneProvider{}

	switch cfg.Transport {
	case "stdio":
		env := envSlice(cfg.Env)
		mcpClient, err = client.NewStdioMCPClient(cfg.Command, env, cfg.Args...)
		if err != nil {
			return nil, fmt.Errorf("stdio client for %s: %w", cfg.Name, err)
		}
	case "http":
		opts := []transport.StreamableHTTPCOption{}
		// Build auth provider from structured config or legacy headers map
		var authErr error
		provider, authErr = buildAuthProvider(cfg)
		if authErr != nil {
			return nil, fmt.Errorf("auth for %s: %w", cfg.Name, authErr)
		}
		if provider.Name() != "none" {
			slog.Info("upstream auth configured", "component", "client", "server", cfg.Name, "auth", provider.Name())
			// Transport-level auth (mTLS) uses custom http.Client
			if tp, ok := provider.(auth.TransportProvider); ok {
				opts = append(opts, transport.WithHTTPBasicClient(tp.HTTPClient()))
			} else {
				opts = append(opts, transport.WithHTTPHeaderFunc(provider.Headers))
			}
		}
		mcpClient, err = client.NewStreamableHttpClient(cfg.URL, opts...)
		if err != nil {
			// Check if this is actually an OAuth server
			if needsOAuth(err) && oauth != nil {
				slog.Info("server requires OAuth, switching to OAuth flow", "component", "client", "server", cfg.Name)
				return newOAuthClient(ctx, cfg, oauth)
			}
			return nil, fmt.Errorf("http client for %s: %w", cfg.Name, err)
		}
	case "oauth":
		if oauth == nil {
			return nil, fmt.Errorf("oauth transport for %s requires OAuthConnector", cfg.Name)
		}
		return newOAuthClient(ctx, cfg, oauth)
	default:
		return nil, fmt.Errorf("unsupported transport %q for %s", cfg.Transport, cfg.Name)
	}

	c := &Client{
		ServerName:   cfg.Name,
		mcpClient:    mcpClient,
		AuthProvider: provider,
	}

	if err := c.initialize(ctx); err != nil {
		mcpClient.Close()
		return nil, fmt.Errorf("initialize %s: %w", cfg.Name, err)
	}

	if err := c.fetchTools(ctx); err != nil {
		mcpClient.Close()
		return nil, fmt.Errorf("list tools for %s: %w", cfg.Name, err)
	}

	return c, nil
}

// Tools returns the tools discovered from this server.
func (c *Client) Tools() []mcp.Tool {
	return c.tools
}

// CallTool forwards a tool call to this upstream server.
// In generate/managed mode, adds Mcp-Param-* headers from tool argument values
// based on x-mcp-header annotations discovered in the tool schema.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	// Generate Mcp-Param headers from annotations when in generate or managed mode
	if (c.McpHeaderMode == "generate" || c.McpHeaderMode == "managed") && c.HeaderMap != nil {
		if annotations, ok := c.HeaderMap[name]; ok && len(annotations) > 0 {
			headers := make(http.Header)
			for _, ann := range annotations {
				val, exists := args[ann.ParamPath]
				if !exists {
					continue // optional param not provided — omit header
				}
				headerVal := formatHeaderValue(val, ann.ParamType)
				if headerVal != "" {
					headers.Set("Mcp-Param-"+ann.HeaderName, headerVal)
				}
			}
			if len(headers) > 0 {
				req.Header = headers
				slog.Debug("generated Mcp-Param headers", "component", "client", "server", c.ServerName, "tool", name, "headers", len(headers))
			}
		}
	}

	return c.mcpClient.CallTool(ctx, req)
}

// CallToolWithMRTP calls a tool on this upstream server, passing through
// MultiRoundTripParams (InputResponses + RequestState) from the client.
// Used for upstream elicitation passthrough — the client's retry carries
// the upstream's opaque requestState back through the gateway.
func (c *Client) CallToolWithMRTP(ctx context.Context, name string, args map[string]any, mrtp mcp.MultiRoundTripParams) (*mcp.CallToolResult, error) {
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	req.Params.MultiRoundTripParams = mrtp

	// Same header generation as CallTool
	if (c.McpHeaderMode == "generate" || c.McpHeaderMode == "managed") && c.HeaderMap != nil {
		if annotations, ok := c.HeaderMap[name]; ok && len(annotations) > 0 {
			headers := make(http.Header)
			for _, ann := range annotations {
				val, exists := args[ann.ParamPath]
				if !exists {
					continue
				}
				headerVal := formatHeaderValue(val, ann.ParamType)
				if headerVal != "" {
					headers.Set("Mcp-Param-"+ann.HeaderName, headerVal)
				}
			}
			if len(headers) > 0 {
				req.Header = headers
			}
		}
	}

	return c.mcpClient.CallTool(ctx, req)
}

// formatHeaderValue converts a tool argument value to a header string per MCP spec.
func formatHeaderValue(val any, paramType string) string {
	switch paramType {
	case "string":
		s, _ := val.(string)
		return s
	case "integer":
		switch v := val.(type) {
		case float64:
			return fmt.Sprintf("%d", int64(v))
		case int:
			return fmt.Sprintf("%d", v)
		case int64:
			return fmt.Sprintf("%d", v)
		default:
			return fmt.Sprintf("%v", v)
		}
	case "boolean":
		b, _ := val.(bool)
		if b {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}

// Close shuts down the MCP client connection and any active subscriptions.
func (c *Client) Close() error {
	if c.stopListen != nil {
		c.stopListen()
		c.stopListen = nil
	}
	return c.mcpClient.Close()
}

// StartToolSubscription opens a subscriptions/listen stream to receive
// notifications/tools/list_changed from modern upstream servers.
// For legacy servers this is a no-op — they don't support subscriptions.
func (c *Client) StartToolSubscription(ctx context.Context) {
	if c.ServerEra != "modern" {
		return
	}

	// Register notification handler for tool list changes
	c.mcpClient.OnNotification(func(n mcp.JSONRPCNotification) {
		if n.Method == string(mcp.MethodNotificationToolsListChanged) {
			slog.Info("upstream tools changed", "component", "client", "server", c.ServerName)
			if c.OnToolsChanged != nil {
				c.OnToolsChanged()
			}
		}
	})

	// Open async subscription stream for tool list changes
	stop, err := c.mcpClient.ListenAsync(ctx, mcp.SubscriptionFilter{
		ToolsListChanged: true,
	}, func(err error) {
		slog.Warn("upstream subscription error", "component", "client", "server", c.ServerName, "error", err)
	})
	if err != nil {
		slog.Warn("failed to start upstream subscription", "component", "client", "server", c.ServerName, "error", err)
		return
	}
	c.stopListen = stop
	slog.Debug("upstream tool subscription started", "component", "client", "server", c.ServerName)
}

// Ping checks if the MCP session is alive.
func (c *Client) Ping(ctx context.Context) error {
	return c.mcpClient.Ping(ctx)
}

func (c *Client) initialize(ctx context.Context) error {
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "recall-mcp-gateway",
		Version: "1.0.0",
	}
	initReq.Params.Capabilities = mcp.ClientCapabilities{}

	result, err := c.mcpClient.Initialize(ctx, initReq)
	if err != nil {
		return err
	}

	// Detect upstream era from negotiated protocol version.
	// mcp-go v1.3.0 tries server/discover first (modern), falls back to initialize (legacy).
	// The returned ProtocolVersion reflects which path succeeded.
	if mcp.IsModernProtocol(result.ProtocolVersion) {
		c.ServerEra = "modern"
	} else {
		c.ServerEra = "legacy"
	}
	slog.Debug("upstream era detected", "component", "client", "server", c.ServerName, "era", c.ServerEra, "version", result.ProtocolVersion)

	return nil
}

func (c *Client) fetchTools(ctx context.Context) error {
	result, err := c.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return err
	}
	c.tools = result.Tools

	// Compute freshness from upstream TTL hint.
	// Pre-2026 servers won't include TTLMs → treat as 0 (always stale).
	if ttl, ok := result.TTL(); ok && ttl > 0 {
		c.toolsFreshUntil = time.Now().Add(time.Duration(ttl) * time.Millisecond)
	} else {
		c.toolsFreshUntil = time.Time{} // zero = always stale
	}

	// Parse x-mcp-header annotations from tool schemas
	c.HeaderMap = detect.ParseHeaderAnnotations(result.Tools)
	if len(c.HeaderMap) > 0 {
		slog.Info("x-mcp-header annotations detected", "component", "client", "server", c.ServerName, "tools_with_annotations", len(c.HeaderMap))
	}

	return nil
}

// ToolsFresh returns true if the cached tool list is still within its TTL.
func (c *Client) ToolsFresh() bool {
	if c.toolsFreshUntil.IsZero() {
		return false // no TTL → always stale
	}
	return time.Now().Before(c.toolsFreshUntil)
}

// InvalidateTools marks the tool cache as stale, forcing a re-fetch on next access.
func (c *Client) InvalidateTools() {
	c.toolsFreshUntil = time.Time{}
}

// RefreshTools re-fetches the tool list from the upstream server.
func (c *Client) RefreshTools(ctx context.Context) error {
	return c.fetchTools(ctx)
}

func newOAuthClient(ctx context.Context, cfg config.ServerConfig, oauth *OAuthConnector) (*Client, error) {
	mcpClient, err := oauth.Connect(ctx, cfg.Name, cfg.URL)
	if err != nil {
		return nil, err
	}

	c := &Client{
		ServerName: cfg.Name,
		mcpClient:  mcpClient,
	}

	if err := c.initialize(ctx); err != nil {
		mcpClient.Close()
		// If the token was stale, clear it and force re-auth
		if needsOAuth(err) {
			slog.Warn("stored token invalid, clearing and re-authorizing", "component", "client", "server", cfg.Name)
			tokenStore := NewFileTokenStore(cfg.Name)
			tokenStore.SaveToken(ctx, nil) // clear the stale token
			// Retry — Connect will now trigger the browser flow since no token exists
			mcpClient2, err2 := oauth.Connect(ctx, cfg.Name, cfg.URL)
			if err2 != nil {
				return nil, fmt.Errorf("oauth re-auth for %s: %w", cfg.Name, err2)
			}
			c.mcpClient = mcpClient2
			if err3 := c.initialize(ctx); err3 != nil {
				mcpClient2.Close()
				return nil, fmt.Errorf("initialize %s after re-auth: %w", cfg.Name, err3)
			}
		} else {
			return nil, fmt.Errorf("initialize %s: %w", cfg.Name, err)
		}
	}

	if err := c.fetchTools(ctx); err != nil {
		c.mcpClient.Close()
		return nil, fmt.Errorf("list tools for %s: %w", cfg.Name, err)
	}

	return c, nil
}

// buildAuthProvider creates an auth provider from server config.
// Priority: structured Auth config > legacy Headers map > none.
func buildAuthProvider(cfg config.ServerConfig) (auth.Provider, error) {
	if cfg.Auth != nil {
		if len(cfg.Headers) > 0 {
			slog.Warn("both auth and headers configured; using auth (headers ignored)", "component", "client", "server", cfg.Name)
		}
		return auth.NewProvider(&auth.Config{
			Type:         cfg.Auth.Type,
			Token:        cfg.Auth.Token,
			Username:     cfg.Auth.Username,
			Password:     cfg.Auth.Password,
			Headers:      cfg.Auth.Headers,
			TokenURL:     cfg.Auth.TokenURL,
			ClientID:     cfg.Auth.ClientID,
			ClientSecret: cfg.Auth.ClientSecret,
			Scopes:       cfg.Auth.Scopes,
			ClientCert:   cfg.Auth.ClientCert,
			ClientKey:    cfg.Auth.ClientKey,
			CACert:       cfg.Auth.CACert,
		})
	}
	// Backward compat: legacy headers map → HeadersProvider with env var expansion
	if len(cfg.Headers) > 0 {
		expanded := make(map[string]string, len(cfg.Headers))
		for k, v := range cfg.Headers {
			expanded[k] = auth.ExpandEnv(v)
		}
		return auth.NewProvider(&auth.Config{Type: "headers", Headers: expanded})
	}
	return &auth.NoneProvider{}, nil
}

func envSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}
