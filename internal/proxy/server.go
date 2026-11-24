package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/controlisai/recall-mcp-gateway/internal/alerts"
	"github.com/controlisai/recall-mcp-gateway/internal/approval"
	"github.com/controlisai/recall-mcp-gateway/internal/audit"
	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/detect"
	"github.com/controlisai/recall-mcp-gateway/internal/engine"
	"github.com/controlisai/recall-mcp-gateway/internal/metrics"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
	"github.com/controlisai/recall-mcp-gateway/internal/tracing"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

// Server is the agent-facing MCP proxy server.
type Server struct {
	pool          *upstream.Pool
	auth          *AgentAuthenticator
	auditor       audit.AuditStore
	mcpServer     *server.MCPServer
	httpServer    *server.StreamableHTTPServer
	stateEnc      *approval.StateEncryptor
	serverConfigs []config.ServerConfig
	ruleEngine    *rules.Engine
	approvals     approval.StoreInterface
	approvalNotif  *approval.WebhookNotifier
	baseURL        string
	approvalSigner *approval.SignedURLGenerator
	webhookWg      sync.WaitGroup
	bridge        *engine.Bridge
	orgResolver   *OrgResolver
	agentConfigs  []config.AgentConfig
	agentMu       sync.RWMutex
	productConfigs []config.ProductConfig
	defaultOrgID  string
	toolFilter    atomic.Pointer[ToolFilter]
	history       *HistoryBuffer
	profiler      *detect.Profiler
	content       *detect.ContentDetector
	sequence      *detect.SequenceDetector
	workflow      *detect.WorkflowDetector
	dataflow      *detect.DataFlowTracker
	errorAnalyzer *detect.ErrorAnalyzer
	unusedTools   *detect.UnusedToolTracker
	tracker       *baseline.Tracker
	latency       *baseline.LatencyTracker
	alertDetector *alerts.Detector
	alertWebhook  *alerts.WebhookSender
	skipLocalRules bool   // skip proxy rules when engine bridge is connected
	clientTTLMs    int64  // TTL in ms for ListTools response to clients
	sessionClients    sync.Map // sessionID -> client name (from MCP initialize)
	sessionUsers      sync.Map // sessionID -> JWT sub (user identity for legacy clients)
	onAgentDiscovered func(agentID string) // callback when a new agent is auto-detected
}

// ServerConfig holds all dependencies for creating a proxy server.
type ServerConfig struct {
	Pool           *upstream.Pool
	Auth           *AgentAuthenticator
	Auditor        audit.AuditStore
	Servers        []config.ServerConfig
	Rules          []config.RuleConfig
	RuleEngine     *rules.Engine // shared engine; if nil, creates from Rules
	Agents         []config.AgentConfig
	Products       []config.ProductConfig
	ApprovalStore  approval.StoreInterface
	ApprovalNotif  *approval.WebhookNotifier
	BaseURL        string // e.g. "http://localhost:8090" for approval callback links
	ApprovalSecret string // for requestState AEAD encryption (empty = ephemeral key)
	EngineBridge   *engine.Bridge
	DefaultOrgID   string
	Tracker        *baseline.Tracker
	Latency        *baseline.LatencyTracker
	AlertDetector  *alerts.Detector
	AlertWebhook   *alerts.WebhookSender
	SkipLocalRules bool   // skip proxy rules when engine connected (default true)
	ClientTTLMs    int64  // TTL in ms for ListTools response to clients (default 30000)
}

// NewServer creates the MCP proxy server.
func NewServer(cfg ServerConfig) *Server {
	profiler := detect.NewProfiler()

	re := cfg.RuleEngine
	if re == nil {
		re = rules.NewEngine(cfg.Rules)
	}

	s := &Server{
		pool:           cfg.Pool,
		auth:           cfg.Auth,
		auditor:        cfg.Auditor,
		serverConfigs:  cfg.Servers,
		ruleEngine:     re,
		approvals:      cfg.ApprovalStore,
		approvalNotif:  cfg.ApprovalNotif,
		baseURL:        cfg.BaseURL,
		bridge:         cfg.EngineBridge,
		orgResolver:    NewOrgResolver(cfg.Agents, cfg.DefaultOrgID),
		agentConfigs:   cfg.Agents,
		productConfigs: cfg.Products,
		defaultOrgID:   cfg.DefaultOrgID,
		// toolFilter set below via atomic.Pointer
		history:        NewHistoryBuffer(50),
		profiler:       profiler,
		content:        detect.NewContentDetector(),
		sequence:       detect.NewSequenceDetector(profiler),
		workflow:       detect.NewWorkflowDetector(),
		dataflow:       detect.NewDataFlowTracker(),
		errorAnalyzer:  detect.NewErrorAnalyzer(),
		unusedTools:    detect.NewUnusedToolTracker(),
		tracker:        cfg.Tracker,
		latency:        cfg.Latency,
		alertDetector:  cfg.AlertDetector,
		alertWebhook:   cfg.AlertWebhook,
		skipLocalRules: cfg.SkipLocalRules,
		clientTTLMs:    cfg.ClientTTLMs,
	}
	if s.clientTTLMs == 0 {
		s.clientTTLMs = 30000 // default 30s
	}
	if stateEnc, err := approval.NewStateEncryptor(cfg.ApprovalSecret); err == nil {
		s.stateEnc = stateEnc
	} else {
		slog.Warn("failed to create state encryptor", "component", "proxy", "error", err)
	}
	if cfg.ApprovalSecret != "" && cfg.BaseURL != "" {
		s.approvalSigner = approval.NewSignedURLGenerator(cfg.ApprovalSecret, cfg.BaseURL)
	}
	s.toolFilter.Store(NewToolFilter(cfg.Agents))

	// Profile all upstream tools
	for _, status := range cfg.Pool.Statuses() {
		serverTools := cfg.Pool.Registry().ServerTools(status.Name)
		profiler.ProfileTools(status.Name, serverTools)
	}

	hooks := &server.Hooks{}
	hooks.AddAfterCallTool(s.afterCallTool)
	hooks.AddAfterInitialize(func(ctx context.Context, id any, msg *mcp.InitializeRequest, result *mcp.InitializeResult) {
		clientName := msg.Params.ClientInfo.Name
		if clientName == "" {
			return
		}
		if session := server.ClientSessionFromContext(ctx); session != nil {
			s.sessionClients.Store(session.SessionID(), clientName)
			// Store JWT user identity alongside session for audit
			if claims := JWTClaimsFromContext(ctx); claims != nil && claims.Subject != "" {
				s.sessionUsers.Store(session.SessionID(), claims.Subject)
			}
			slog.Debug("session identified", "component", "proxy", "session_id", session.SessionID(), "agent", clientName)
		}
		// Map the OAuth token to the discovered agent name for future requests
		if token := tokenFromContext(ctx); token != "" && s.auth.oauthCheck != nil {
			s.auth.oauthCheck.SetTokenAgent(token, clientName)
			slog.Debug("oauth token mapped to agent", "component", "proxy", "agent", clientName)
		}
		// Auto-register agent if not already configured
		if s.onAgentDiscovered != nil {
			s.onAgentDiscovered(clientName)
		}
	})
	// Phase 1.5: Refresh stale upstream tool caches before building the list.
	// Lazy re-fetch — only when a client asks for tools, not on a timer.
	hooks.AddBeforeListTools(func(ctx context.Context, id any, msg *mcp.ListToolsRequest) {
		if s.pool.RefreshStaleTools(ctx) {
			s.RefreshTools()
		}
	})

	// Phase 2: Filter tools/list by agent's allowed servers + hide rules
	hooks.AddAfterListTools(func(ctx context.Context, id any, msg *mcp.ListToolsRequest, result *mcp.ListToolsResult) {
		// Hide all tools from unauthorized requests and log the rejection
		if IsUnauthorized(ctx) {
			result.Tools = []mcp.Tool{}
			rid := RequestIDFromContext(ctx)
			if rid == "" {
				rid = generateRequestID()
			}
			s.auditor.Log(audit.Entry{
				Timestamp:      time.Now().UTC(),
				RequestID:      rid,
				AgentID:        "unauthorized",
				Tool:           "(tools/list)",
				Decision:       "block",
				Mode:           "auth",
				DecisionReason: "invalid or missing agent token",
			})
			return
		}
		// Unified agent resolution: Bearer token → _meta.clientInfo → session → anonymous
		resolution := resolveAgent(ctx, &s.sessionClients)
		agentID := resolution.AgentID

		// Filter by allowed_servers, blockedTools, hiddenTools, and hide rules
		tf := s.toolFilter.Load()
		var filtered []mcp.Tool
		for _, tool := range result.Tools {
			// Check server access
			if allowed, hasRestriction := tf.agentServers[agentID]; hasRestriction {
				route, ok := s.pool.Registry().Resolve(tool.Name)
				if ok && !allowed[route.ServerName] {
					continue // server not allowed for this agent
				}
			}
			// Check per-agent blockedTools (not visible, not callable)
			if tf.IsBlocked(agentID, tool.Name) {
				continue
			}
			// Check per-agent hiddenTools (not visible, but still callable)
			if hidden, ok := tf.hiddenTools[agentID]; ok && hidden[tool.Name] {
				continue
			}
			// Check hide rules (supports tool glob, agent scope, classification, server)
			hidden := false
			for _, rule := range s.ruleEngine.Rules() {
				if rule.Action != "hide" {
					continue
				}
				if rule.Tool != "" && !rules.MatchToolGlob(rule.Tool, tool.Name) {
					continue
				}
				if len(rule.Agents) > 0 && !containsStr(rule.Agents, agentID) {
					continue
				}
				// Classification-based hide: match tool's auto-classified risk type
				if rule.Classification != "" {
					profile := s.profiler.GetProfile(tool.Name)
					if profile == nil || !strings.EqualFold(profile.Classification, rule.Classification) {
						continue
					}
				}
				// Server-based hide
				if rule.Server != "" {
					route, ok := s.pool.Registry().Resolve(tool.Name)
					if !ok || route.ServerName != rule.Server {
						continue
					}
				}
				hidden = true
				break
			}
			if !hidden {
				filtered = append(filtered, tool)
			}
		}
		result.Tools = filtered

		// Set cache hints: private scope (per-agent filtering), configurable TTL
		result.SetCacheHints(s.clientTTLMs, mcp.CacheScopePrivate)
	})

	s.mcpServer = server.NewMCPServer(
		"recall-mcp-gateway",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithTaskCapabilities(false, true, true), // list=false, cancel=true, toolCallTasks=true
		server.WithInstructions("Recall MCP Gateway — governance proxy for MCP tool calls. Provides rule-based access control, behavioral baselines, anomaly detection, approval workflows, and compliance scoring for upstream MCP tool servers."),
		server.WithHooks(hooks),
	)

	// Middleware: tag context with isTaskCall when running as a task-augmented tool call.
	// Also detach from HTTP request context so the task goroutine doesn't get cancelled
	// when the HTTP response (CreateTaskResult) is sent back to the client.
	s.mcpServer.Use(func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if req.Params.Task != nil {
				ctx = detachContext(ctx)
				ctx = context.WithValue(ctx, taskCallKey, true)
			}
			return next(ctx, req)
		}
	})

	s.registerTools()

	s.httpServer = server.NewStreamableHTTPServer(
		s.mcpServer,
		server.WithEndpointPath("/mcp"),
		server.WithHTTPContextFunc(s.httpContextFunc),
	)

	return s
}

// Handler returns the HTTP handler for the MCP endpoint.
func (s *Server) Handler() http.Handler { return s.httpServer }

// DrainWebhooks waits for all in-flight webhook notification goroutines to complete.
// Call during graceful shutdown to avoid losing pending notifications.
func (s *Server) DrainWebhooks(timeout time.Duration) {
	done := make(chan struct{})
	go func() { s.webhookWg.Wait(); close(done) }()
	select {
	case <-done:
		slog.Info("all webhook notifications drained", "component", "proxy")
	case <-time.After(timeout):
		slog.Warn("webhook drain timeout — some notifications may be lost", "component", "proxy", "timeout", timeout)
	}
}

// goNotify runs a webhook notification in a tracked goroutine.
func (s *Server) goNotify(notifier *approval.WebhookNotifier, entry *approval.Entry) {
	s.webhookWg.Add(1)
	go func() {
		defer s.webhookWg.Done()
		notifier.Notify(entry)
	}()
}

// Profiler returns the tool profiler.
func (s *Server) Profiler() *detect.Profiler { return s.profiler }

// WorkflowDetector returns the workflow detector.
func (s *Server) WorkflowDetector() *detect.WorkflowDetector { return s.workflow }

// SequenceDetector returns the sequence detector.
func (s *Server) SequenceDetector() *detect.SequenceDetector { return s.sequence }

// History returns the per-agent call history buffer.
func (s *Server) History() *HistoryBuffer { return s.history }

// DataFlow returns the data flow tracker.
func (s *Server) DataFlow() *detect.DataFlowTracker { return s.dataflow }

// ErrorAnalyzer returns the error analyzer.
func (s *Server) ErrorAnalyzer() *detect.ErrorAnalyzer { return s.errorAnalyzer }


// UnusedTools returns the unused tool tracker.
func (s *Server) UnusedTools() *detect.UnusedToolTracker { return s.unusedTools }

// SetOnAgentDiscovered sets the callback invoked when a new agent is auto-detected from MCP initialize.
func (s *Server) SetOnAgentDiscovered(fn func(agentID string)) {
	s.onAgentDiscovered = fn
}

// UpdateToolFilter atomically replaces the tool filter with one built from new agent configs.
func (s *Server) UpdateToolFilter(agents []config.AgentConfig) {
	s.toolFilter.Store(NewToolFilter(agents))
	s.agentMu.Lock()
	s.agentConfigs = agents
	s.agentMu.Unlock()
}

// SetEngineBridge enables or replaces the engine bridge for two-layer evaluation at runtime.
func (s *Server) SetEngineBridge(bridge *engine.Bridge) {
	s.bridge = bridge
	slog.Info("engine bridge enabled — two-layer evaluation active", "component", "proxy")
}

func (s *Server) registerTools() {
	// Register built-in proxy tools
	s.mcpServer.AddTool(listServersTool(), s.listServersHandler)

	// Register upstream tools with TaskSupportOptional (allows async task-based approval)
	tools := s.pool.Tools()
	for _, tool := range tools {
		t := tool
		if t.Execution == nil {
			t.Execution = &mcp.ToolExecution{}
		}
		t.Execution.TaskSupport = mcp.TaskSupportOptional
		s.mcpServer.AddTool(t, s.makeToolHandler(t.Name))
		s.unusedTools.RegisterTool(t.Name)
	}
	slog.Info("tools registered", "component", "proxy", "total", len(tools)+1, "upstream", len(tools))
}

// RefreshTools re-registers tools after an upstream reconnection.
func (s *Server) RefreshTools() {
	for _, t := range s.pool.Tools() {
		s.mcpServer.DeleteTools(t.Name)
	}
	s.registerTools()
}

// makeToolHandler creates a handler that forwards calls to the upstream pool.
func (s *Server) makeToolHandler(exposedName string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		isTaskCall, _ := ctx.Value(taskCallKey).(bool)
		// Reject unauthenticated requests when agents are configured
		if IsUnauthorized(ctx) {
			rid := RequestIDFromContext(ctx)
			if rid == "" {
				rid = generateRequestID()
			}
			s.auditor.Log(audit.Entry{
				Timestamp:      time.Now().UTC(),
				RequestID:      rid,
				AgentID:        "unauthorized",
				Tool:           exposedName,
				Decision:       "block",
				Mode:           "auth",
				DecisionReason: "invalid or missing agent token",
			})
			return governanceError(ErrCodeUnauthorized, rid, "invalid or missing agent token"), nil
		}

		// Unified agent resolution: Bearer token → _meta.clientInfo → session → anonymous
		resolution := resolveAgent(ctx, &s.sessionClients)
		agentID := resolution.AgentID

		// Anonymous mode enforcement
		// Auto-register agent discovered from modern _meta.clientInfo
		if resolution.IdentitySource == IdentityMetaClientInfo && s.onAgentDiscovered != nil {
			s.onAgentDiscovered(agentID)
		}

		requestID := RequestIDFromContext(ctx)
		if requestID == "" {
			requestID = generateRequestID()
		}
		start := time.Now()

		// OTel tracing: extract trace context from _meta and create a child span.
		// If no trace context in _meta, creates a root span (if tracing is enabled).
		if req.Params.Meta != nil {
			if tp, _ := req.Params.Meta.GetMetaField("traceparent").(string); tp != "" {
				ts, _ := req.Params.Meta.GetMetaField("tracestate").(string)
				ctx = tracing.ExtractTraceParent(ctx, tp, ts)
			}
		}
		ctx, span := tracing.Tracer.Start(ctx, "mcp.proxy.tools/call",
			trace.WithAttributes(
				attribute.String("mcp.tool", exposedName),
				attribute.String("mcp.agent_id", agentID),
				attribute.String("mcp.identity_source", resolution.IdentitySource),
				attribute.String("mcp.protocol_era", resolution.ProtocolEra),
			),
		)
		defer span.End()

		// Resolve the tool
		route, ok := s.pool.Registry().Resolve(exposedName)
		if !ok {
			s.auditor.Log(audit.Entry{
				Timestamp: time.Now().UTC(), RequestID: requestID,
				AgentID: agentID, Tool: exposedName,
				Decision: "block", Mode: "unknown_tool",
				IdentitySource: resolution.IdentitySource, ProtocolEra: resolution.ProtocolEra,
			})
			return governanceError(ErrCodeToolNotFound, requestID, "unknown tool: %s", exposedName), nil
		}

		// Per-agent allowed_servers check
		tf := s.toolFilter.Load()
		if allowed, hasRestriction := tf.agentServers[agentID]; hasRestriction {
			if !allowed[route.ServerName] {
				s.auditor.Log(audit.Entry{
					Timestamp: time.Now().UTC(), RequestID: requestID,
					AgentID: agentID, Tool: exposedName, Server: route.ServerName,
					Decision: "block", Mode: "server_restriction",
					IdentitySource: resolution.IdentitySource, ProtocolEra: resolution.ProtocolEra,
					DecisionReason: fmt.Sprintf("Server %s not in allowed servers for agent %s", route.ServerName, agentID),
				})
				return governanceError(ErrCodeAccessDenied, requestID, "tool %s not in allowed servers for agent %s", exposedName, agentID), nil
			}
		}

		// Per-agent blockedTools check — tool is blocked for this agent (not visible, not callable)
		if tf.IsBlocked(agentID, exposedName) {
			s.auditor.Log(audit.Entry{
				Timestamp: time.Now().UTC(), RequestID: requestID,
				AgentID: agentID, Tool: exposedName, Server: route.ServerName,
				Decision: "block", Mode: "tool_blocked",
				IdentitySource: resolution.IdentitySource, ProtocolEra: resolution.ProtocolEra,
				DecisionReason: fmt.Sprintf("Tool %s is blocked for agent %s", exposedName, agentID),
			})
			return governanceError(ErrCodeAccessDenied, requestID, "tool %s not in allowed servers for agent %s", exposedName, agentID), nil
		}
		// Note: hiddenTools are NOT blocked here — they execute normally when called directly

		args := req.GetArguments()
		// Fallback: mcp-go v1.3.0 corrupts arguments on MRTP retries (consent/elicitation).
		// Use raw args extracted from HTTP body before mcp-go parsing.
		if args == nil {
			if rawArgs := RawArgsFromContext(ctx); rawArgs != nil {
				args = rawArgs
			}
		}
		// Second fallback: retrieve from global cache populated by HTTP handler wrapper
		// in cmd/gateway/main.go (runs before mcp-go, so args are intact)
		if args == nil {
			if cached, ok := rawArgsCache.Load("rawargs:" + exposedName); ok {
				args = cached.(map[string]any)
			}
		}

		// Consent retry: if the client sent a valid requestState from a prior require_consent,
		// the user has already consented — skip rule evaluation and proceed to upstream.
		consentGranted := false
		if req.Params.RequestState != "" && s.stateEnc != nil {
			state, err := s.stateEnc.Decrypt(req.Params.RequestState)
			if err == nil && state.AgentID == agentID && state.Tool == exposedName {
				// ArgsHash check: verify if args are available. mcp-go v1.3.0 has a bug where
				// arguments are lost on MRTP retries (parsed as key array instead of map).
				// When args are nil, trust the encrypted state — it was created with the
				// correct hash and the AEAD encryption is tamper-proof.
				argsMatch := args == nil || state.ArgsHash == approval.HashArgs(agentID, exposedName, args)
				if argsMatch {
					// Check elicitation response
					resp := server.ElicitationResponse(req.Params.InputResponses, "consent")
					if resp != nil && resp.Action == mcp.ElicitationResponseActionAccept {
						consentGranted = true
						slog.Info("consent granted — proceeding", "component", "proxy", "tool", exposedName, "agent", agentID)
					}
				}
			}
		}

		// Content detection
		var signals []detect.Signal
		contentSignals := s.content.Scan(args)
		signals = append(signals, contentSignals...)

		// When consent was already granted on a prior elicitation round-trip, skip all governance
		// (rules, approval, consent, engine bridge) and proceed directly to upstream execution.
		var decision rules.Decision
		if consentGranted {
			decision = rules.Decision{Action: "allow", Rule: -1, Reason: "consent granted"}
		}

		// Look up agent archetype from config (default: autonomous)
		archetype := config.ArchetypeAutonomous
		s.agentMu.RLock()
		for _, ac := range s.agentConfigs {
			if ac.ID == agentID && ac.Archetype != "" {
				archetype = ac.Archetype
				break
			}
		}
		s.agentMu.RUnlock()

		// Rule evaluation — skipped when consent already granted
		evalCtx := rules.EvalContext{ServerName: route.ServerName, Archetype: archetype}
		if !consentGranted {
		if profile := s.profiler.GetProfile(exposedName); profile != nil {
			evalCtx.Classification = profile.Classification
		}
		if s.skipLocalRules && s.bridge != nil && s.bridge.IsConfigured() {
			// Platform is the single governance authority — skip local rules
			decision = rules.Decision{Action: "allow", Rule: -1}
		} else {
			decision = s.ruleEngine.EvaluateWithContext(agentID, exposedName, evalCtx, args)
			if decision.Action != "allow" {
				slog.Warn("tool call blocked", "component", "proxy", "tool", exposedName, "agent", agentID, "decision", decision.Action, "rule", decision.Rule, "reason", decision.Reason)
			} else if s.ruleEngine.RuleCount() > 0 {
				slog.Debug("tool call allowed (no match)", "component", "proxy", "tool", exposedName, "agent", agentID, "server", evalCtx.ServerName, "classification", evalCtx.Classification, "rules", s.ruleEngine.RuleCount())
			}
		}
		} // end !consentGranted

		orgID := OrgFromContext(ctx)

		// Extract user identity and delegation context from JWT claims
		var userID, actingFor string
		var delegationDepth int
		if claims := JWTClaimsFromContext(ctx); claims != nil {
			if claims.Subject != "" {
				userID = claims.Subject
			}
			if claims.Actor != nil {
				actingFor = claims.Actor.Subject
				delegationDepth = claims.DelegationDepth()
			}
		} else if session := server.ClientSessionFromContext(ctx); session != nil {
			if sub, ok := s.sessionUsers.Load(session.SessionID()); ok {
				userID = sub.(string)
			}
		}

		entry := audit.Entry{
			Timestamp:       time.Now().UTC(),
			RequestID:       requestID,
			AgentID:         agentID,
			UserID:          userID,
			ActingFor:       actingFor,
			DelegationDepth: delegationDepth,
			OrgID:           orgID,
			Tool:           exposedName,
			Server:         route.ServerName,
			Decision:       decision.Action,
			Arguments:      args,
			Mode:           "passthrough",
			IdentitySource: resolution.IdentitySource,
			ProtocolEra:    resolution.ProtocolEra,
			Archetype:      archetype,
			TraceID:        tracing.TraceIDFromContext(ctx),
		}

		// Span event for governance decision
		if decision.Action != "allow" {
			span.AddEvent("governance_decision", trace.WithAttributes(
				attribute.String("decision", decision.Action),
				attribute.String("reason", decision.Reason),
				attribute.Int("rule_index", decision.Rule),
			))
		}

		for _, sc := range s.serverConfigs {
			if sc.Name == route.ServerName && sc.CostPerCall > 0 {
				entry.CostPerCall = sc.CostPerCall
				break
			}
		}

		// Populate matched rule info
		entry.MatchedRuleIndex = decision.Rule
		if decision.Rule >= 0 {
			entry.DecisionReason = decision.Reason
			entry.MatchedRuleAction = decision.Action
			rules := s.ruleEngine.Rules()
			if decision.Rule < len(rules) {
				entry.MatchedRule = rules[decision.Rule].Tool
			}
		}

		// Block / rate_limited
		if decision.Action == "block" || decision.Action == "rate_limited" {
			entry.LatencyMs = time.Since(start).Milliseconds()
			entry.Mode = "rule"
			entry.Signals = toSignalEntries(signals)
			blockedEntry := detect.HistoryEntry{
				Tool: exposedName, Server: route.ServerName,
				Arguments: args, Timestamp: time.Now(), Signals: signals,
			}
			s.sequence.Record(agentID, blockedEntry)
			s.history.Add(agentID, blockedEntry)
			s.unusedTools.RecordCall(exposedName) // blocked calls still count as "used"
			s.recordBaseline(agentID, exposedName, route.ServerName, false, len(signals), args, 0)
			s.runAnalysis(agentID)
			// Per-rule webhook notification
			if decision.Notify != "" && s.alertWebhook != nil {
				s.webhookWg.Add(1)
				go func() {
					defer s.webhookWg.Done()
					s.alertWebhook.Send(alerts.Alert{
						Type: "rule_" + decision.Action, Severity: "warning",
						AgentID: agentID, Tool: exposedName,
						Message: fmt.Sprintf("Rule %s: %s", decision.Action, decision.Reason),
					}, nil)
				}()
			}
			s.auditor.Log(entry)
			metrics.ToolCallsTotal.WithLabelValues(exposedName, agentID, decision.Action, route.ServerName).Inc()
			return governanceError(ErrCodeBlocked, requestID, "%s: %s", decision.Action, decision.Reason), nil
		}

		// Scope step-up check: if the rule requires scopes the JWT doesn't have
		if len(decision.RequiredScopes) > 0 {
			claims := JWTClaimsFromContext(ctx)
			if claims != nil && !hasRequiredScopes(claims.Scopes, decision.RequiredScopes) {
				entry.Decision = "scope_insufficient"
				entry.Mode = "rule"
				entry.DecisionReason = fmt.Sprintf("requires scopes: %v", decision.RequiredScopes)
				s.auditor.Log(entry)
				// Return scope step-up hint in _meta
				result := governanceError(ErrCodeAccessDenied, requestID,
					"scope step-up required: have %v, need %v", claims.Scopes, decision.RequiredScopes)
				result.Meta.SetMetaField("recall:requiredScopes", decision.RequiredScopes)
				return result, nil
			}
		}

		// Approval flow
		if decision.Action == "require_approval" && s.approvals != nil {
			if s.approvals.CheckApproved(agentID, exposedName, args) {
				// Previously approved — consume and execute
				entry.Decision = "approved"
				entry.Mode = "approval"
			} else {
				// Check if there's already a pending/denied/expired approval for this call
				existing := s.approvals.CheckPendingOrDenied(agentID, exposedName, args)
				if existing != nil {
					switch existing.Status {
					case approval.StatusPending:
						// Retry while still pending — return pending message again
						entry.Decision = "pending_approval"
						entry.Mode = "approval"
						s.auditor.Log(entry)
						return s.pendingApprovalResponse(existing, decision), nil
					case approval.StatusDenied:
						entry.Decision = "denied"
						entry.Mode = "approval"
						s.auditor.Log(entry)
						reason := existing.Reason
						if reason == "" {
							reason = "Reviewer denied the request"
						}
						return governanceError(ErrCodeDenied, requestID, "denied: %s", reason), nil
					case approval.StatusExpired:
						// Expired — allow creating a new approval below
					}
				}

				// Create new pending approval
				pending := s.approvals.Create(agentID, exposedName, args)
				pending.RetryMethod = decision.RetryMethod
				pending.StatusWebhook = decision.StatusWebhook
				pending.McpMethod = "tools/call"
				pending.McpName = exposedName
				pending.ActingFor = actingFor
				// Persist mutable fields to SQLite if applicable
				if sqlStore, ok := s.approvals.(*approval.SQLiteStore); ok {
					sqlStore.UpdateFields(pending.ID)
				}
				// Notify reviewer via webhook (signed URLs if signer available)
				if decision.Notify != "" {
					if s.approvalSigner != nil {
						s.goNotify(approval.NewSignedWebhookNotifier(decision.Notify, s.baseURL, "admin", s.approvalSigner), pending)
					} else {
						s.goNotify(approval.NewWebhookNotifier(decision.Notify, s.baseURL), pending)
					}
				} else if s.approvalNotif != nil {
					s.goNotify(s.approvalNotif, pending)
				}
				// Delegation-aware routing: notify the delegated user with user-specific signed URLs
				if actingFor != "" && decision.DelegationNotify != "" {
					if s.approvalSigner != nil {
						s.goNotify(approval.NewSignedWebhookNotifier(decision.DelegationNotify, s.baseURL, actingFor, s.approvalSigner), pending)
					} else {
						s.goNotify(approval.NewWebhookNotifier(decision.DelegationNotify, s.baseURL), pending)
					}
				}

				// Log the pending state
				entry.Decision = "pending_approval"
				entry.Mode = "approval"
				entry.Signals = toSignalEntries(signals)
				s.auditor.Log(entry)

				// Modern clients: return immediately OR block if running as task
				if server.IsModernRequest(ctx) {
					if isTaskCall {
						// Task-augmented call: block until resolved (mcp-go manages the task lifecycle)
						slog.Info("task-augmented approval — blocking until resolved", "component", "proxy", "approval_id", pending.ID, "tool", exposedName)
					} else {
						// Non-task modern call: return immediately with structured pending response
						return s.pendingApprovalResponse(pending, decision), nil
					}
				}

				// Block until approved, denied, expired, or task cancelled
				resolved := s.approvals.WaitForResolutionCtx(ctx, pending.ID)

				if resolved != nil && resolved.Status == approval.StatusApproved {
					entry.Decision = "approved"
					slog.Info("approval approved — executing tool", "component", "proxy", "approval_id", pending.ID, "tool", exposedName)
				} else {
					s.unusedTools.RecordCall(exposedName)
					s.recordBaseline(agentID, exposedName, route.ServerName, false, len(signals), args, 0)
					s.runAnalysis(agentID)
					entry.LatencyMs = time.Since(start).Milliseconds()
					if resolved != nil && resolved.Status == approval.StatusDenied {
						entry.Decision = "denied"
						reason := resolved.Reason
						if reason == "" {
							reason = "Reviewer denied the request"
						}
						s.auditor.Log(entry)
						return governanceError(ErrCodeDenied, requestID, "denied: %s", reason), nil
					}
					entry.Decision = "expired"
					s.auditor.Log(entry)
					return governanceError(ErrCodeExpired, requestID, "approval expired"), nil
				}
			}
		}

		// Consent flow: return MCP elicitation (input_required) for user consent.
		// If archetype is not user_embedded and consent was configured, fall back to require_approval.
		if decision.Action == "require_consent" && !consentGranted {
			if archetype != config.ArchetypeUserEmbedded && s.approvals != nil {
				slog.Warn("require_consent falling back to require_approval (agent archetype is not user_embedded)",
					"component", "proxy", "tool", exposedName, "agent", agentID, "archetype", archetype)
				decision.Action = "require_approval"

				// Check for existing approval before creating a new one (prevents duplicates on retry)
				if s.approvals.CheckApproved(agentID, exposedName, args) {
					entry.Decision = "approved"
					entry.Mode = "consent_fallback"
					// Approved: skip rest of consent block, proceed to upstream execution
					goto consentDone
				}

				existing := s.approvals.CheckPendingOrDenied(agentID, exposedName, args)
				if existing != nil {
					switch existing.Status {
					case approval.StatusPending:
						entry.Decision = "pending_approval"
						entry.Mode = "consent_fallback"
						s.auditor.Log(entry)
						return s.pendingApprovalResponse(existing, decision), nil
					case approval.StatusDenied:
						entry.Decision = "denied"
						entry.Mode = "consent_fallback"
						s.auditor.Log(entry)
						reason := existing.Reason
						if reason == "" {
							reason = "Reviewer denied the request"
						}
						return governanceError(ErrCodeDenied, requestID, "denied: %s", reason), nil
					case approval.StatusExpired:
						// Expired — allow creating a new approval below
					}
				}

				// Create new pending approval
				pending := s.approvals.Create(agentID, exposedName, args)
				pending.McpMethod = "tools/call"
				pending.McpName = exposedName
				pending.ActingFor = actingFor
				pending.RetryMethod = decision.RetryMethod
				pending.StatusWebhook = decision.StatusWebhook
				if sqlStore, ok := s.approvals.(*approval.SQLiteStore); ok {
					sqlStore.UpdateFields(pending.ID)
				}
				// Notify reviewer via webhook (signed URLs if signer available)
				if decision.Notify != "" {
					if s.approvalSigner != nil {
						s.goNotify(approval.NewSignedWebhookNotifier(decision.Notify, s.baseURL, "admin", s.approvalSigner), pending)
					} else {
						s.goNotify(approval.NewWebhookNotifier(decision.Notify, s.baseURL), pending)
					}
				} else if s.approvalNotif != nil {
					s.goNotify(s.approvalNotif, pending)
				}
				if actingFor != "" && decision.DelegationNotify != "" {
					if s.approvalSigner != nil {
						s.goNotify(approval.NewSignedWebhookNotifier(decision.DelegationNotify, s.baseURL, actingFor, s.approvalSigner), pending)
					} else {
						s.goNotify(approval.NewWebhookNotifier(decision.DelegationNotify, s.baseURL), pending)
					}
				}
				entry.Decision = "pending_approval"
				entry.Mode = "consent_fallback"
				entry.DecisionReason = "autonomous agent cannot respond to consent form"
				s.auditor.Log(entry)

				// Task-augmented: block until resolved (mcp-go manages task lifecycle)
				if isTaskCall {
					resolved := s.approvals.WaitForResolutionCtx(ctx, pending.ID)
					if resolved != nil && resolved.Status == approval.StatusApproved {
						entry.Decision = "approved"
						entry.Mode = "consent_fallback"
						goto consentDone
					}
					reason := "Approval denied or expired"
					if resolved != nil && resolved.Reason != "" {
						reason = resolved.Reason
					}
					return governanceError(ErrCodeDenied, requestID, "consent fallback denied: %s", reason), nil
				}
				return s.pendingApprovalResponse(pending, decision), nil
			}
			if s.stateEnc == nil {
				entry.Decision = "require_consent"
				entry.Mode = "consent"
				s.auditor.Log(entry)
				return governanceError(ErrCodeConsentRequired, requestID, "no approval_secret configured"), nil
			}
			state := approval.RequestState{
				AgentID:   agentID,
				Tool:      exposedName,
				ArgsHash:  approval.HashArgs(agentID, exposedName, args),
				Timestamp: time.Now(),
			}
			encrypted, err := s.stateEnc.Encrypt(state)
			if err != nil {
				entry.Decision = "require_consent"
				entry.Mode = "consent"
				s.auditor.Log(entry)
				return governanceError(ErrCodeConsentRequired, requestID, "state encryption failed"), nil
			}

			msg := "This tool requires your consent to proceed."
			schema := map[string]any{"type": "object", "properties": map[string]any{
				"confirmed": map[string]any{"type": "boolean", "title": "Confirm"},
			}}
			if decision.ConsentForm != nil {
				if decision.ConsentForm.Title != "" {
					msg = decision.ConsentForm.Title
				}
				if decision.ConsentForm.Description != "" {
					msg = msg + "\n\n" + decision.ConsentForm.Description
				}
			}

			builder := server.NewInputRequestBuilder(encrypted)
			builder.Elicit("consent", mcp.ElicitationParams{
				Message:         msg,
				RequestedSchema: schema,
			})

			entry.Decision = "require_consent"
			entry.Mode = "consent"
			entry.DecisionReason = decision.Reason
			entry.ConsentPhase = "requested"
			s.auditor.Log(entry)
			return builder.ToolResult(), nil
		}
		consentDone:

		// Layer 2: Engine evaluation
		if s.bridge != nil && s.bridge.IsConfigured() {
			apiKey := s.resolveAPIKey(agentID)
			engOrgID := s.resolveOrgID(ctx, agentID)
			engineResp, _ := s.bridge.DecideForTool(ctx, agentID, exposedName, args, apiKey, engOrgID)
			if engineResp != nil {
				entry.EngineDecisionID = engineResp.DecisionID
			}
			if engineResp != nil && !engineResp.Proceed {
				s.unusedTools.RecordCall(exposedName)
				s.recordBaseline(agentID, exposedName, route.ServerName, false, len(signals), args, 0)
				s.runAnalysis(agentID)
				entry.LatencyMs = time.Since(start).Milliseconds()
				entry.Decision = engineResp.Decision
				entry.Mode = "engine"
				entry.Signals = toSignalEntries(signals)
				s.auditor.Log(entry)
				msg := engineResp.Narrative.Summary
				if msg == "" {
					msg = engineResp.Decision
				}
				return governanceError(ErrCodeBlocked, requestID, "engine: %s: %s", engineResp.Decision, msg), nil
			}
		}

		// Forward to upstream. Pass through MRTP params for upstream elicitation
		// only if this is NOT a gateway consent retry (gateway state is consumed here,
		// not forwarded to upstream).
		var result *mcp.CallToolResult
		var err error
		if !consentGranted && (len(req.Params.InputResponses) > 0 || req.Params.RequestState != "") {
			result, err = s.pool.CallToolWithMRTP(ctx, exposedName, args, req.Params.MultiRoundTripParams)
		} else {
			result, err = s.pool.CallTool(ctx, exposedName, args)
		}
		latencyMs := time.Since(start).Milliseconds()
		entry.LatencyMs = latencyMs

		if err != nil {
			entry.IsError = true
			entry.ErrorMessage = err.Error()
			entry.ErrorCategory = classifyError(err)

			// Detect tool unavailability: -32602 "Unknown tool" from upstream
			// means the tool was removed. Invalidate cache for that server so
			// next ListTools re-fetches. Log as tool_removed signal.
			if isUnknownToolError(err) {
				s.pool.InvalidateServerTools(route.ServerName)
				signals = append(signals, detect.Signal{
					Name:     "tool_removed",
					Category: "tool_removed",
					Severity: "warning",
					Message:  fmt.Sprintf("Tool %q no longer available on server %s", exposedName, route.ServerName),
				})
			}
		} else {
			entry.ResultPreview = resultPreview(result)
			entry.IsError = result.IsError
			if result.IsError {
				entry.ErrorCategory = "application_error"
			}
		}

		// Record in history + sequence detector
		histEntry := detect.HistoryEntry{
			Tool: exposedName, Server: route.ServerName,
			Arguments: args, IsError: entry.IsError,
			Timestamp: time.Now(), Signals: signals,
		}
		seqSignals := s.sequence.Record(agentID, histEntry)
		s.history.Add(agentID, histEntry)
		s.unusedTools.RecordCall(exposedName)
		signals = append(signals, seqSignals...)

		// Workflow assertions
		recentTools := s.history.RecentTools(agentID)
		if wfSignal := s.workflow.CheckAssertion(agentID, recentTools); wfSignal != nil {
			signals = append(signals, *wfSignal)
		}

		// Record baselines + check anomalies
		s.recordBaseline(agentID, exposedName, route.ServerName, entry.IsError, len(signals), args, float64(latencyMs))

		// Periodic analysis (lightweight — runs inline)
		s.runAnalysis(agentID)

		entry.Signals = toSignalEntries(signals)
		if decision.Action != "allow" {
			entry.Mode = "rule"
		}
		if consentGranted {
			entry.ConsentPhase = "granted"
		}

		s.auditor.Log(entry)

		// Record Prometheus metrics
		metrics.ToolCallsTotal.WithLabelValues(exposedName, agentID, entry.Decision, route.ServerName).Inc()
		metrics.ToolLatency.WithLabelValues(exposedName, route.ServerName).Observe(float64(latencyMs) / 1000.0)
		if entry.IsError && entry.ErrorCategory != "" && entry.ErrorCategory != "application_error" {
			metrics.UpstreamErrorsTotal.WithLabelValues(route.ServerName, entry.ErrorCategory).Inc()
		}

		if err != nil {
			// Sanitize gateway-internal errors — don't leak internal details to agents.
			// Full error is already logged and stored in audit entry.
			slog.Debug("upstream call failed", "component", "proxy", "tool", exposedName, "error", err)
			return governanceError(ErrCodeServerError, requestID, "server %s unavailable", route.ServerName), nil
		}
		return result, nil
	}
}

// recordBaseline records an observation in baseline tracker and latency tracker,
// then checks for anomalies via the alert detector.
func (s *Server) recordBaseline(agentID, tool, serverName string, isError bool, signalCount int, args map[string]any, latencyMs float64) {
	// Check for anomalies BEFORE recording (so new args/tools are detected)
	if s.alertDetector != nil {
		detectedAlerts := s.alertDetector.Check(agentID, tool, isError, signalCount, args)
		if s.alertWebhook != nil {
			for _, a := range detectedAlerts {
				alert := a
				s.webhookWg.Add(1)
				go func() {
					defer s.webhookWg.Done()
					s.alertWebhook.Send(alert, nil)
				}()
			}
		}
	}

	if s.tracker != nil {
		s.tracker.Record(baseline.Observation{
			AgentID:     agentID,
			Tool:        tool,
			IsError:     isError,
			SignalCount: signalCount,
			Args:        args,
			Timestamp:   time.Now(),
		})
	}

	if s.latency != nil && latencyMs > 0 {
		s.latency.Record(serverName, latencyMs, isError)
	}
}

// runAnalysis periodically runs dataflow and error analysis.
// Called inline but only does work every 100 calls per agent.
func (s *Server) runAnalysis(agentID string) {
	history := s.history.Get(agentID)
	if len(history) < 10 || len(history)%50 != 0 {
		return // only analyze every 50 calls
	}

	s.dataflow.Analyze(history, s.profiler)
	s.errorAnalyzer.Analyze(history)
}

// rawArgsCache stores raw tool arguments extracted from HTTP body before mcp-go parsing.
// Keyed by SHA-256 of the request body. Workaround for mcp-go v1.3.0 MRTP args bug.
var rawArgsCache sync.Map

// httpContextFunc injects agent identity, protocol era, and org into the request context.
// Bearer token auth is resolved here (priority 1). Further resolution (_meta, session) happens
// via resolveAgent() at handler time when the full MCP context is available.
func (s *Server) httpContextFunc(ctx context.Context, r *http.Request) context.Context {
	// Note: raw args extraction for MRTP retry workaround now done in the HTTP handler
	// wrapper in cmd/gateway/main.go (runs before mcp-go reads the body).

	res := s.auth.AuthenticateWithClaims(r)
	agentID := res.AgentID
	identitySource := res.Source
	if !res.OK {
		if !s.auth.IsAnonymous() {
			slog.Warn("rejected unauthenticated request", "component", "proxy", "remote_addr", r.RemoteAddr)
			ctx = ContextWithUnauthorized(ctx)
		}
		agentID = "anonymous"
		identitySource = IdentityAnonymous
	}

	// Store JWT claims in context when authenticated via external AS
	if res.Claims != nil {
		ctx = ContextWithJWTClaims(ctx, res.Claims)
	}

	// Stash the bearer token in context for initialize hook to use
	if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		ctx = contextWithToken(ctx, strings.TrimPrefix(authHeader, "Bearer "))
	}

	ctx = ContextWithAgent(ctx, agentID)
	ctx = ContextWithIdentitySource(ctx, identitySource)

	orgID := s.orgResolver.Resolve(r, agentID)
	if orgID != "" {
		ctx = ContextWithOrg(ctx, orgID)
	}

	return ctx
}

func (s *Server) afterCallTool(ctx context.Context, id any, message *mcp.CallToolRequest, result any) {}

func (s *Server) resolveAPIKey(agentID string) string {
	s.agentMu.RLock()
	agents := s.agentConfigs
	s.agentMu.RUnlock()
	for _, a := range agents {
		if a.ID == agentID && a.Product != "" {
			for _, p := range s.productConfigs {
				if p.Name == a.Product {
					return p.APIKey
				}
			}
		}
	}
	return ""
}

func (s *Server) resolveOrgID(ctx context.Context, agentID string) string {
	return s.orgResolver.ResolveFromContext(ctx, agentID)
}

func toSignalEntries(signals []detect.Signal) []audit.SignalEntry {
	if len(signals) == 0 {
		return nil
	}
	entries := make([]audit.SignalEntry, len(signals))
	for i, sig := range signals {
		entries[i] = audit.SignalEntry{
			Name: sig.Name, Category: sig.Category,
			Severity: sig.Severity, Message: sig.Message,
		}
	}
	return entries
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "circuit is open"):
		return "circuit_open"
	case strings.Contains(msg, "401") || strings.Contains(msg, "403") ||
		strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden"):
		return "auth_error"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return "timeout"
	case strings.Contains(msg, "session terminated") || strings.Contains(msg, "missing session") ||
		strings.Contains(msg, "need to re-initialize"):
		return "session_error"
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") || strings.Contains(msg, "eof"):
		return "network"
	default:
		return "application_error"
	}
}

// pendingApprovalResponse builds the response message for a pending approval.
// Includes structured approval metadata in _meta for programmatic clients.
func (s *Server) pendingApprovalResponse(entry *approval.Entry, decision rules.Decision) *mcp.CallToolResult {
	retryMethod := decision.RetryMethod
	if retryMethod == "" {
		retryMethod = "poll"
	}
	hintMs := decision.RetryHintMs
	if hintMs <= 0 {
		hintMs = 30000
	}

	var msg string
	if retryMethod == "callback" {
		msg = fmt.Sprintf("[Recall] Pending external approval (ID: %s). Reviewer notified. Your system will be notified when resolved.", entry.ID)
	} else {
		msg = fmt.Sprintf("[Recall] Pending external approval (ID: %s). Reviewer notified. Retry in ~%ds.", entry.ID, hintMs/1000)
	}

	result := mcp.NewToolResultError(msg)
	// Add structured approval metadata for programmatic clients
	result.Meta = &mcp.Meta{}
	result.Meta.SetMetaField("recall:errorCode", ErrCodePendingApproval)
	result.Meta.SetMetaField("recall:approval", map[string]any{
		"id":           entry.ID,
		"status":       "pending",
		"retryMethod":  retryMethod,
		"retryHintMs":  hintMs,
		"expiresAt":    entry.ExpiresAt.Format(time.RFC3339),
	})
	return result
}

// isUnknownToolError checks if an upstream error indicates the tool no longer exists.
// MCP spec uses -32602 (Invalid Params) with "Unknown tool" message.
func isUnknownToolError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown tool") || strings.Contains(msg, "-32602")
}

func resultPreview(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			if len(tc.Text) > 200 {
				return tc.Text[:200] + "..."
			}
			return tc.Text
		}
	}
	return fmt.Sprintf("(%d content items)", len(result.Content))
}

// detachContext creates a new context that inherits all values from the parent
// but is NOT cancelled when the parent is cancelled. Used for task goroutines
// that need to outlive the HTTP request context.
type detachedContext struct{ parent context.Context }

func (d detachedContext) Deadline() (time.Time, bool)       { return time.Time{}, false }
func (d detachedContext) Done() <-chan struct{}              { return nil }
func (d detachedContext) Err() error                        { return nil }
func (d detachedContext) Value(key any) any                 { return d.parent.Value(key) }

func detachContext(ctx context.Context) context.Context {
	return detachedContext{parent: ctx}
}

func generateRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "req-" + hex.EncodeToString(b)
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
