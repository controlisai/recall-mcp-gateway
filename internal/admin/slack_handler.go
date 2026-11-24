package admin

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/approval"
)

// SlackHandler handles inbound Slack interactive payload callbacks.
type SlackHandler struct {
	store         approval.StoreInterface
	signingSecret string // Slack app signing secret for request verification
}

// NewSlackHandler creates a Slack callback handler.
func NewSlackHandler(store approval.StoreInterface, signingSecret string) *SlackHandler {
	return &SlackHandler{store: store, signingSecret: signingSecret}
}

// RegisterRoutes mounts the Slack callback endpoint.
func (h *SlackHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/slack/callback", h.handleCallback)
}

// slackPayload represents the relevant fields from a Slack interactive message payload.
type slackPayload struct {
	Type        string        `json:"type"`
	User        slackUser     `json:"user"`
	Actions     []slackAction `json:"actions"`
	ResponseURL string        `json:"response_url,omitempty"` // Slack async response URL
}

type slackUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

type slackAction struct {
	ActionID string `json:"action_id"` // e.g., "approve_apr-abc123"
	Value    string `json:"value"`     // "approve" or "deny"
}

func (h *SlackHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Read body for signature verification
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		http.Error(w, `{"error":"read body failed"}`, http.StatusBadRequest)
		return
	}

	// Verify Slack signature
	if h.signingSecret != "" {
		if !h.verifySignature(r, body) {
			slog.Warn("slack callback signature verification failed", "component", "slack")
			http.Error(w, `{"error":"invalid signature"}`, http.StatusForbidden)
			return
		}
	}

	// Parse payload from form-encoded body
	values, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, `{"error":"invalid form body"}`, http.StatusBadRequest)
		return
	}

	payloadJSON := values.Get("payload")
	if payloadJSON == "" {
		http.Error(w, `{"error":"missing payload"}`, http.StatusBadRequest)
		return
	}

	var payload slackPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		http.Error(w, `{"error":"invalid payload JSON"}`, http.StatusBadRequest)
		return
	}

	if len(payload.Actions) == 0 {
		http.Error(w, `{"error":"no actions"}`, http.StatusBadRequest)
		return
	}

	action := payload.Actions[0]
	// Parse action_id: "approve_apr-abc123" or "deny_apr-abc123"
	approvalAction, approvalID := parseActionID(action.ActionID)
	if approvalID == "" || !strings.HasPrefix(approvalID, "apr-") {
		http.Error(w, `{"error":"invalid action_id format"}`, http.StatusBadRequest)
		return
	}

	reviewer := payload.User.Username
	if reviewer == "" {
		reviewer = payload.User.Name
	}
	if reviewer == "" {
		reviewer = "slack-user-" + payload.User.ID
	}

	slog.Info("slack callback received", "component", "slack", "action", approvalAction,
		"approval_id", approvalID, "reviewer", reviewer)

	var statusMsg string
	switch approvalAction {
	case "approve":
		entry, err := h.store.Approve(approvalID, reviewer)
		if err != nil {
			statusMsg = fmt.Sprintf("Failed to approve: %s", err.Error())
		} else {
			statusMsg = fmt.Sprintf("Approved by %s. Tool `%s` will proceed.", reviewer, entry.Tool)
		}
	case "deny":
		entry, err := h.store.Deny(approvalID, reviewer, "denied via Slack")
		if err != nil {
			statusMsg = fmt.Sprintf("Failed to deny: %s", err.Error())
		} else {
			statusMsg = fmt.Sprintf("Denied by %s. Tool `%s` will not proceed.", reviewer, entry.Tool)
		}
	default:
		statusMsg = fmt.Sprintf("Unknown action: %s", approvalAction)
	}

	// Respond to Slack immediately (within 3s timeout)
	responsePayload, _ := json.Marshal(map[string]any{
		"replace_original": true,
		"text":             statusMsg,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write(responsePayload)

	// Also send to response_url for guaranteed delivery (Slack may have timed out)
	if payload.ResponseURL != "" {
		go func() {
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Post(payload.ResponseURL, "application/json", bytes.NewReader(responsePayload))
			if err != nil {
				slog.Warn("slack response_url delivery failed", "component", "slack", "error", err)
				return
			}
			resp.Body.Close()
		}()
	}
}

// parseActionID splits "approve_apr-abc123" into ("approve", "apr-abc123").
func parseActionID(actionID string) (action, id string) {
	if strings.HasPrefix(actionID, "approve_") {
		return "approve", strings.TrimPrefix(actionID, "approve_")
	}
	if strings.HasPrefix(actionID, "deny_") {
		return "deny", strings.TrimPrefix(actionID, "deny_")
	}
	return "", ""
}

// verifySignature checks the Slack request signature (HMAC-SHA256).
// See: https://api.slack.com/authentication/verifying-requests-from-slack
func (h *SlackHandler) verifySignature(r *http.Request, body []byte) bool {
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")
	sig := r.Header.Get("X-Slack-Signature")

	if timestamp == "" || sig == "" {
		return false
	}

	// Reject requests older than 5 minutes (replay protection)
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix()-ts > 300 {
		return false
	}

	// Compute expected signature: v0=HMAC-SHA256(signing_secret, "v0:{timestamp}:{body}")
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, string(body))
	mac := hmac.New(sha256.New, []byte(h.signingSecret))
	mac.Write([]byte(baseString))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}
