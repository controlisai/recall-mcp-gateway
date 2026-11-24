package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOAuthClientCredentials_FetchToken(t *testing.T) {
	srv := mockTokenServer(t, "test-token-123", 3600)
	defer srv.Close()

	p := NewOAuthClientCredentialsProvider(OAuthClientCredentialsConfig{
		TokenURL:     srv.URL,
		ClientID:     "client1",
		ClientSecret: "secret1",
	})

	h := p.Headers(context.Background())
	if h == nil {
		t.Fatal("headers nil")
	}
	if h["Authorization"] != "Bearer test-token-123" {
		t.Errorf("auth = %q", h["Authorization"])
	}
}

func TestOAuthClientCredentials_CachesToken(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: "cached-token",
			ExpiresIn:   3600,
		})
	}))
	defer srv.Close()

	p := NewOAuthClientCredentialsProvider(OAuthClientCredentialsConfig{
		TokenURL:     srv.URL,
		ClientID:     "c",
		ClientSecret: "s",
	})

	// Call 3 times — should only hit the server once
	for i := 0; i < 3; i++ {
		h := p.Headers(context.Background())
		if h["Authorization"] != "Bearer cached-token" {
			t.Fatalf("call %d: auth = %q", i, h["Authorization"])
		}
	}

	if callCount.Load() != 1 {
		t.Errorf("token endpoint called %d times, want 1", callCount.Load())
	}
}

func TestOAuthClientCredentials_RefreshExpired(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: "token-" + string(rune('0'+n)),
			ExpiresIn:   1, // expires in 1 second
		})
	}))
	defer srv.Close()

	p := NewOAuthClientCredentialsProvider(OAuthClientCredentialsConfig{
		TokenURL:     srv.URL,
		ClientID:     "c",
		ClientSecret: "s",
	})

	// First call
	h := p.Headers(context.Background())
	if h == nil {
		t.Fatal("first call: nil headers")
	}

	// Force expiry (token expires in 1s, buffer is 30s, so it's already "expired")
	// The 30s buffer means a 1s token is immediately considered expired on second call
	h2 := p.Headers(context.Background())
	if h2 == nil {
		t.Fatal("second call: nil headers")
	}

	if callCount.Load() != 2 {
		t.Errorf("token endpoint called %d times, want 2", callCount.Load())
	}
}

func TestOAuthClientCredentials_Scopes(t *testing.T) {
	var receivedScope string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		receivedScope = r.FormValue("scope")
		json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok", ExpiresIn: 3600})
	}))
	defer srv.Close()

	p := NewOAuthClientCredentialsProvider(OAuthClientCredentialsConfig{
		TokenURL:     srv.URL,
		ClientID:     "c",
		ClientSecret: "s",
		Scopes:       []string{"read", "write", "admin"},
	})

	p.Headers(context.Background())
	if receivedScope != "read write admin" {
		t.Errorf("scope = %q, want 'read write admin'", receivedScope)
	}
}

func TestOAuthClientCredentials_VerifiesCredentials(t *testing.T) {
	var receivedClientID, receivedClientSecret, receivedGrantType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		receivedGrantType = r.FormValue("grant_type")
		receivedClientID = r.FormValue("client_id")
		receivedClientSecret = r.FormValue("client_secret")
		json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok", ExpiresIn: 3600})
	}))
	defer srv.Close()

	p := NewOAuthClientCredentialsProvider(OAuthClientCredentialsConfig{
		TokenURL:     srv.URL,
		ClientID:     "my-client",
		ClientSecret: "my-secret",
	})

	p.Headers(context.Background())
	if receivedGrantType != "client_credentials" {
		t.Errorf("grant_type = %q", receivedGrantType)
	}
	if receivedClientID != "my-client" {
		t.Errorf("client_id = %q", receivedClientID)
	}
	if receivedClientSecret != "my-secret" {
		t.Errorf("client_secret = %q", receivedClientSecret)
	}
}

func TestOAuthClientCredentials_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer srv.Close()

	p := NewOAuthClientCredentialsProvider(OAuthClientCredentialsConfig{
		TokenURL:     srv.URL,
		ClientID:     "bad",
		ClientSecret: "bad",
	})

	h := p.Headers(context.Background())
	if h != nil {
		t.Errorf("expected nil headers on error, got %v", h)
	}
}

func TestOAuthClientCredentials_ConcurrentAccess(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		time.Sleep(50 * time.Millisecond) // simulate slow token endpoint
		json.NewEncoder(w).Encode(tokenResponse{AccessToken: "concurrent-token", ExpiresIn: 3600})
	}))
	defer srv.Close()

	p := NewOAuthClientCredentialsProvider(OAuthClientCredentialsConfig{
		TokenURL:     srv.URL,
		ClientID:     "c",
		ClientSecret: "s",
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := p.Headers(context.Background())
			if h["Authorization"] != "Bearer concurrent-token" {
				t.Errorf("auth = %q", h["Authorization"])
			}
		}()
	}
	wg.Wait()

	// Double-check locking should prevent more than ~2 calls (first + one that waited)
	if callCount.Load() > 3 {
		t.Errorf("token endpoint called %d times (expected <=3 with double-check locking)", callCount.Load())
	}
}

func TestNewProvider_OAuthClientCredentials(t *testing.T) {
	srv := mockTokenServer(t, "factory-token", 3600)
	defer srv.Close()

	p, err := NewProvider(&Config{
		Type:         "oauth_client_credentials",
		TokenURL:     srv.URL,
		ClientID:     "c",
		ClientSecret: "s",
		Scopes:       []string{"api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "oauth_client_credentials" {
		t.Errorf("name = %q", p.Name())
	}

	h := p.Headers(context.Background())
	if h["Authorization"] != "Bearer factory-token" {
		t.Errorf("auth = %q", h["Authorization"])
	}
}

func TestNewProvider_OAuthClientCredentials_RequiredFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"missing token_url", Config{Type: "oauth_client_credentials", ClientID: "c", ClientSecret: "s"}},
		{"missing client_id", Config{Type: "oauth_client_credentials", TokenURL: "http://x", ClientSecret: "s"}},
		{"missing client_secret", Config{Type: "oauth_client_credentials", TokenURL: "http://x", ClientID: "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewProvider(&tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestExtractJWTIssuer(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{"JWT with iss", "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiJodHRwczovL2F1dGguZXhhbXBsZS5jb20ifQ.sig", "https://auth.example.com"},
		{"opaque token", "abc123def456", ""},
		{"JWT no iss", "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ1c2VyMSJ9.sig", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJWTIssuer(tt.token)
			if got != tt.want {
				t.Errorf("extractJWTIssuer(%q) = %q, want %q", tt.token, got, tt.want)
			}
		})
	}
}

func TestOAuthClientCredentials_IssValidation(t *testing.T) {
	// Build a fake JWT access token with iss claim
	// header: {"alg":"RS256"} → base64url: eyJhbGciOiJSUzI1NiJ9
	// payload: {"iss":"https://auth.example.com","sub":"svc"} → base64url
	header := "eyJhbGciOiJSUzI1NiJ9"
	payload := "eyJpc3MiOiJodHRwczovL2F1dGguZXhhbXBsZS5jb20iLCJzdWIiOiJzdmMifQ"
	jwtToken := header + "." + payload + ".fake-sig"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tokenResponse{AccessToken: jwtToken, ExpiresIn: 3600})
	}))
	defer srv.Close()

	// Matching issuer — should succeed without warning
	p := NewOAuthClientCredentialsProvider(OAuthClientCredentialsConfig{
		TokenURL:       srv.URL,
		ClientID:       "c",
		ClientSecret:   "s",
		ExpectedIssuer: "https://auth.example.com",
	})
	h := p.Headers(context.Background())
	if h == nil {
		t.Fatal("expected headers")
	}
	if h["Authorization"] != "Bearer "+jwtToken {
		t.Errorf("token not returned correctly")
	}
}

func TestOAuthClientCredentials_OpaqueTokenNoIssCheck(t *testing.T) {
	// Opaque token (not JWT) — iss check should be silently skipped
	srv := mockTokenServer(t, "opaque-token-no-dots", 3600)
	defer srv.Close()

	p := NewOAuthClientCredentialsProvider(OAuthClientCredentialsConfig{
		TokenURL:       srv.URL,
		ClientID:       "c",
		ClientSecret:   "s",
		ExpectedIssuer: "https://auth.example.com", // won't fire for opaque tokens
	})
	h := p.Headers(context.Background())
	if h == nil {
		t.Fatal("expected headers for opaque token")
	}
}

func mockTokenServer(t *testing.T, token string, expiresIn int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", ct)
		}
		r.ParseForm()
		if r.FormValue("grant_type") != "client_credentials" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "unsupported_grant_type"})
			return
		}
		json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: token,
			TokenType:   "Bearer",
			ExpiresIn:   expiresIn,
		})
	}))
}
