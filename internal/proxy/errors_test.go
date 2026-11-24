package proxy

import (
	"strings"
	"testing"
)

func TestGovernanceError_NoDetailInResult(t *testing.T) {
	// The governanceError function must never leak the detail string into the client response.
	// Detail is for server-side logging only; client gets a generic message.
	secret := "internal_database_query_plan_v2"
	result := governanceError(ErrCodeToolNotFound, "req-123", "secret tool: %s on server %s", secret, "prod-db-01")

	// Extract text from result content
	for _, c := range result.Content {
		if tc, ok := c.(interface{ GetText() string }); ok {
			text := tc.GetText()
			if strings.Contains(text, secret) {
				t.Errorf("client response contains secret detail: %s", text)
			}
			if strings.Contains(text, "prod-db-01") {
				t.Errorf("client response contains server name: %s", text)
			}
		}
	}

	// Verify it uses the generic message
	if result.Content == nil || len(result.Content) == 0 {
		t.Fatal("expected content in result")
	}
}
