package auth

import (
	"context"
	"encoding/base64"
	"os"
	"testing"
)

func TestNoneProvider(t *testing.T) {
	p := &NoneProvider{}
	if p.Name() != "none" {
		t.Errorf("name = %q", p.Name())
	}
	h := p.Headers(context.Background())
	if h != nil {
		t.Errorf("headers = %v, want nil", h)
	}
}

func TestBearerProvider(t *testing.T) {
	p := &BearerProvider{token: "my-secret-token"}
	if p.Name() != "bearer" {
		t.Errorf("name = %q", p.Name())
	}
	h := p.Headers(context.Background())
	if h["Authorization"] != "Bearer my-secret-token" {
		t.Errorf("auth = %q", h["Authorization"])
	}
}

func TestBasicProvider(t *testing.T) {
	p := NewBasicProvider("admin", "pass123")
	if p.Name() != "basic" {
		t.Errorf("name = %q", p.Name())
	}
	h := p.Headers(context.Background())
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:pass123"))
	if h["Authorization"] != expected {
		t.Errorf("auth = %q, want %q", h["Authorization"], expected)
	}
}

func TestHeadersProvider(t *testing.T) {
	p := &HeadersProvider{headers: map[string]string{
		"X-API-Key":  "key123",
		"X-Tenant":   "acme",
	}}
	if p.Name() != "headers" {
		t.Errorf("name = %q", p.Name())
	}
	h := p.Headers(context.Background())
	if h["X-API-Key"] != "key123" {
		t.Errorf("X-API-Key = %q", h["X-API-Key"])
	}
	if h["X-Tenant"] != "acme" {
		t.Errorf("X-Tenant = %q", h["X-Tenant"])
	}
}

func TestNewProvider_None(t *testing.T) {
	p, err := NewProvider(nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "none" {
		t.Errorf("name = %q", p.Name())
	}

	p, err = NewProvider(&Config{Type: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "none" {
		t.Errorf("name = %q", p.Name())
	}

	p, err = NewProvider(&Config{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "none" {
		t.Errorf("name = %q", p.Name())
	}
}

func TestNewProvider_Bearer(t *testing.T) {
	p, err := NewProvider(&Config{Type: "bearer", Token: "tok123"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "bearer" {
		t.Errorf("name = %q", p.Name())
	}
	h := p.Headers(context.Background())
	if h["Authorization"] != "Bearer tok123" {
		t.Errorf("auth = %q", h["Authorization"])
	}
}

func TestNewProvider_Bearer_RequiresToken(t *testing.T) {
	_, err := NewProvider(&Config{Type: "bearer"})
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestNewProvider_Basic(t *testing.T) {
	p, err := NewProvider(&Config{Type: "basic", Username: "user", Password: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "basic" {
		t.Errorf("name = %q", p.Name())
	}
}

func TestNewProvider_Basic_RequiresUsername(t *testing.T) {
	_, err := NewProvider(&Config{Type: "basic", Password: "pass"})
	if err == nil {
		t.Fatal("expected error for empty username")
	}
}

func TestNewProvider_Headers(t *testing.T) {
	p, err := NewProvider(&Config{Type: "headers", Headers: map[string]string{"X-Key": "val"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "headers" {
		t.Errorf("name = %q", p.Name())
	}
}

func TestNewProvider_Unsupported(t *testing.T) {
	_, err := NewProvider(&Config{Type: "kerberos"})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestExpandEnv(t *testing.T) {
	os.Setenv("TEST_AUTH_TOKEN", "secret123")
	defer os.Unsetenv("TEST_AUTH_TOKEN")

	tests := []struct {
		input string
		want  string
	}{
		{"plain", "plain"},
		{"${TEST_AUTH_TOKEN}", "secret123"},
		{"Bearer ${TEST_AUTH_TOKEN}", "Bearer secret123"},
		{"${NONEXISTENT_VAR}", ""},
		{"pre-${TEST_AUTH_TOKEN}-post", "pre-secret123-post"},
		{"", ""},
		{"no-vars-here", "no-vars-here"},
	}
	for _, tt := range tests {
		got := ExpandEnv(tt.input)
		if got != tt.want {
			t.Errorf("ExpandEnv(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNewProvider_Bearer_EnvExpansion(t *testing.T) {
	os.Setenv("TEST_BEARER_TOKEN", "env-token-value")
	defer os.Unsetenv("TEST_BEARER_TOKEN")

	p, err := NewProvider(&Config{Type: "bearer", Token: "${TEST_BEARER_TOKEN}"})
	if err != nil {
		t.Fatal(err)
	}
	h := p.Headers(context.Background())
	if h["Authorization"] != "Bearer env-token-value" {
		t.Errorf("auth = %q, want Bearer env-token-value", h["Authorization"])
	}
}

func TestNewProvider_Basic_EnvExpansion(t *testing.T) {
	os.Setenv("TEST_DB_PASS", "s3cret")
	defer os.Unsetenv("TEST_DB_PASS")

	p, err := NewProvider(&Config{Type: "basic", Username: "admin", Password: "${TEST_DB_PASS}"})
	if err != nil {
		t.Fatal(err)
	}
	h := p.Headers(context.Background())
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:s3cret"))
	if h["Authorization"] != expected {
		t.Errorf("auth = %q, want %q", h["Authorization"], expected)
	}
}

func TestNewProvider_Headers_EnvExpansion(t *testing.T) {
	os.Setenv("TEST_API_KEY", "key-from-env")
	defer os.Unsetenv("TEST_API_KEY")

	p, err := NewProvider(&Config{Type: "headers", Headers: map[string]string{
		"X-API-Key": "${TEST_API_KEY}",
		"X-Static":  "literal-value",
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := p.Headers(context.Background())
	if h["X-API-Key"] != "key-from-env" {
		t.Errorf("X-API-Key = %q", h["X-API-Key"])
	}
	if h["X-Static"] != "literal-value" {
		t.Errorf("X-Static = %q", h["X-Static"])
	}
}
