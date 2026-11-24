package upstream

import (
	"errors"
	"testing"
)

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{"nil", nil, false},
		{"401 unauthorized", errors.New("HTTP 401: unauthorized"), true},
		{"403 forbidden", errors.New("HTTP 403: forbidden"), true},
		{"401 in transport", errors.New("transport error: server returned 401"), true},
		{"403 in response", errors.New("response status 403"), true},
		{"invalid credentials", errors.New("invalid credentials"), true},
		{"authentication failed", errors.New("authentication failed"), true},
		{"not auth error", errors.New("connection refused"), false},
		{"tool error", errors.New("invalid arguments"), false},
		{"empty", errors.New(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAuthError(tt.err)
			if got != tt.expect {
				t.Errorf("isAuthError(%q) = %v, want %v", tt.err, got, tt.expect)
			}
		})
	}
}

func TestIsSessionError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{"nil", nil, false},
		{"session terminated", errors.New("transport error: session terminated (404)"), true},
		{"missing session ID", errors.New("Bad Request: Missing session ID"), true},
		{"session not found", errors.New("session not found"), true},
		{"session expired", errors.New("session expired"), true},
		{"connection refused", errors.New("dial tcp [::1]:3004: connect: connection refused"), true},
		{"connection reset", errors.New("read: connection reset by peer"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"eof", errors.New("unexpected EOF"), true},
		{"closed connection", errors.New("use of closed network connection"), true},
		{"transport error", errors.New("transport error: failed to send request"), true},
		{"need to re-initialize", errors.New("need to re-initialize"), true},
		{"i/o timeout", errors.New("i/o timeout"), true},

		// NOT session errors — application-level errors
		{"tool not found", errors.New("tool 'foo' not found"), false},
		{"invalid arguments", errors.New("invalid arguments: missing 'path'"), false},
		{"permission denied", errors.New("permission denied"), false},
		{"tool error", errors.New("database query failed: syntax error"), false},
		{"empty error", errors.New(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSessionError(tt.err)
			if got != tt.expect {
				t.Errorf("isSessionError(%q) = %v, want %v", tt.err, got, tt.expect)
			}
		})
	}
}
