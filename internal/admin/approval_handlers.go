package admin

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/approval"
)

// ApprovalHandlers provides HTTP handlers for the approval admin API.
type ApprovalHandlers struct {
	store       approval.StoreInterface
	adminKey    string // if non-empty, require Authorization: Bearer <key> for admin approve/deny
	signer      *approval.SignedURLGenerator // if non-nil, enables signed URL approval endpoint
	signedLimiter *signedURLRateLimiter // per-IP rate limiter for signed URL endpoint
	SessionValidator func(r *http.Request) bool // if non-nil, accepts dashboard session as auth
	TrustedProxies   []*net.IPNet // CIDR ranges to trust X-Forwarded-For from
}

// NewApprovalHandlers creates approval handlers. adminKey controls admin endpoint auth.
func NewApprovalHandlers(store approval.StoreInterface, adminKey string) *ApprovalHandlers {
	return &ApprovalHandlers{
		store:         store,
		adminKey:      adminKey,
		signedLimiter: newSignedURLRateLimiter(10), // 10 requests per minute per IP
	}
}

// resolveClientIP extracts the client IP from a request.
// If RemoteAddr matches a trusted proxy CIDR, X-Forwarded-For is used.
// Otherwise, RemoteAddr is returned (ignoring any XFF header).
func resolveClientIP(r *http.Request, trusted []*net.IPNet) string {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	if len(trusted) == 0 {
		return ip
	}
	remoteIP := net.ParseIP(ip)
	if remoteIP == nil {
		return ip
	}
	for _, cidr := range trusted {
		if cidr.Contains(remoteIP) {
			if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
				return strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0])
			}
			break
		}
	}
	return ip
}

// ParseTrustedProxies parses a list of CIDR strings into net.IPNet values.
func ParseTrustedProxies(cidrs []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, s := range cidrs {
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			slog.Warn("invalid trusted_proxy CIDR, skipping", "component", "config", "cidr", s, "error", err)
			continue
		}
		nets = append(nets, ipNet)
	}
	return nets
}

// signedURLRateLimiter tracks per-IP request counts for the signed approval endpoint.
type signedURLRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*signedBucket
	limit   int
}

type signedBucket struct {
	count   int
	resetAt time.Time
}

func newSignedURLRateLimiter(perMinute int) *signedURLRateLimiter {
	return &signedURLRateLimiter{
		buckets: make(map[string]*signedBucket),
		limit:   perMinute,
	}
}

func (rl *signedURLRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok || now.After(b.resetAt) {
		rl.buckets[ip] = &signedBucket{count: 1, resetAt: now.Add(time.Minute)}
		// Cleanup stale entries (>2min old) to prevent unbounded map growth
		if len(rl.buckets) > 100 {
			cutoff := now.Add(-2 * time.Minute)
			for k, v := range rl.buckets {
				if v.resetAt.Before(cutoff) {
					delete(rl.buckets, k)
				}
			}
		}
		return true
	}
	b.count++
	return b.count <= rl.limit
}

// SetSigner configures signed URL generation/validation for the approval endpoints.
func (h *ApprovalHandlers) SetSigner(signer *approval.SignedURLGenerator) {
	h.signer = signer
}

// RegisterRoutes mounts approval routes on the mux.
func (h *ApprovalHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/approvals", h.handleList)
	mux.HandleFunc("/admin/v1/approvals/", h.handleApprovalAction)
	mux.HandleFunc("/approvals/", h.handleSignedApproval) // signed URL endpoint (GET for browser, POST for programmatic)
}

// checkAuth verifies the admin key when configured. Returns false if auth fails (response already written).
func (h *ApprovalHandlers) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if h.adminKey == "" {
		return true // no auth configured
	}
	// Accept dashboard session cookie (if dashboard auth is configured)
	if h.SessionValidator != nil && h.SessionValidator(r) {
		return true
	}
	// Accept admin_key Bearer token
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), []byte(h.adminKey)) != 1 {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return false
	}
	return true
}

// reviewerFromRequest extracts the reviewer identity from the request.
func reviewerFromRequest(r *http.Request) string {
	if reviewer := r.Header.Get("X-Reviewer"); reviewer != "" {
		return reviewer
	}
	return "admin-api"
}

// handleList returns all pending approvals.
func (h *ApprovalHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !h.checkAuth(w, r) {
		return
	}

	pending := h.store.Pending()
	// Build history from resolved entries
	all := h.store.All()
	var history []approval.Entry
	for _, e := range all {
		if e.Status != approval.StatusPending {
			history = append(history, e)
		}
	}
	// Sort history newest first by resolved time
	sort.Slice(history, func(i, j int) bool {
		ti := history[i].ResolvedAt
		if ti.IsZero() { ti = history[i].ExpiresAt }
		tj := history[j].ResolvedAt
		if tj.IsZero() { tj = history[j].ExpiresAt }
		return ti.After(tj)
	})
	if len(history) > 20 {
		history = history[:20]
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"approvals": pending,
		"count":     len(pending),
		"history":   history,
	})
}

// handleApprovalAction routes approve/deny requests.
func (h *ApprovalHandlers) handleApprovalAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !h.checkAuth(w, r) {
		return
	}

	// Parse path: /admin/v1/approvals/{id}/approve or /deny
	path := strings.TrimPrefix(r.URL.Path, "/admin/v1/approvals/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
		return
	}

	id := parts[0]
	action := parts[1]

	w.Header().Set("Content-Type", "application/json")

	reviewer := reviewerFromRequest(r)

	switch action {
	case "approve":
		entry, err := h.store.Approve(id, reviewer)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		go h.fireStatusWebhook(entry)
		json.NewEncoder(w).Encode(map[string]any{
			"status":   "approved",
			"approval": entry,
		})

	case "deny":
		var body struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		entry, err := h.store.Deny(id, reviewer, body.Reason)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		go h.fireStatusWebhook(entry)
		json.NewEncoder(w).Encode(map[string]any{
			"status":   "denied",
			"approval": entry,
		})

	default:
		http.Error(w, `{"error":"unknown action"}`, http.StatusBadRequest)
	}
}

// fireStatusWebhook sends a status notification to the configured webhook URL
// when an approval is resolved. Only fires for callback mode with a status_webhook URL.
func (h *ApprovalHandlers) fireStatusWebhook(entry *approval.Entry) {
	if entry == nil || entry.StatusWebhook == "" {
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"approval_id": entry.ID,
		"status":      string(entry.Status),
		"tool":        entry.Tool,
		"agent_id":    entry.AgentID,
		"resolved_by": entry.ResolvedBy,
		"resolved_at": entry.ResolvedAt,
		"reason":      entry.Reason,
	})

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(entry.StatusWebhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		slog.Warn("status webhook delivery failed", "component", "approval", "approval_id", entry.ID, "url", entry.StatusWebhook, "error", err)
		return
	}
	resp.Body.Close()
	slog.Info("status webhook delivered", "component", "approval", "approval_id", entry.ID, "url", entry.StatusWebhook, "status_code", resp.StatusCode)
}

// handleSignedApproval handles approval actions via signed URLs.
// GET: browser click from Slack/email (returns HTML confirmation)
// POST: programmatic call (returns JSON, supports deny reason)
// Path: /approvals/{id}/{action}?reviewer=...&exp=...&sig=...
func (h *ApprovalHandlers) handleSignedApproval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.signer == nil {
		http.Error(w, `{"error":"signed approvals not configured"}`, http.StatusNotImplemented)
		return
	}

	// Rate limit per IP. Only trust X-Forwarded-For from configured trusted proxies.
	ip := resolveClientIP(r, h.TrustedProxies)
	if h.signedLimiter != nil && !h.signedLimiter.allow(strings.TrimSpace(ip)) {
		slog.Warn("signed approval rate limited", "component", "approval", "ip", ip)
		w.Header().Set("Retry-After", "60")
		http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
		return
	}

	// Parse path: /approvals/{id}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/approvals/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
		return
	}
	id, action := parts[0], parts[1]

	// Validate signed URL
	params := approval.ParseQuery(id, action, r.URL.Query())
	if err := h.signer.Validate(params); err != nil {
		slog.Warn("signed approval rejected", "component", "approval", "id", id, "action", action, "error", err)
		if r.Method == http.MethodGet {
			h.writeHTML(w, http.StatusForbidden, "Approval Link Invalid",
				"This approval link is invalid or has expired.", "error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	reviewer := params.Reviewer

	// Execute the approval/denial
	switch action {
	case "approve":
		entry, err := h.store.Approve(id, reviewer)
		if err != nil {
			slog.Warn("signed approval failed", "component", "approval", "id", id, "error", err)
			if r.Method == http.MethodGet {
				h.writeHTML(w, http.StatusBadRequest, "Approval Failed", err.Error(), "error")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		go h.fireStatusWebhook(entry)
		slog.Info("signed approval approved", "component", "approval", "id", id, "reviewer", reviewer)
		if r.Method == http.MethodGet {
			h.writeHTML(w, http.StatusOK, "Approved",
				"Tool call <b>"+html.EscapeString(entry.Tool)+"</b> by <b>"+html.EscapeString(entry.AgentID)+"</b> has been approved.", "success")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "approved", "approval": entry})

	case "deny":
		reason := ""
		if r.Method == http.MethodPost {
			var body struct{ Reason string `json:"reason"` }
			json.NewDecoder(r.Body).Decode(&body)
			reason = body.Reason
		}
		if reason == "" {
			reason = "denied via signed link"
		}
		entry, err := h.store.Deny(id, reviewer, reason)
		if err != nil {
			if r.Method == http.MethodGet {
				h.writeHTML(w, http.StatusBadRequest, "Denial Failed", err.Error(), "error")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		go h.fireStatusWebhook(entry)
		slog.Info("signed approval denied", "component", "approval", "id", id, "reviewer", reviewer, "reason", reason)
		if r.Method == http.MethodGet {
			h.writeHTML(w, http.StatusOK, "Denied",
				"Tool call <b>"+html.EscapeString(entry.Tool)+"</b> by <b>"+html.EscapeString(entry.AgentID)+"</b> has been denied.", "warning")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "denied", "approval": entry})

	default:
		http.Error(w, `{"error":"unknown action"}`, http.StatusBadRequest)
	}
}

// writeHTML renders a minimal HTML response for browser-based approval clicks.
func (h *ApprovalHandlers) writeHTML(w http.ResponseWriter, status int, title, message, style string) {
	color := "#22c55e" // green
	if style == "error" {
		color = "#ef4444"
	} else if style == "warning" {
		color = "#f59e0b"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s — Recall MCP Gateway</title>
<style>body{font-family:system-ui,sans-serif;background:#0d0f14;color:#c8cdd8;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.card{background:#12151c;border:1px solid #1e2330;border-radius:12px;padding:32px;max-width:400px;text-align:center}
.dot{width:12px;height:12px;border-radius:50%%;background:%s;display:inline-block;margin-right:8px}
h1{font-size:20px;margin:0 0 12px} p{color:#5a6070;font-size:14px;line-height:1.5}</style></head>
<body><div class="card"><h1><span class="dot"></span>%s</h1><p>%s</p></div></body></html>`,
		title, color, title, message)
}
