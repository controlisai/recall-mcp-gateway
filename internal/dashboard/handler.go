package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/audit"
	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/detect"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieName = "recall_session"
	sessionTTL        = 8 * time.Hour
)

// sseRelay abstracts the Redis SSE relay for cross-pod event broadcasting.
// When nil, the dashboard only shows events from the local pod.
type sseRelay interface {
	PublishEvent(entry json.RawMessage) error
	SubscribeEvents(ctx context.Context) <-chan json.RawMessage
}

type session struct {
	user      string
	expiresAt time.Time
}

// Handler serves the embedded dashboard and SSE audit feed.
type Handler struct {
	pool     *upstream.Pool
	auditor  audit.AuditStore
	profiler *detect.Profiler
	latency  *baseline.LatencyTracker
	version       string
	defaultAction  string // "allow" or "block"
	mcpHeaderMode  string // "passthrough", "generate", "managed"
	tracingEnabled bool
	tracingEndpoint string
	metricsEnabled bool
	authMode       string // "external" or "built-in"
	authIssuer     string // external AS issuer URL
	authAudience   string
	authJwksUrl    string
	maxDelegDepth  int
	approvalSecretConfigured bool
	slackSecretConfigured    bool
	allowedOrigins           []string
	approvalTtl              string
	approvalStorage          string
	clusterMode              bool   // true when Redis is connected
	redisURL                 string // Redis URL (display only)
	sseRelay                 sseRelay // optional Redis SSE relay (nil = standalone)
	staticFS http.Handler
	user     string // empty = no auth
	passHash string // bcrypt hash

	sessionMu sync.RWMutex
	sessions  map[string]*session // token -> session
}

// defaultAdminPassHash is bcrypt hash of "admin" — default credentials.
var defaultAdminPassHash = "$2a$10$QAt3Vylrk8B6PDGjy10CLe33tnCq.IrEBXj3wDKJn3jmA5aSUDK2i"

// NewHandler creates the dashboard HTTP handler.
// passHash must be a bcrypt hash. If empty and user is set, uses default hash (password: "admin").
// The ctx is used to stop the session cleanup goroutine on shutdown.
func NewHandler(pool *upstream.Pool, auditor audit.AuditStore, profiler *detect.Profiler, latency *baseline.LatencyTracker, version, user, passHash string, ctx context.Context) (*Handler, error) {
	staticFS, err := StaticFS()
	if err != nil {
		return nil, err
	}
	// Default credentials: admin / admin
	if user != "" && passHash == "" {
		passHash = defaultAdminPassHash
	}
	if user == "" && passHash != "" {
		user = "admin"
	}
	h := &Handler{
		pool:     pool,
		auditor:  auditor,
		profiler: profiler,
		latency:  latency,
		version:  version,
		staticFS: http.FileServer(http.FS(staticFS)),
		user:     user,
		passHash: passHash,
		sessions: make(map[string]*session),
	}

	// Periodic session cleanup (every 15 minutes)
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.cleanExpiredSessions()
			}
		}
	}()

	return h, nil
}


// SetModeConfig sets the gateway mode config for dashboard display.
func (h *Handler) SetModeConfig(defaultAction, mcpHeaderMode string) {
	h.defaultAction = defaultAction
	h.mcpHeaderMode = mcpHeaderMode
}

// SetAuthConfig sets auth config for dashboard display.
func (h *Handler) SetAuthConfig(authMode, authIssuer string) {
	h.authMode = authMode
	h.authIssuer = authIssuer
}

// SetAuthServerDetails sets AS read-only details for dashboard display.
func (h *Handler) SetAuthServerDetails(audience, jwksURL string, maxDepth int) {
	h.authAudience = audience
	h.authJwksUrl = jwksURL
	h.maxDelegDepth = maxDepth
}

// UpdateSettingValue updates a single setting for the next SSE init.
func (h *Handler) UpdateSettingValue(key, value string) {
	switch key {
	case "mcpHeaderMode":
		h.mcpHeaderMode = value
	case "approvalTtl":
		h.approvalTtl = value
	}
}

// SetAllowedOrigins updates the displayed origins list.
func (h *Handler) SetAllowedOrigins(origins []string) {
	h.allowedOrigins = origins
}

// SetClusterMode sets whether the gateway is running in cluster mode (Redis connected).
func (h *Handler) SetClusterMode(enabled bool, redisURL string) {
	h.clusterMode = enabled
	h.redisURL = redisURL
}

// SetSSERelay wires the Redis SSE relay for cross-pod event broadcasting.
// When set, audit entries logged on this pod are published to Redis, and events
// published by other pods are delivered to all SSE subscribers on this pod.
func (h *Handler) SetSSERelay(relay sseRelay) {
	h.sseRelay = relay
}

// SetSecurityConfig sets security-related config for dashboard display.
func (h *Handler) SetSecurityConfig(approvalSecretConfigured, slackSecretConfigured bool, allowedOrigins []string, approvalTtl, approvalStorage string) {
	h.approvalSecretConfigured = approvalSecretConfigured
	h.slackSecretConfigured = slackSecretConfigured
	h.allowedOrigins = allowedOrigins
	h.approvalTtl = approvalTtl
	h.approvalStorage = approvalStorage
}

// SetObservabilityConfig sets observability config for dashboard display.
func (h *Handler) SetObservabilityConfig(tracingEnabled bool, tracingEndpoint string, metricsEnabled bool) {
	h.tracingEnabled = tracingEnabled
	h.tracingEndpoint = tracingEndpoint
	h.metricsEnabled = metricsEnabled
}

func (h *Handler) cleanExpiredSessions() {
	h.sessionMu.Lock()
	defer h.sessionMu.Unlock()
	now := time.Now()
	for token, sess := range h.sessions {
		if now.After(sess.expiresAt) {
			delete(h.sessions, token)
		}
	}
}

func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// noCacheStatic wraps the static file server with no-cache headers to prevent stale JS.
func (h *Handler) noCacheStatic() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		h.staticFS.ServeHTTP(w, r)
	})
}

// RegisterRoutes mounts the dashboard routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/dashboard", http.RedirectHandler("/dashboard/", http.StatusMovedPermanently))
	mux.Handle("/dashboard/login", http.HandlerFunc(h.handleLogin))
	mux.Handle("/dashboard/logout", http.HandlerFunc(h.handleLogout))
	mux.Handle("/dashboard/auth-status", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"authEnabled": h.user != ""})
	}))
	mux.Handle("/dashboard/", h.withAuth(http.StripPrefix("/dashboard/", h.noCacheStatic())))
	mux.Handle("/ws/audit", h.withAuth(http.HandlerFunc(h.handleSSE)))
}

// withAuth wraps a handler with cookie-based session auth.
func (h *Handler) withAuth(next http.Handler) http.Handler {
	if h.user == "" {
		return next // no auth configured
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.IsValidSession(r) {
			next.ServeHTTP(w, r)
			return
		}
		// Not authenticated — serve login page
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(loginPage))
	})
}

// IsValidSession checks if the request has a valid dashboard session cookie.
func (h *Handler) IsValidSession(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	h.sessionMu.RLock()
	sess, ok := h.sessions[cookie.Value]
	h.sessionMu.RUnlock()
	return ok && time.Now().Before(sess.expiresAt)
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(loginPage))
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	r.ParseForm()
	user := r.FormValue("username")
	pass := r.FormValue("password")

	if user != h.user || bcrypt.CompareHashAndPassword([]byte(h.passHash), []byte(pass)) != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(loginPageWithError))
		return
	}

	// Create session
	token := generateSessionToken()
	h.sessionMu.Lock()
	h.sessions[token] = &session{user: user, expiresAt: time.Now().Add(sessionTTL)}
	h.sessionMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})

	http.Redirect(w, r, "/dashboard/", http.StatusSeeOther)
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		h.sessionMu.Lock()
		delete(h.sessions, cookie.Value)
		h.sessionMu.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})

	http.Redirect(w, r, "/dashboard/login", http.StatusSeeOther)
}

func (h *Handler) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	if h.user == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*") // open CORS only when no auth
	}

	initData := h.buildInitMessage()
	writeSSE(w, flusher, initData)

	entryCh := make(chan audit.Entry, 64)
	var closed sync.Once
	closeCh := make(chan struct{})

	unsub := h.auditor.Subscribe(func(e audit.Entry) {
		// Publish to Redis relay so other pods receive this event.
		if h.sseRelay != nil {
			if data, err := json.Marshal(e); err == nil {
				_ = h.sseRelay.PublishEvent(data)
			}
		}
		select {
		case entryCh <- e:
		default:
		}
	})
	defer unsub()

	// Subscribe to events from other pods via Redis relay.
	var relayCh <-chan json.RawMessage
	if h.sseRelay != nil {
		relayCh = h.sseRelay.SubscribeEvents(r.Context())
	}

	go func() {
		<-r.Context().Done()
		closed.Do(func() { close(closeCh) })
	}()

	for {
		select {
		case <-closeCh:
			return
		case entry := <-entryCh:
			writeSSE(w, flusher, map[string]any{"type": "entry", "entry": entry})
		case raw, ok := <-relayCh:
			if !ok {
				relayCh = nil // channel closed (context done) — stop selecting on it
				continue
			}
			// Deliver cross-pod event as a raw JSON entry to the SSE client.
			writeSSE(w, flusher, map[string]any{"type": "entry", "entry": json.RawMessage(raw)})
		}
	}
}

func (h *Handler) buildInitMessage() map[string]any {
	statuses := h.pool.Statuses()
	servers := make([]map[string]any, len(statuses))
	for i, s := range statuses {
		srv := map[string]any{
			"name": s.Name, "status": s.Status, "tool_count": s.ToolCount,
			"circuit_state": s.CircuitState, "reconnect_count": s.ReconnectCount,
		}
		if s.ServerEra != "" {
			srv["server_era"] = s.ServerEra
		}
		if s.AuthType != "" {
			srv["auth_type"] = s.AuthType
		}
		if !s.LastReconnectAt.IsZero() {
			srv["last_reconnect_at"] = s.LastReconnectAt
		}
		if h.latency != nil {
			if health := h.latency.Get(s.Name); health != nil {
				srv["meanLatencyMs"] = health.MeanLatencyMs
				srv["errorRate"] = health.ErrorRate
				srv["healthStatus"] = health.Status
				srv["sampleCount"] = health.SampleCount
			}
		}
		if len(s.Events) > 0 {
			srv["events"] = s.Events
		}
		servers[i] = srv
	}

	msg := map[string]any{
		"type":    "init",
		"version": h.version,
		"servers": servers,
		"recent":  h.auditor.Recent(),
		"user":    h.user, // empty string if no auth
	}
	if h.defaultAction != "" {
		msg["defaultAction"] = h.defaultAction
	}
	if h.mcpHeaderMode != "" {
		msg["mcpHeaderMode"] = h.mcpHeaderMode
	}
	msg["tracingEnabled"] = h.tracingEnabled
	if h.tracingEndpoint != "" {
		msg["tracingEndpoint"] = h.tracingEndpoint
	}
	msg["metricsEnabled"] = h.metricsEnabled
	if h.authMode != "" {
		msg["authMode"] = h.authMode
	}
	if h.authIssuer != "" {
		msg["authIssuer"] = h.authIssuer
	}
	if h.authAudience != "" {
		msg["authAudience"] = h.authAudience
	}
	if h.authJwksUrl != "" {
		msg["authJwksUrl"] = h.authJwksUrl
	}
	if h.maxDelegDepth > 0 {
		msg["maxDelegationDepth"] = h.maxDelegDepth
	}
	msg["approvalSecretConfigured"] = h.approvalSecretConfigured
	msg["slackSecretConfigured"] = h.slackSecretConfigured
	if len(h.allowedOrigins) > 0 {
		msg["allowedOrigins"] = h.allowedOrigins
	}
	if h.approvalTtl != "" {
		msg["approvalTtl"] = h.approvalTtl
	}
	if h.approvalStorage != "" {
		msg["approvalStorage"] = h.approvalStorage
	}
	msg["clusterMode"] = h.clusterMode
	if h.redisURL != "" {
		msg["redisURL"] = h.redisURL
	}

	// Include full tool profiles (with server + classification) and legacy risk grouping
	if h.profiler != nil {
		allProfiles := h.profiler.AllProfiles()
		msg["tools"] = allProfiles

		// Legacy toolProfiles for backward compat
		profiles := h.profiler.ProfilesByRisk()
		toolRisk := make(map[string]any)
		for level, tools := range profiles {
			if len(tools) > 0 {
				names := make([]string, len(tools))
				for i, t := range tools {
					names[i] = t.Name
				}
				toolRisk[level] = names
			}
		}
		msg["toolProfiles"] = toolRisk
	}

	// Tool annotations from upstream servers (passthrough + config overrides)
	if h.pool != nil {
		msg["toolAnnotations"] = h.pool.Registry().ToolAnnotations()
	}

	return msg
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, data any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		slog.Error("marshal error", "component", "dashboard", "error", err)
		return
	}
	w.Write([]byte("data: "))
	w.Write(jsonData)
	w.Write([]byte("\n\n"))
	flusher.Flush()
}

const loginPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Recall MCP Gateway — Login</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif; background: #0a0a0a; color: #e5e5e5; display: flex; align-items: center; justify-content: center; min-height: 100vh; }
  .login-card { background: #141414; border: 1px solid #262626; border-radius: 12px; padding: 40px; width: 100%; max-width: 380px; }
  .logo { display: flex; align-items: center; gap: 10px; margin-bottom: 32px; justify-content: center; }
  .logo-text { font-size: 18px; font-weight: 600; letter-spacing: -0.3px; }
  .logo-text span { color: #3b82f6; }
  .field { margin-bottom: 16px; }
  .field label { display: block; font-size: 13px; color: #a3a3a3; margin-bottom: 6px; font-weight: 500; }
  .field input { width: 100%; padding: 10px 12px; background: #0a0a0a; border: 1px solid #262626; border-radius: 8px; color: #e5e5e5; font-size: 14px; outline: none; transition: border-color 0.15s; }
  .field input:focus { border-color: #3b82f6; }
  .btn { width: 100%; padding: 10px; background: #3b82f6; color: #fff; border: none; border-radius: 8px; font-size: 14px; font-weight: 500; cursor: pointer; margin-top: 8px; transition: background 0.15s; }
  .btn:hover { background: #2563eb; }
  .error { color: #ef4444; font-size: 13px; margin-bottom: 16px; text-align: center; }
  .footer { text-align: center; margin-top: 24px; font-size: 12px; color: #525252; }
</style>
</head>
<body>
<div class="login-card">
  <div class="logo">
    <img src="data:image/png;base64,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" alt="ControlisAI" width="28" height="28" style="border-radius:6px">
    <div class="logo-text"><span>Recall</span> MCP Gateway</div>
  </div>
  <form method="POST" action="/dashboard/login">
    <div class="field">
      <label for="username">Username</label>
      <input type="text" id="username" name="username" autocomplete="username" autofocus required>
    </div>
    <div class="field">
      <label for="password">Password</label>
      <input type="password" id="password" name="password" autocomplete="current-password" required>
    </div>
    <button type="submit" class="btn">Sign in</button>
  </form>
  <div class="footer">MCP tool control plane</div>
</div>
</body>
</html>`

const loginPageWithError = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Recall MCP Gateway — Login</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif; background: #0a0a0a; color: #e5e5e5; display: flex; align-items: center; justify-content: center; min-height: 100vh; }
  .login-card { background: #141414; border: 1px solid #262626; border-radius: 12px; padding: 40px; width: 100%; max-width: 380px; }
  .logo { display: flex; align-items: center; gap: 10px; margin-bottom: 32px; justify-content: center; }
  .logo-text { font-size: 18px; font-weight: 600; letter-spacing: -0.3px; }
  .logo-text span { color: #3b82f6; }
  .field { margin-bottom: 16px; }
  .field label { display: block; font-size: 13px; color: #a3a3a3; margin-bottom: 6px; font-weight: 500; }
  .field input { width: 100%; padding: 10px 12px; background: #0a0a0a; border: 1px solid #262626; border-radius: 8px; color: #e5e5e5; font-size: 14px; outline: none; transition: border-color 0.15s; }
  .field input:focus { border-color: #3b82f6; }
  .btn { width: 100%; padding: 10px; background: #3b82f6; color: #fff; border: none; border-radius: 8px; font-size: 14px; font-weight: 500; cursor: pointer; margin-top: 8px; transition: background 0.15s; }
  .btn:hover { background: #2563eb; }
  .error { color: #ef4444; font-size: 13px; margin-bottom: 16px; text-align: center; }
  .footer { text-align: center; margin-top: 24px; font-size: 12px; color: #525252; }
</style>
</head>
<body>
<div class="login-card">
  <div class="logo">
    <img src="data:image/png;base64,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" alt="ControlisAI" width="28" height="28" style="border-radius:6px">
    <div class="logo-text"><span>Recall</span> MCP Gateway</div>
  </div>
  <form method="POST" action="/dashboard/login">
    <div class="error">Invalid username or password</div>
    <div class="field">
      <label for="username">Username</label>
      <input type="text" id="username" name="username" autocomplete="username" autofocus required>
    </div>
    <div class="field">
      <label for="password">Password</label>
      <input type="password" id="password" name="password" autocomplete="current-password" required>
    </div>
    <button type="submit" class="btn">Sign in</button>
  </form>
  <div class="footer">MCP tool control plane</div>
</div>
</body>
</html>`
