package upstream

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/controlisai/recall-mcp-gateway/internal/audit"
	"github.com/controlisai/recall-mcp-gateway/internal/auth"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/metrics"
	"github.com/controlisai/recall-mcp-gateway/internal/sanitize"
)

const (
	maxBackoff              = 30 * time.Second
	initialBackoff          = 1 * time.Second
	reconnectJitterMs       = 500 // max random jitter before reconnect
	maxConcurrentReconnects = 3
	idleTimeout             = 5 * time.Minute
	idleCheckInterval       = 1 * time.Minute
	maxConnectionEvents     = 50 // ring buffer cap per server
)

// ConnectionEvent records a server connection state transition.
type ConnectionEvent struct {
	Type      string    `json:"type"`               // connected, disconnected, reconnecting, reconnected, circuit_opened, circuit_closed, idle_closed, auth_failed
	Timestamp time.Time `json:"timestamp"`
	Details   string    `json:"details,omitempty"`
}

// ServerStatus tracks the health of an upstream MCP server connection.
type ServerStatus struct {
	Name            string            `json:"name"`
	Status          string            `json:"status"` // connected, reconnecting, down, idle-closed
	CircuitState    string            `json:"circuit_state,omitempty"` // closed, open, half_open
	LastConnected   time.Time         `json:"last_connected,omitempty"`
	LastActivity    time.Time         `json:"last_activity,omitempty"`
	LastError       string            `json:"last_error,omitempty"`
	ReconnectIn     time.Duration     `json:"reconnect_in,omitempty"`
	ToolCount       int               `json:"tool_count"`
	ReconnectCount  int               `json:"reconnect_count,omitempty"`
	LastReconnectAt    time.Time         `json:"last_reconnect_at,omitempty"`
	Events             []ConnectionEvent `json:"events,omitempty"`
	ServerEra          string            `json:"server_era,omitempty"`           // modern or legacy
	lastKnownTools     []mcp.Tool        // preserved across idle close for registry
	HeaderAnnotations  int               `json:"header_annotations,omitempty"`   // count of x-mcp-header annotated tools
	McpHeaderMode      string            `json:"mcp_header_mode,omitempty"`      // effective header mode
	AuthType           string            `json:"auth_type,omitempty"`            // none, bearer, basic, headers, oauth_client_credentials, mtls
}

// Pool manages connections to multiple upstream MCP servers.
type Pool struct {
	mu       sync.RWMutex
	configs  []config.ServerConfig
	clients   map[string]*Client          // serverName -> Client
	statuses  map[string]*ServerStatus    // serverName -> status
	circuits  map[string]*CircuitBreaker  // serverName -> circuit breaker
	providers map[string]auth.Provider    // serverName -> auth provider (for refresh/reload)
	registry *Registry
	oauth    *OAuthConnector

	// Resilience: per-server reconnect mutex prevents thundering herd
	reconnectMu   sync.Map          // serverName -> *sync.Mutex
	reconnectSema chan struct{}      // global semaphore limiting concurrent reconnects

	// Lifecycle
	ctx        context.Context    // cancellable context for all background goroutines
	ctxCancel  context.CancelFunc // cancels ctx on Close
	healthStop chan struct{}
	idleStop   chan struct{}
	inFlight   sync.WaitGroup // tracks in-flight CallTool calls for graceful shutdown

	// Alerting
	onServerDown func(serverName string, downSince time.Time) // callback for persistent failure alerts
	alertedDown  map[string]bool                               // servers already alerted as down

	// Connection history
	events  map[string][]ConnectionEvent // serverName -> ring buffer (last 50 events)
	auditor audit.AuditStore             // optional: persist connection events to audit store

	// Tool change notifications — called when any upstream server's tools change
	onToolsChanged func(serverName string)

	// Registry rebuild notification — called after registry is rebuilt (for profiling, etc.)
	onRegistryRebuilt func()

	// Global MCP header mode default (per-server overrides in ServerConfig)
	globalHeaderMode string
}

// NewPool creates a new upstream connection pool.
// Pass a non-nil OAuthConnector to enable OAuth for servers that require it.
func NewPool(servers []config.ServerConfig, oauth *OAuthConnector) *Pool {
	return &Pool{
		configs:       servers,
		clients:       make(map[string]*Client),
		statuses:      make(map[string]*ServerStatus),
		circuits:      make(map[string]*CircuitBreaker),
		providers:     make(map[string]auth.Provider),
		registry:      NewRegistry(),
		oauth:         oauth,
		reconnectSema: make(chan struct{}, maxConcurrentReconnects),
		healthStop:    make(chan struct{}),
		idleStop:      make(chan struct{}),
		alertedDown:   make(map[string]bool),
		events:        make(map[string][]ConnectionEvent),
	}
}

// Connect initializes connections to all configured upstream servers.
// It connects to each server, fetches tools, and builds the merged registry.
// Servers that fail to connect are marked as down — remaining servers are still usable.
func (p *Pool) Connect(ctx context.Context) error {
	// Store a cancellable context for background goroutines (reconnect, health, idle)
	p.ctx, p.ctxCancel = context.WithCancel(context.Background())

	var wg sync.WaitGroup
	var mu sync.Mutex
	serverTools := make(map[string][]mcp.Tool)

	for _, cfg := range p.configs {
		wg.Add(1)
		go func(cfg config.ServerConfig) {
			defer wg.Done()

			status := &ServerStatus{
				Name:   cfg.Name,
				Status: "reconnecting",
			}

			client, err := NewClient(ctx, cfg, p.oauth)
			if err != nil {
				slog.Error("failed to connect to server", "component", "pool", "server", cfg.Name, "error", sanitize.Error(err.Error()))
				status.Status = "down"
				status.LastError = err.Error()

				mu.Lock()
				p.statuses[cfg.Name] = status
				p.circuits[cfg.Name] = NewCircuitBreaker(cfg.Name)
				mu.Unlock()

				p.recordEvent(cfg.Name, "disconnected", err.Error())

				// Start background reconnection
				go p.reconnectLoop(ctx, cfg)
				return
			}

			p.startClientSubscription(client)

			tools := client.Tools()
			status.Status = "connected"
			status.LastConnected = time.Now()
			status.ToolCount = len(tools)

			mu.Lock()
			p.clients[cfg.Name] = client
			p.statuses[cfg.Name] = status
			p.circuits[cfg.Name] = NewCircuitBreaker(cfg.Name)
			if client.AuthProvider != nil {
				p.providers[cfg.Name] = client.AuthProvider
			}
			serverTools[cfg.Name] = tools
			mu.Unlock()

			p.recordEvent(cfg.Name, "connected", fmt.Sprintf("%d tools", len(tools)))
			slog.Info("connected to server", "component", "pool", "server", cfg.Name, "tools", len(tools))
			metrics.ActiveConnections.Inc()
		}(cfg)
	}

	wg.Wait()

	// Build registry from all connected servers
	collisions := p.registry.Build(serverTools)
	slog.Info("tool registry built", "component", "pool", "tools", p.registry.ToolCount(), "collisions", collisions)

	// Start background health checking and idle cleanup
	go p.healthCheckLoop()
	go p.idleCheckLoop()

	return nil
}

// CallTool routes a tool call to the correct upstream server.
// Checks circuit breaker before calling. On session errors, auto-reconnects and retries once.
func (p *Pool) CallTool(ctx context.Context, toolName string, args map[string]any) (*mcp.CallToolResult, error) {
	route, ok := p.registry.Resolve(toolName)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}

	// Check circuit breaker
	p.mu.RLock()
	cb := p.circuits[route.ServerName]
	client, ok := p.clients[route.ServerName]
	p.mu.RUnlock()

	if cb != nil && !cb.AllowRequest() {
		return nil, fmt.Errorf("MCP server %s circuit is open (server unhealthy — recovering)", route.ServerName)
	}

	if !ok {
		// Check if idle-closed — lazy reconnect on demand
		p.mu.RLock()
		status, hasStatus := p.statuses[route.ServerName]
		p.mu.RUnlock()
		if hasStatus && status.Status == "idle-closed" {
			slog.Info("lazy reconnect for idle-closed server", "component", "pool", "server", route.ServerName)
			if reconnErr := p.reconnectOnError(ctx, route.ServerName); reconnErr != nil {
				return nil, fmt.Errorf("MCP server %s was idle-closed, reconnect failed: %v", route.ServerName, reconnErr)
			}
			p.mu.RLock()
			client, ok = p.clients[route.ServerName]
			p.mu.RUnlock()
			if !ok {
				return nil, fmt.Errorf("MCP server %s unavailable after reconnect", route.ServerName)
			}
		} else {
			// Server unavailable — record as circuit failure
			if cb != nil {
				cb.RecordFailure()
			}
			return nil, fmt.Errorf("MCP server %s is currently unavailable", route.ServerName)
		}
	}

	// Track in-flight for graceful shutdown + last activity for idle detection
	p.inFlight.Add(1)
	defer p.inFlight.Done()
	p.recordActivity(route.ServerName)

	result, err := client.CallTool(ctx, route.OriginalName, args)
	if err == nil {
		if cb != nil {
			cb.RecordSuccess()
		}
		return result, nil
	}

	// Auth errors: refresh/reload credentials and reconnect
	if isAuthError(err) {
		p.recordEvent(route.ServerName, "auth_failed", sanitize.Error(err.Error()))
		slog.Warn("auth error, attempting credential refresh", "component", "pool", "server", route.ServerName, "error", sanitize.Error(err.Error()))
		p.mu.RLock()
		provider := p.providers[route.ServerName]
		p.mu.RUnlock()

		refreshed := false
		if rp, ok := provider.(auth.RefreshableProvider); ok {
			rp.InvalidateToken()
			refreshed = true
		}
		if rp, ok := provider.(auth.ReloadableProvider); ok {
			if reloadErr := rp.Reload(); reloadErr == nil {
				refreshed = true
			} else {
				slog.Warn("cert reload failed", "component", "pool", "server", route.ServerName, "error", reloadErr)
			}
		}
		if refreshed {
			if reconnErr := p.reconnectOnError(ctx, route.ServerName); reconnErr == nil {
				// Retry with fresh credentials
				p.mu.RLock()
				client, ok = p.clients[route.ServerName]
				p.mu.RUnlock()
				if ok {
					slog.Info("retrying tool after auth refresh", "component", "pool", "tool", route.OriginalName, "server", route.ServerName)
					return client.CallTool(ctx, route.OriginalName, args)
				}
			}
		}
		if cb != nil {
			cb.RecordFailure()
		}
		return result, err
	}

	// Check if this is a session/connection error worth reconnecting for
	if !isSessionError(err) {
		// Application errors don't count toward circuit breaker
		return result, err
	}

	if cb != nil {
		cb.RecordFailure()
	}
	slog.Warn("session error, attempting reconnect", "component", "pool", "server", route.ServerName, "error", sanitize.Error(err.Error()))

	// Attempt singleflight reconnect
	if reconnErr := p.reconnectOnError(ctx, route.ServerName); reconnErr != nil {
		return nil, fmt.Errorf("upstream error: %v (reconnect failed: %v)", err, reconnErr)
	}

	// Retry the call on the fresh session
	p.mu.RLock()
	client, ok = p.clients[route.ServerName]
	p.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("MCP server %s unavailable after reconnect", route.ServerName)
	}

	slog.Info("retrying tool after reconnect", "component", "pool", "tool", route.OriginalName, "server", route.ServerName)
	retryResult, retryErr := client.CallTool(ctx, route.OriginalName, args)
	if retryErr == nil && cb != nil {
		cb.RecordSuccess()
	} else if retryErr != nil && cb != nil {
		cb.RecordFailure()
	}
	return retryResult, retryErr
}

// CallToolWithMRTP routes a tool call with MultiRoundTripParams for upstream elicitation passthrough.
// The MRTP params (InputResponses + RequestState) are forwarded to the upstream server.
func (p *Pool) CallToolWithMRTP(ctx context.Context, toolName string, args map[string]any, mrtp mcp.MultiRoundTripParams) (*mcp.CallToolResult, error) {
	route, ok := p.registry.Resolve(toolName)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}

	p.mu.RLock()
	cb := p.circuits[route.ServerName]
	client, ok := p.clients[route.ServerName]
	p.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("MCP server %s not connected", route.ServerName)
	}
	if cb != nil && !cb.AllowRequest() {
		return nil, fmt.Errorf("MCP server %s circuit open — server is unhealthy", route.ServerName)
	}

	p.inFlight.Add(1)
	defer p.inFlight.Done()
	p.recordActivity(route.ServerName)

	result, err := client.CallToolWithMRTP(ctx, route.OriginalName, args, mrtp)
	if err == nil {
		if cb != nil {
			cb.RecordSuccess()
		}
	} else if cb != nil {
		cb.RecordFailure()
	}
	return result, err
}

// isSessionError returns true if the error indicates a dead/stale MCP session
// that can be recovered by reconnecting.
func isSessionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	sessionPatterns := []string{
		"session terminated",
		"missing session",
		"session not found",
		"session expired",
		"connection refused",
		"connection reset",
		"broken pipe",
		"eof",
		"use of closed network connection",
		"transport error",
		"need to re-initialize",
		"i/o timeout",
	}
	for _, pattern := range sessionPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

// isAuthError returns true if the error indicates an authentication/authorization failure.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	authPatterns := []string{
		"401",
		"403",
		"unauthorized",
		"forbidden",
		"invalid credentials",
		"authentication failed",
	}
	for _, pattern := range authPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

// reconnectOnError performs a singleflight reconnect for a specific server.
// Only one goroutine reconnects per server; others wait for the result.
func (p *Pool) reconnectOnError(ctx context.Context, serverName string) error {
	// Get or create per-server mutex
	val, _ := p.reconnectMu.LoadOrStore(serverName, &sync.Mutex{})
	mu := val.(*sync.Mutex)

	mu.Lock()
	defer mu.Unlock()

	// Double-check: another goroutine may have reconnected while we waited for the lock
	p.mu.RLock()
	status, exists := p.statuses[serverName]
	client, hasClient := p.clients[serverName]
	p.mu.RUnlock()
	if !exists {
		return fmt.Errorf("server %s removed", serverName)
	}
	// If another goroutine reconnected while we waited, verify the client is fresh
	if hasClient && status.Status == "connected" && time.Since(status.LastConnected) < 5*time.Second {
		// Try a quick call to verify the new session is alive
		verifyErr := client.Ping(ctx)
		if verifyErr == nil {
			slog.Info("server already reconnected by another goroutine", "component", "pool", "server", serverName)
			return nil
		}
		// Ping failed — session is still dead, continue with reconnect
	}

	// Find config for this server
	var cfg *config.ServerConfig
	p.mu.RLock()
	for i := range p.configs {
		if p.configs[i].Name == serverName {
			c := p.configs[i]
			cfg = &c
			break
		}
	}
	p.mu.RUnlock()
	if cfg == nil {
		return fmt.Errorf("no config for server %s", serverName)
	}

	// Acquire global reconnect semaphore (limits concurrent reconnects)
	select {
	case p.reconnectSema <- struct{}{}:
		defer func() { <-p.reconnectSema }()
	case <-ctx.Done():
		return ctx.Err()
	}

	// Jitter to prevent thundering herd
	jitter := time.Duration(rand.Intn(reconnectJitterMs)) * time.Millisecond
	time.Sleep(jitter)

	// Mark as reconnecting
	p.mu.Lock()
	p.statuses[serverName].Status = "reconnecting"
	p.mu.Unlock()

	p.recordEvent(serverName, "reconnecting", "session recovery")
	slog.Info("reconnecting to server (session recovery)", "component", "pool", "server", serverName)

	// Close old client
	p.mu.Lock()
	if old, ok := p.clients[serverName]; ok {
		old.Close()
		delete(p.clients, serverName)
	}
	p.mu.Unlock()

	// Create new client
	client, err := NewClient(ctx, *cfg, p.oauth)
	if err != nil {
		p.mu.Lock()
		p.statuses[serverName].Status = "down"
		p.statuses[serverName].LastError = err.Error()
		p.mu.Unlock()

		p.recordEvent(serverName, "disconnected", err.Error())

		// Start background reconnect loop for continued recovery
		go p.reconnectLoop(p.ctx, *cfg)
		return fmt.Errorf("reconnect to %s failed: %w", serverName, err)
	}

	p.startClientSubscription(client)
	tools := client.Tools()

	p.mu.Lock()
	p.clients[serverName] = client
	p.statuses[serverName].Status = "connected"
	p.statuses[serverName].LastConnected = time.Now()
	p.statuses[serverName].LastError = ""
	p.statuses[serverName].ToolCount = len(tools)
	p.statuses[serverName].ReconnectCount++
	p.statuses[serverName].LastReconnectAt = time.Now()
	reconnectNum := p.statuses[serverName].ReconnectCount
	if client.AuthProvider != nil {
		p.providers[serverName] = client.AuthProvider
	}
	p.mu.Unlock()

	p.rebuildRegistry()
	p.recordEvent(serverName, "reconnected", fmt.Sprintf("%d tools, reconnect #%d", len(tools), reconnectNum))
	slog.Info("reconnected to server (session recovered)", "component", "pool", "server", serverName, "tools", len(tools), "reconnect_num", reconnectNum)
	return nil
}

// Tools returns the merged tool list from all connected servers.
func (p *Pool) Tools() []mcp.Tool {
	return p.registry.Tools()
}

// InvalidateServerTools marks a server's tool cache as stale, forcing re-fetch
// on the next ListTools request. Called when upstream returns -32602 (unknown tool).
func (p *Pool) InvalidateServerTools(serverName string) {
	p.mu.RLock()
	c, ok := p.clients[serverName]
	p.mu.RUnlock()
	if ok {
		c.InvalidateTools()
		slog.Info("invalidated tool cache", "component", "pool", "server", serverName)
	}
}

// RefreshStaleTools checks all connected clients and re-fetches tools from any
// upstream server whose tool list has expired (TTL stale). Returns true if any
// tools were refreshed (caller should re-register with mcp-go). This is the
// lazy re-fetch — called on ListTools requests, not on a background timer.
func (p *Pool) RefreshStaleTools(ctx context.Context) bool {
	p.mu.RLock()
	var staleServers []string
	for name, c := range p.clients {
		if !c.ToolsFresh() {
			staleServers = append(staleServers, name)
		}
	}
	p.mu.RUnlock()

	if len(staleServers) == 0 {
		return false
	}

	refreshed := false
	for _, name := range staleServers {
		p.mu.RLock()
		c, ok := p.clients[name]
		p.mu.RUnlock()
		if !ok {
			continue
		}
		if err := c.RefreshTools(ctx); err != nil {
			slog.Warn("stale tool refresh failed", "component", "pool", "server", name, "error", err)
			continue
		}
		refreshed = true
		slog.Debug("refreshed stale tools", "component", "pool", "server", name, "tools", len(c.Tools()))
	}

	if refreshed {
		p.rebuildRegistry()
	}
	return refreshed
}

// ToolCount returns the total number of registered tools.
func (p *Pool) ToolCount() int {
	return p.registry.ToolCount()
}

// Registry returns the tool registry for direct access.
func (p *Pool) Registry() *Registry {
	return p.registry
}

// Statuses returns the current status of all upstream servers.
func (p *Pool) Statuses() []ServerStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	const maxSSEEvents = 20
	result := make([]ServerStatus, 0, len(p.statuses))
	for _, s := range p.statuses {
		ss := *s
		if cb, ok := p.circuits[s.Name]; ok {
			ss.CircuitState = cb.State().String()
		}
		// Include recent events (capped for SSE payload)
		if evts, ok := p.events[s.Name]; ok && len(evts) > 0 {
			start := 0
			if len(evts) > maxSSEEvents {
				start = len(evts) - maxSSEEvents
			}
			ss.Events = make([]ConnectionEvent, len(evts)-start)
			copy(ss.Events, evts[start:])
		}
		// Auth type from config
		for _, cfg := range p.configs {
			if cfg.Name == s.Name {
				if cfg.Auth != nil && cfg.Auth.Type != "" {
					ss.AuthType = cfg.Auth.Type
				}
				break
			}
		}
		result = append(result, ss)
	}
	return result
}

// IsReady returns true if at least one upstream server is connected.
func (p *Pool) IsReady() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, s := range p.statuses {
		if s.Status == "connected" {
			return true
		}
	}
	return false
}

// AddServer dynamically adds and connects a new upstream server.
func (p *Pool) AddServer(ctx context.Context, cfg config.ServerConfig) error {
	p.mu.Lock()
	// Check if server already exists
	if _, exists := p.statuses[cfg.Name]; exists {
		p.mu.Unlock()
		return fmt.Errorf("server %s already exists", cfg.Name)
	}
	p.configs = append(p.configs, cfg)
	p.statuses[cfg.Name] = &ServerStatus{Name: cfg.Name, Status: "reconnecting"}
	p.circuits[cfg.Name] = NewCircuitBreaker(cfg.Name)
	p.mu.Unlock()

	client, err := NewClient(ctx, cfg, p.oauth)
	if err != nil {
		p.mu.Lock()
		p.statuses[cfg.Name].Status = "down"
		p.statuses[cfg.Name].LastError = err.Error()
		p.mu.Unlock()
		p.recordEvent(cfg.Name, "disconnected", err.Error())
		go p.reconnectLoop(p.ctx, cfg)
		return fmt.Errorf("connect to %s: %w", cfg.Name, err)
	}

	p.startClientSubscription(client)
	tools := client.Tools()
	p.mu.Lock()
	p.clients[cfg.Name] = client
	p.statuses[cfg.Name].Status = "connected"
	p.statuses[cfg.Name].LastConnected = time.Now()
	p.statuses[cfg.Name].ToolCount = len(tools)
	if client.AuthProvider != nil {
		p.providers[cfg.Name] = client.AuthProvider
	}
	p.mu.Unlock()

	// Rebuild registry with new server's tools
	serverTools := make(map[string][]mcp.Tool)
	p.mu.RLock()
	for name, c := range p.clients {
		serverTools[name] = c.Tools()
	}
	p.mu.RUnlock()
	p.registry.Build(serverTools)
	if p.onRegistryRebuilt != nil {
		p.onRegistryRebuilt()
	}

	p.recordEvent(cfg.Name, "connected", fmt.Sprintf("%d tools", len(tools)))
	slog.Info("added server", "component", "pool", "server", cfg.Name, "tools", len(tools))
	return nil
}

// RemoveServer disconnects and removes an upstream server.
func (p *Pool) RemoveServer(name string) error {
	p.mu.Lock()
	client, ok := p.clients[name]
	if !ok {
		// Check if it's a down server
		if _, exists := p.statuses[name]; !exists {
			p.mu.Unlock()
			return fmt.Errorf("server %s not found", name)
		}
	}
	if client != nil {
		client.Close()
	}
	delete(p.clients, name)
	delete(p.statuses, name)
	delete(p.circuits, name)
	delete(p.providers, name)
	delete(p.events, name)
	// Remove from configs
	newConfigs := make([]config.ServerConfig, 0, len(p.configs))
	for _, c := range p.configs {
		if c.Name != name {
			newConfigs = append(newConfigs, c)
		}
	}
	p.configs = newConfigs
	p.mu.Unlock()

	// Rebuild registry without removed server's tools
	serverTools := make(map[string][]mcp.Tool)
	p.mu.RLock()
	for n, c := range p.clients {
		serverTools[n] = c.Tools()
	}
	p.mu.RUnlock()
	p.registry.Build(serverTools)
	if p.onRegistryRebuilt != nil {
		p.onRegistryRebuilt()
	}

	slog.Info("removed server", "component", "pool", "server", name)
	return nil
}

// GetServerConfig returns the config for a named server.
func (p *Pool) GetServerConfig(name string) (config.ServerConfig, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, c := range p.configs {
		if c.Name == name {
			return c, true
		}
	}
	return config.ServerConfig{}, false
}

// UpdateServer updates an existing server's config (merge semantics) and reconnects.
// Only non-zero fields in newCfg overwrite the existing config. Secret fields with
// value "****" are treated as "keep existing".
func (p *Pool) UpdateServer(ctx context.Context, name string, newCfg config.ServerConfig) error {
	p.mu.Lock()

	// Find existing config index
	idx := -1
	for i := range p.configs {
		if p.configs[i].Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		p.mu.Unlock()
		return fmt.Errorf("server %s not found", name)
	}

	// Merge: only overwrite non-zero fields
	merged := p.configs[idx]
	if newCfg.URL != "" {
		merged.URL = newCfg.URL
	}
	if newCfg.Command != "" {
		merged.Command = newCfg.Command
	}
	if newCfg.Args != nil {
		merged.Args = newCfg.Args
	}
	if newCfg.Env != nil {
		merged.Env = newCfg.Env
	}
	if newCfg.Headers != nil {
		merged.Headers = newCfg.Headers
	}
	if newCfg.CostPerCall != 0 {
		merged.CostPerCall = newCfg.CostPerCall
	}
	if newCfg.Auth != nil {
		mergeAuth(&merged, newCfg.Auth)
	}

	p.configs[idx] = merged

	// Close old client
	if old, ok := p.clients[name]; ok {
		old.Close()
		delete(p.clients, name)
	}
	p.statuses[name] = &ServerStatus{Name: name, Status: "reconnecting"}
	p.mu.Unlock()

	p.recordEvent(name, "reconnecting", "config updated")

	// Create new client with merged config
	client, err := NewClient(ctx, merged, p.oauth)
	if err != nil {
		p.mu.Lock()
		p.statuses[name].Status = "down"
		p.statuses[name].LastError = err.Error()
		p.mu.Unlock()
		p.recordEvent(name, "disconnected", err.Error())
		go p.reconnectLoop(p.ctx, merged)
		return fmt.Errorf("reconnect %s with updated config: %w", name, err)
	}

	p.startClientSubscription(client)
	tools := client.Tools()
	p.mu.Lock()
	p.clients[name] = client
	p.statuses[name].Status = "connected"
	p.statuses[name].LastConnected = time.Now()
	p.statuses[name].LastError = ""
	p.statuses[name].ToolCount = len(tools)
	if client.AuthProvider != nil {
		p.providers[name] = client.AuthProvider
	}
	// Reset circuit breaker
	if cb, ok := p.circuits[name]; ok {
		cb.RecordSuccess()
	}
	p.mu.Unlock()

	p.rebuildRegistry()
	p.recordEvent(name, "connected", fmt.Sprintf("%d tools (config updated)", len(tools)))
	slog.Info("updated server config and reconnected", "component", "pool", "server", name, "tools", len(tools))
	return nil
}

// mergeAuth merges auth config, treating "****" as "keep existing".
func mergeAuth(existing *config.ServerConfig, newAuth *config.ServerAuthConfig) {
	if existing.Auth == nil {
		existing.Auth = newAuth
		return
	}
	old := existing.Auth

	// If auth type changed, replace entirely — old fields are irrelevant
	if newAuth.Type != "" && newAuth.Type != old.Type {
		existing.Auth = newAuth
		return
	}

	// Same type — merge fields, respecting placeholders
	if newAuth.Token != "" && newAuth.Token != "****" {
		old.Token = newAuth.Token
	}
	if newAuth.Username != "" && newAuth.Username != "****" {
		old.Username = newAuth.Username
	}
	if newAuth.Password != "" && newAuth.Password != "****" {
		old.Password = newAuth.Password
	}
	if newAuth.Headers != nil {
		old.Headers = newAuth.Headers
	}
	if newAuth.TokenURL != "" {
		old.TokenURL = newAuth.TokenURL
	}
	if newAuth.ClientID != "" && newAuth.ClientID != "****" {
		old.ClientID = newAuth.ClientID
	}
	if newAuth.ClientSecret != "" && newAuth.ClientSecret != "****" {
		old.ClientSecret = newAuth.ClientSecret
	}
	if newAuth.Scopes != nil {
		old.Scopes = newAuth.Scopes
	}
	if newAuth.ClientCert != "" {
		old.ClientCert = newAuth.ClientCert
	}
	if newAuth.ClientKey != "" {
		old.ClientKey = newAuth.ClientKey
	}
	if newAuth.CACert != "" {
		old.CACert = newAuth.CACert
	}
}

// Configs returns the current server configurations.
func (p *Pool) Configs() []config.ServerConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]config.ServerConfig, len(p.configs))
	copy(result, p.configs)
	return result
}

// Close shuts down all upstream connections with graceful drain.
// Stops health check and idle checker, waits for in-flight calls (up to 10s), then closes.
func (p *Pool) Close() {
	// Cancel context for all background goroutines (reconnect loops, health checks)
	if p.ctxCancel != nil {
		p.ctxCancel()
	}
	// Stop legacy stop channels
	select {
	case <-p.healthStop:
	default:
		close(p.healthStop)
	}
	select {
	case <-p.idleStop:
	default:
		close(p.idleStop)
	}

	// Wait for in-flight calls to complete (with timeout)
	done := make(chan struct{})
	go func() {
		p.inFlight.Wait()
		close(done)
	}()
	select {
	case <-done:
		slog.Info("all in-flight calls drained", "component", "pool")
	case <-time.After(10 * time.Second):
		slog.Warn("drain timeout — closing with in-flight calls", "component", "pool")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	for name, c := range p.clients {
		if err := c.Close(); err != nil {
			slog.Warn("error closing server client", "component", "pool", "server", name, "error", err)
		}
		metrics.ActiveConnections.Dec()
	}
	p.clients = make(map[string]*Client)
}

// SetAuditor sets the audit store for persisting connection events.
// Call after the auditor is created (pool may be connected before auditor exists).
func (p *Pool) SetAuditor(a audit.AuditStore) {
	p.auditor = a
}

// SetOnToolsChanged registers a callback invoked when any upstream server's
// tool list changes (via subscriptions/listen notification from modern servers).
func (p *Pool) SetOnToolsChanged(fn func(serverName string)) {
	p.onToolsChanged = fn
}

// SetOnRegistryRebuilt registers a callback invoked after the tool registry is rebuilt
// (on connect, reconnect, or tool list change).
func (p *Pool) SetOnRegistryRebuilt(fn func()) {
	p.onRegistryRebuilt = fn
}

// OverrideRegistryForTest replaces the pool's registry. Test-only.
func (p *Pool) OverrideRegistryForTest(r *Registry) {
	p.registry = r
}

// SetGlobalHeaderMode sets the default mcp_header_mode for all servers.
func (p *Pool) SetGlobalHeaderMode(mode string) {
	p.globalHeaderMode = mode
}

// GlobalHeaderMode returns the current global header mode.
func (p *Pool) GlobalHeaderMode() string { return p.globalHeaderMode }

// findServerConfig returns the config for a named server, or nil.
func (p *Pool) findServerConfig(name string) *config.ServerConfig {
	for i := range p.configs {
		if p.configs[i].Name == name {
			return &p.configs[i]
		}
	}
	return nil
}

// startClientSubscription wires the OnToolsChanged callback, sets the header mode,
// and starts the upstream tool subscription for a newly connected client.
func (p *Pool) startClientSubscription(c *Client) {
	// Resolve effective header mode for this server
	serverCfg := p.findServerConfig(c.ServerName)
	serverMode := ""
	if serverCfg != nil {
		serverMode = serverCfg.McpHeaderMode
	}
	c.McpHeaderMode = config.ResolveHeaderMode(serverMode, p.globalHeaderMode)

	// Header expectation flagging: log when server has x-mcp-header annotations
	// but is in passthrough mode (annotations won't be used for header generation)
	if len(c.HeaderMap) > 0 {
		if c.McpHeaderMode == "passthrough" {
			slog.Info("server has x-mcp-header annotations but is in passthrough mode — consider enabling generate mode",
				"component", "pool", "server", c.ServerName,
				"annotated_tools", len(c.HeaderMap),
				"mode", c.McpHeaderMode)
		} else {
			slog.Info("x-mcp-header generate mode active",
				"component", "pool", "server", c.ServerName,
				"annotated_tools", len(c.HeaderMap),
				"mode", c.McpHeaderMode)
		}
	}

	// Update server status with era and header info
	p.mu.Lock()
	if s, ok := p.statuses[c.ServerName]; ok {
		s.ServerEra = c.ServerEra
		s.HeaderAnnotations = len(c.HeaderMap)
		s.McpHeaderMode = c.McpHeaderMode
	}
	p.mu.Unlock()

	if p.onToolsChanged != nil {
		serverName := c.ServerName
		c.OnToolsChanged = func() {
			p.onToolsChanged(serverName)
		}
	}
	c.StartToolSubscription(p.ctx)
}

// recordEvent appends a connection event to the per-server ring buffer
// and optionally persists it to the audit store.
func (p *Pool) recordEvent(serverName, eventType, details string) {
	now := time.Now()

	p.mu.Lock()
	events := p.events[serverName]
	events = append(events, ConnectionEvent{
		Type:      eventType,
		Timestamp: now,
		Details:   details,
	})
	if len(events) > maxConnectionEvents {
		events = events[len(events)-maxConnectionEvents:]
	}
	p.events[serverName] = events
	p.mu.Unlock()

	// Persist to audit store
	if p.auditor != nil {
		isErr := eventType == "disconnected" || eventType == "auth_failed" || eventType == "circuit_opened"
		decision := "allow"
		if isErr {
			decision = "block"
		}
		p.auditor.Log(audit.Entry{
			Timestamp:      now.UTC(),
			RequestID:      fmt.Sprintf("conn-%s-%d", serverName, now.UnixMilli()),
			AgentID:        "system",
			Tool:           "(server-lifecycle)",
			Server:         serverName,
			Decision:       decision,
			DecisionReason: fmt.Sprintf("%s: %s", eventType, details),
			IsError:        isErr,
			ErrorMessage:   conditionalError(isErr, details),
			ErrorCategory:  classifyConnectionEvent(eventType),
			Mode:           "connection",
		})
	}
}

// classifyConnectionEvent returns an error category for connection event types.
func classifyConnectionEvent(eventType string) string {
	switch eventType {
	case "disconnected", "circuit_opened":
		return "network"
	case "auth_failed":
		return "auth_error"
	case "idle_closed":
		return "session_error"
	default:
		return ""
	}
}

// conditionalError returns details as error message only if isErr is true.
func conditionalError(isErr bool, details string) string {
	if isErr {
		return details
	}
	return ""
}

// LoadEventsFromAudit populates the in-memory ring buffers from persisted audit entries.
// Call on startup after SetAuditor to restore connection history across restarts.
func (p *Pool) LoadEventsFromAudit() {
	if p.auditor == nil {
		return
	}

	entries := p.auditor.Search(audit.SearchFilter{
		AgentID: "system",
		Tool:    "(server-lifecycle)",
		Limit:   200,
	})
	if len(entries) == 0 {
		return
	}

	// Group by server and convert to ConnectionEvents
	byServer := make(map[string][]ConnectionEvent)
	for _, e := range entries {
		// Parse event type from DecisionReason (format: "eventType: details")
		eventType, details := parseEventReason(e.DecisionReason)
		byServer[e.Server] = append(byServer[e.Server], ConnectionEvent{
			Type:      eventType,
			Timestamp: e.Timestamp,
			Details:   details,
		})
	}

	p.mu.Lock()
	for server, events := range byServer {
		// Only load for servers that still exist in config
		if _, ok := p.statuses[server]; !ok {
			continue
		}
		// Prepend historical events before any current-session events
		existing := p.events[server]
		merged := append(events, existing...)
		if len(merged) > maxConnectionEvents {
			merged = merged[len(merged)-maxConnectionEvents:]
		}
		p.events[server] = merged
	}
	p.mu.Unlock()

	slog.Info("loaded historical connection events from audit", "component", "pool", "count", len(entries))
}

// parseEventReason extracts eventType and details from "eventType: details" format.
func parseEventReason(reason string) (string, string) {
	idx := strings.Index(reason, ": ")
	if idx < 0 {
		return reason, ""
	}
	return reason[:idx], reason[idx+2:]
}

// Events returns the connection event history for a server (most recent last).
func (p *Pool) Events(serverName string) []ConnectionEvent {
	p.mu.RLock()
	defer p.mu.RUnlock()
	events := p.events[serverName]
	if events == nil {
		return nil
	}
	result := make([]ConnectionEvent, len(events))
	copy(result, events)
	return result
}

// recordActivity updates the last activity timestamp for a server.
func (p *Pool) recordActivity(serverName string) {
	p.mu.Lock()
	if s, ok := p.statuses[serverName]; ok {
		s.LastActivity = time.Now()
	}
	p.mu.Unlock()
}

// idleCheckLoop periodically closes sessions that have been idle too long.
func (p *Pool) idleCheckLoop() {
	ticker := time.NewTicker(idleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.idleStop:
			return
		case <-ticker.C:
			p.closeIdleSessions()
		}
	}
}

func (p *Pool) closeIdleSessions() {
	now := time.Now()

	p.mu.RLock()
	var idleServers []string
	for name, status := range p.statuses {
		if status.Status != "connected" {
			continue
		}
		// Skip if never had activity (just connected, no calls yet) — use LastConnected
		lastActive := status.LastActivity
		if lastActive.IsZero() {
			lastActive = status.LastConnected
		}
		if !lastActive.IsZero() && now.Sub(lastActive) > idleTimeout {
			idleServers = append(idleServers, name)
		}
	}
	p.mu.RUnlock()

	for _, name := range idleServers {
		slog.Debug("closing idle session", "component", "pool", "server", name, "idle_timeout", idleTimeout)

		p.mu.Lock()
		if client, ok := p.clients[name]; ok {
			// Preserve tools for registry before closing
			if s, ok := p.statuses[name]; ok {
				s.lastKnownTools = client.Tools()
			}
			client.Close()
			delete(p.clients, name)
		}
		if s, ok := p.statuses[name]; ok {
			s.Status = "idle-closed"
		}
		// Reset circuit so lazy reconnect starts clean
		if cb, ok := p.circuits[name]; ok {
			cb.RecordSuccess()
		}
		p.mu.Unlock()

		p.recordEvent(name, "idle_closed", fmt.Sprintf("idle >%v", idleTimeout))
	}
}

const (
	healthCheckInterval      = 30 * time.Second
	persistentFailureTimeout = 5 * time.Minute
)

// SetOnServerDown sets a callback fired when a server has been down for >5 minutes.
func (p *Pool) SetOnServerDown(fn func(serverName string, downSince time.Time)) {
	p.onServerDown = fn
}

// healthCheckLoop periodically pings all connected upstream servers.
// On failure, records circuit breaker failure and triggers reconnect.
func (p *Pool) healthCheckLoop() {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.healthStop:
			return
		case <-ticker.C:
			p.healthCheckAll()
		}
	}
}

func (p *Pool) healthCheckAll() {
	p.mu.RLock()
	clients := make(map[string]*Client, len(p.clients))
	for name, c := range p.clients {
		clients[name] = c
	}
	p.mu.RUnlock()

	var wg sync.WaitGroup
	for name, client := range clients {
		wg.Add(1)
		go func(name string, client *Client) {
			defer wg.Done()

			hctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
			err := client.Ping(hctx)
			cancel()

			p.mu.RLock()
			cb := p.circuits[name]
			p.mu.RUnlock()

			if err == nil {
				if cb != nil {
					wasOpen := cb.State() == CircuitOpen || cb.State() == CircuitHalfOpen
					cb.RecordSuccess()
					if wasOpen {
						p.recordEvent(name, "circuit_closed", "probe succeeded")
					}
				}
				return
			}

			slog.Debug("health check failed", "component", "pool", "server", name, "error", err)
			if cb != nil {
				cb.RecordFailure()
			}

			// If circuit just tripped to OPEN, trigger reconnect
			if cb != nil && cb.State() == CircuitOpen {
				p.recordEvent(name, "circuit_opened", fmt.Sprintf("health check failed: %v", err))
				slog.Warn("circuit opened, triggering reconnect", "component", "pool", "server", name)
				if reconnErr := p.reconnectOnError(p.ctx, name); reconnErr != nil {
					slog.Error("health check reconnect failed", "component", "pool", "server", name, "error", reconnErr)
				}
			}
		}(name, client)
	}
	wg.Wait()

	// Check for persistent failures — servers down >5 min
	if p.onServerDown != nil {
		p.mu.Lock()
		for name, status := range p.statuses {
			if status.Status == "down" || status.Status == "reconnecting" {
				if cb, ok := p.circuits[name]; ok && cb.State() == CircuitOpen {
					if !p.alertedDown[name] && time.Since(cb.LastStateChange()) > persistentFailureTimeout {
						p.alertedDown[name] = true
						go p.onServerDown(name, cb.LastStateChange())
					}
				}
			}
			// Clear alert flag when server recovers
			if status.Status == "connected" && p.alertedDown[name] {
				delete(p.alertedDown, name)
			}
		}
		p.mu.Unlock()
	}
}

// reconnectLoop attempts to reconnect to a failed upstream server with exponential backoff.
func (p *Pool) reconnectLoop(ctx context.Context, cfg config.ServerConfig) {
	backoff := initialBackoff

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Check if server was removed while waiting
		p.mu.RLock()
		status, exists := p.statuses[cfg.Name]
		p.mu.RUnlock()
		if !exists {
			slog.Info("server was removed, stopping reconnect loop", "component", "pool", "server", cfg.Name)
			return
		}
		// If reconnectOnError already recovered this server, stop the loop
		if status.Status == "connected" {
			slog.Info("server already recovered, stopping reconnect loop", "component", "pool", "server", cfg.Name)
			return
		}

		// Coordinate with reconnectOnError via per-server mutex
		val, _ := p.reconnectMu.LoadOrStore(cfg.Name, &sync.Mutex{})
		srvMu := val.(*sync.Mutex)
		srvMu.Lock()

		// Re-check after acquiring lock — reconnectOnError may have recovered
		p.mu.RLock()
		status, exists = p.statuses[cfg.Name]
		p.mu.RUnlock()
		if !exists || status.Status == "connected" {
			srvMu.Unlock()
			return
		}

		p.mu.Lock()
		p.statuses[cfg.Name].Status = "reconnecting"
		p.statuses[cfg.Name].ReconnectIn = backoff
		p.mu.Unlock()

		slog.Info("reconnecting to server", "component", "pool", "server", cfg.Name, "backoff", backoff)

		client, err := NewClient(ctx, cfg, p.oauth)
		if err != nil {
			slog.Error("reconnect to server failed", "component", "pool", "server", cfg.Name, "error", sanitize.Error(err.Error()))

			p.mu.Lock()
			p.statuses[cfg.Name].LastError = err.Error()
			p.mu.Unlock()

			srvMu.Unlock()
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		p.startClientSubscription(client)
		tools := client.Tools()

		p.mu.Lock()
		// Close old client if exists
		if old, ok := p.clients[cfg.Name]; ok {
			old.Close()
		}
		p.clients[cfg.Name] = client
		p.statuses[cfg.Name].Status = "connected"
		p.statuses[cfg.Name].LastConnected = time.Now()
		p.statuses[cfg.Name].LastError = ""
		p.statuses[cfg.Name].ToolCount = len(tools)
		p.statuses[cfg.Name].ReconnectIn = 0
		p.statuses[cfg.Name].ReconnectCount++
		p.statuses[cfg.Name].LastReconnectAt = time.Now()
		if client.AuthProvider != nil {
			p.providers[cfg.Name] = client.AuthProvider
		}
		p.mu.Unlock()

		srvMu.Unlock()

		// Rebuild registry with updated tools
		p.rebuildRegistry()

		p.recordEvent(cfg.Name, "reconnected", fmt.Sprintf("%d tools", len(tools)))
		slog.Info("reconnected to server", "component", "pool", "server", cfg.Name, "tools", len(tools))
		return
	}
}

// ReconnectServer disconnects and reconnects a single upstream server.
// If clearToken is true, the OAuth token is deleted first to force re-authorization.
func (p *Pool) ReconnectServer(ctx context.Context, serverName string, clearToken bool) error {
	var cfg *config.ServerConfig
	for i := range p.configs {
		if p.configs[i].Name == serverName {
			cfg = &p.configs[i]
			break
		}
	}
	if cfg == nil {
		return fmt.Errorf("unknown server: %s", serverName)
	}

	if clearToken {
		store := NewFileTokenStore(serverName)
		store.SaveToken(ctx, nil)
		slog.Info("cleared OAuth token", "component", "pool", "server", serverName)
	}

	// Close existing client
	p.mu.Lock()
	if old, ok := p.clients[serverName]; ok {
		old.Close()
		delete(p.clients, serverName)
	}
	p.statuses[serverName] = &ServerStatus{
		Name:   serverName,
		Status: "reconnecting",
	}
	p.mu.Unlock()

	p.recordEvent(serverName, "reconnecting", "manual reconnect")

	client, err := NewClient(ctx, *cfg, p.oauth)
	if err != nil {
		p.mu.Lock()
		p.statuses[serverName].Status = "down"
		p.statuses[serverName].LastError = err.Error()
		p.mu.Unlock()
		p.recordEvent(serverName, "disconnected", err.Error())
		return fmt.Errorf("reconnect %s: %w", serverName, err)
	}

	p.startClientSubscription(client)
	tools := client.Tools()
	p.mu.Lock()
	p.clients[serverName] = client
	p.statuses[serverName].Status = "connected"
	p.statuses[serverName].LastConnected = time.Now()
	p.statuses[serverName].LastError = ""
	p.statuses[serverName].ToolCount = len(tools)
	if client.AuthProvider != nil {
		p.providers[serverName] = client.AuthProvider
	}
	// Reset circuit breaker on manual reconnect
	if cb, ok := p.circuits[serverName]; ok {
		cb.RecordSuccess()
	}
	p.mu.Unlock()

	p.rebuildRegistry()
	p.recordEvent(serverName, "reconnected", fmt.Sprintf("%d tools", len(tools)))
	slog.Info("reconnected server", "component", "pool", "server", serverName, "tools", len(tools))
	return nil
}

// rebuildRegistry reconstructs the tool registry from all connected clients.
// All servers with clients are included (even circuit-open ones) — the circuit check
// in CallTool rejects calls to unhealthy servers with a clear error message.
func (p *Pool) rebuildRegistry() {
	p.mu.RLock()
	serverTools := make(map[string][]mcp.Tool)
	for name, c := range p.clients {
		serverTools[name] = c.Tools()
	}
	// Include idle-closed servers' last known tools so routing survives idle cleanup
	for name, status := range p.statuses {
		if status.Status == "idle-closed" && len(status.lastKnownTools) > 0 {
			if _, hasClient := p.clients[name]; !hasClient {
				serverTools[name] = status.lastKnownTools
			}
		}
	}
	p.mu.RUnlock()

	collisions := p.registry.Build(serverTools)
	slog.Info("registry rebuilt", "component", "pool", "tools", p.registry.ToolCount(), "collisions", collisions)

	if p.onRegistryRebuilt != nil {
		p.onRegistryRebuilt()
	}
}
