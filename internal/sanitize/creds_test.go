package sanitize

import "testing"

func TestError_BearerToken(t *testing.T) {
	input := "request failed: Bearer abc123def456 is invalid"
	got := Error(input)
	if got != "request failed: Bearer **** is invalid" {
		t.Errorf("got %q", got)
	}
}

func TestError_QueryParams(t *testing.T) {
	input := "GET http://engine:8084/decide?api_key=secret123&other=ok"
	got := Error(input)
	if got == input {
		t.Errorf("expected redaction, got %q", got)
	}
	if contains(got, "secret123") {
		t.Errorf("api_key not redacted: %q", got)
	}
}

func TestError_RecallKeys(t *testing.T) {
	input := "auth failed for cai_rcl_sdk_prod_abc123"
	got := Error(input)
	if contains(got, "abc123") {
		t.Errorf("recall key not redacted: %q", got)
	}
}

func TestError_ProKeys(t *testing.T) {
	input := "license rp_prod_mykey123 expired"
	got := Error(input)
	if contains(got, "mykey123") {
		t.Errorf("pro key not redacted: %q", got)
	}
}

func TestError_AuthHeader(t *testing.T) {
	input := "header Authorization: Bearer xyz789 rejected"
	got := Error(input)
	if contains(got, "xyz789") {
		t.Errorf("auth header not redacted: %q", got)
	}
}

func TestError_NoFalsePositive(t *testing.T) {
	input := "tool query returned 42 rows"
	got := Error(input)
	if got != input {
		t.Errorf("false positive: %q -> %q", input, got)
	}
}

func TestURL_RedactsParams(t *testing.T) {
	input := "http://engine:8084/decide?token=abc123&format=json"
	got := URL(input)
	if contains(got, "abc123") {
		t.Errorf("token not redacted: %q", got)
	}
	if !contains(got, "format=json") {
		t.Errorf("non-sensitive param was redacted: %q", got)
	}
}

func TestURL_NoQuery(t *testing.T) {
	input := "http://engine:8084/decide"
	got := URL(input)
	if got != input {
		t.Errorf("URL without query changed: %q -> %q", input, got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && len(sub) > 0 && containsStr(s, sub)))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
