package proxy

import (
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
)

// Governance error codes returned to clients via _meta.recall:errorCode.
// Error messages are intentionally generic to avoid leaking internal details.
const (
	ErrCodeUnauthorized    = "UNAUTHORIZED"
	ErrCodeAccessDenied    = "ACCESS_DENIED"
	ErrCodeToolNotFound    = "TOOL_NOT_FOUND"
	ErrCodeRateLimited     = "RATE_LIMITED"
	ErrCodePendingApproval = "PENDING_APPROVAL"
	ErrCodeDenied          = "DENIED"
	ErrCodeExpired         = "EXPIRED"
	ErrCodeConsentRequired = "CONSENT_REQUIRED"
	ErrCodeServerError     = "SERVER_ERROR"
	ErrCodeBlocked         = "BLOCKED"
)

// Generic client-facing messages (no internal details).
var errMessages = map[string]string{
	ErrCodeUnauthorized:    "[Recall] Unauthorized",
	ErrCodeAccessDenied:    "[Recall] Access denied",
	ErrCodeToolNotFound:    "[Recall] Tool not available",
	ErrCodeRateLimited:     "[Recall] Rate limit exceeded",
	ErrCodePendingApproval: "[Recall] Pending approval",
	ErrCodeDenied:          "[Recall] Request denied",
	ErrCodeExpired:         "[Recall] Request expired",
	ErrCodeConsentRequired: "[Recall] Consent required",
	ErrCodeServerError:     "[Recall] Server error",
	ErrCodeBlocked:         "[Recall] Blocked by policy",
}

// governanceError creates a sanitized MCP tool error with structured metadata.
// The generic message is returned to the client; detailed context is logged server-side.
func governanceError(code string, requestID string, detailFmt string, args ...any) *mcp.CallToolResult {
	msg := errMessages[code]
	if msg == "" {
		msg = "[Recall] Error"
	}

	// Log detailed context server-side (never sent to client)
	detail := fmt.Sprintf(detailFmt, args...)
	if detail != "" {
		slog.Debug("governance error detail", "component", "proxy", "code", code, "requestID", requestID, "detail", detail)
	}

	result := mcp.NewToolResultError(msg)
	result.Meta = &mcp.Meta{}
	result.Meta.SetMetaField("recall:errorCode", code)
	if requestID != "" {
		result.Meta.SetMetaField("recall:requestId", requestID)
	}
	return result
}

// hasRequiredScopes checks if the token's scopes include all required scopes.
func hasRequiredScopes(tokenScopes, required []string) bool {
	have := make(map[string]bool, len(tokenScopes))
	for _, s := range tokenScopes {
		have[s] = true
	}
	for _, r := range required {
		if !have[r] {
			return false
		}
	}
	return true
}
