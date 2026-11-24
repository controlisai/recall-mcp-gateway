package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testKeyPair holds a key pair and JWKS server for JWT tests.
type testKeyPair struct {
	rsaKey  *rsa.PrivateKey
	kid     string
	srv     *httptest.Server
	fetcher *JWKSFetcher
}

func newTestKeyPair(t *testing.T) *testKeyPair {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"
	jwks := buildJWKS(rsaJWK(kid, &key.PublicKey))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwks)
	}))
	t.Cleanup(srv.Close)

	return &testKeyPair{
		rsaKey:  key,
		kid:     kid,
		srv:     srv,
		fetcher: NewJWKSFetcher(srv.URL),
	}
}

func (kp *testKeyPair) signJWT(header, payload string) string {
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(header))
	payloadB64 := base64.RawURLEncoding.EncodeToString([]byte(payload))
	signed := headerB64 + "." + payloadB64

	h := sha256.Sum256([]byte(signed))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, kp.rsaKey, crypto.SHA256, h[:])
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return signed + "." + sigB64
}

func (kp *testKeyPair) buildToken(claims map[string]any) string {
	header := `{"alg":"RS256","kid":"` + kp.kid + `","typ":"JWT"}`
	payload, _ := json.Marshal(claims)
	return kp.signJWT(header, string(payload))
}

func TestJWTValidator_ValidToken(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub": "alice@example.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
		"iat": float64(time.Now().Unix()),
	})

	claims, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.Subject != "alice@example.com" {
		t.Errorf("subject = %q, want alice@example.com", claims.Subject)
	}
	if claims.Issuer != "https://auth.example.com" {
		t.Errorf("issuer = %q, want https://auth.example.com", claims.Issuer)
	}
}

func TestJWTValidator_ExpiredToken(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub": "alice@example.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(-5 * time.Minute).Unix()), // expired 5 min ago
	})

	_, err := v.Validate(token)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestJWTValidator_WrongAudience(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub": "alice@example.com",
		"iss": "https://auth.example.com",
		"aud": "https://other-service.example.com", // wrong audience
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	_, err := v.Validate(token)
	if !errors.Is(err, ErrInvalidAudience) {
		t.Fatalf("expected ErrInvalidAudience, got %v", err)
	}
}

func TestJWTValidator_WrongIssuer(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub": "alice@example.com",
		"iss": "https://evil.example.com", // wrong issuer
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	_, err := v.Validate(token)
	if !errors.Is(err, ErrInvalidIssuer) {
		t.Fatalf("expected ErrInvalidIssuer, got %v", err)
	}
}

func TestJWTValidator_BadSignature(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub": "alice@example.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	// Tamper with the signature
	parts := strings.Split(token, ".")
	parts[2] = "dGFtcGVyZWQ" // "tampered" in base64url
	tampered := strings.Join(parts, ".")

	_, err := v.Validate(tampered)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestJWTValidator_MalformedToken(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"no dots", "justonepart"},
		{"one dot", "two.parts"},
		{"four parts", "a.b.c.d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.Validate(tt.token)
			if !errors.Is(err, ErrMalformedToken) {
				t.Errorf("expected ErrMalformedToken, got %v", err)
			}
		})
	}
}

func TestJWTValidator_AudienceAsArray(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub": "alice@example.com",
		"iss": "https://auth.example.com",
		"aud": []string{"https://other.example.com", "https://gw.example.com/mcp"}, // array with target
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	claims, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(claims.Audience) != 2 {
		t.Errorf("expected 2 audiences, got %d", len(claims.Audience))
	}
}

func TestJWTValidator_SubExtraction(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub":       "user-123",
		"client_id": "service-abc",
		"iss":       "https://auth.example.com",
		"aud":       "https://gw.example.com/mcp",
		"exp":       float64(time.Now().Add(1 * time.Hour).Unix()),
		"scope":     "mcp:read mcp:write",
	})

	claims, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Errorf("subject = %q, want user-123", claims.Subject)
	}
	if claims.ClientID != "service-abc" {
		t.Errorf("clientID = %q, want service-abc", claims.ClientID)
	}
	if len(claims.Scopes) != 2 || claims.Scopes[0] != "mcp:read" {
		t.Errorf("scopes = %v, want [mcp:read mcp:write]", claims.Scopes)
	}
}

func TestJWTValidator_ActClaim(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub": "alice@bigcorp.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
		"act": map[string]any{
			"sub": "payment-agent",
		},
	})

	claims, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.Actor == nil {
		t.Fatal("expected act claim, got nil")
	}
	if claims.Actor.Subject != "payment-agent" {
		t.Errorf("actor.sub = %q, want payment-agent", claims.Actor.Subject)
	}
	if claims.DelegationDepth() != 1 {
		t.Errorf("delegation depth = %d, want 1", claims.DelegationDepth())
	}
}

func TestJWTValidator_NestedActChain(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub": "alice@bigcorp.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
		"act": map[string]any{
			"sub": "orchestrator-svc",
			"act": map[string]any{
				"sub": "original-caller",
			},
		},
	})

	claims, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.DelegationDepth() != 2 {
		t.Errorf("delegation depth = %d, want 2", claims.DelegationDepth())
	}
	if claims.Actor.Actor.Subject != "original-caller" {
		t.Errorf("nested actor = %q, want original-caller", claims.Actor.Actor.Subject)
	}
}

func TestJWTValidator_OpaqueTokenRejected(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	// An opaque token (not a JWT) should fail as malformed
	_, err := v.Validate("a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6")
	if !errors.Is(err, ErrMalformedToken) {
		t.Fatalf("expected ErrMalformedToken for opaque token, got %v", err)
	}
}

func TestJWTValidator_ES256(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	kid := "test-ec-key"
	jwks := buildJWKS(ecJWK(kid, &ecKey.PublicKey))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwks)
	}))
	defer srv.Close()

	fetcher := NewJWKSFetcher(srv.URL)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", fetcher)

	// Build ES256 token
	header := `{"alg":"ES256","kid":"` + kid + `","typ":"JWT"}`
	payload, _ := json.Marshal(map[string]any{
		"sub": "bob@example.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(header))
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	signed := headerB64 + "." + payloadB64

	h := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, ecKey, h[:])
	if err != nil {
		t.Fatal(err)
	}

	// EC signature: r||s padded to 32 bytes each for P-256
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	sig := make([]byte, 64)
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):], sBytes)

	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	token := signed + "." + sigB64

	claims, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate ES256: %v", err)
	}
	if claims.Subject != "bob@example.com" {
		t.Errorf("subject = %q, want bob@example.com", claims.Subject)
	}
}

func TestJWTValidator_ClockSkewTolerance(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	// Token expired 10 seconds ago — within 30s clock skew tolerance
	token := kp.buildToken(map[string]any{
		"sub": "alice@example.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(-10 * time.Second).Unix()),
	})

	_, err := v.Validate(token)
	if err != nil {
		t.Fatalf("expected clock skew tolerance to accept recently-expired token, got %v", err)
	}
}

func TestJWTValidator_NotBeforeFuture(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub": "alice@example.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
		"nbf": float64(time.Now().Add(5 * time.Minute).Unix()), // 5 min in the future
	})

	_, err := v.Validate(token)
	if !errors.Is(err, ErrTokenNotYetValid) {
		t.Fatalf("expected ErrTokenNotYetValid, got %v", err)
	}
}

func TestJWTValidator_NotBeforeWithinSkew(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	// nbf 10 seconds in the future — within 30s clock skew tolerance
	token := kp.buildToken(map[string]any{
		"sub": "alice@example.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
		"nbf": float64(time.Now().Add(10 * time.Second).Unix()),
	})

	_, err := v.Validate(token)
	if err != nil {
		t.Fatalf("expected clock skew tolerance to accept slightly-future nbf, got %v", err)
	}
}

func TestJWTValidator_MayActAuthorized(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub":     "alice@bigcorp.com",
		"iss":     "https://auth.example.com",
		"aud":     "https://gw.example.com/mcp",
		"exp":     float64(time.Now().Add(1 * time.Hour).Unix()),
		"act":     map[string]any{"sub": "payment-agent"},
		"may_act": map[string]any{"sub": "payment-agent"},
	})

	claims, err := v.Validate(token)
	if err != nil {
		t.Fatalf("authorized may_act should pass: %v", err)
	}
	if claims.MayAct == nil {
		t.Fatal("expected may_act claim")
	}
	if claims.MayAct.Subject != "payment-agent" {
		t.Errorf("may_act.sub = %q, want payment-agent", claims.MayAct.Subject)
	}
}

func TestJWTValidator_MayActUnauthorized(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)

	token := kp.buildToken(map[string]any{
		"sub":     "alice@bigcorp.com",
		"iss":     "https://auth.example.com",
		"aud":     "https://gw.example.com/mcp",
		"exp":     float64(time.Now().Add(1 * time.Hour).Unix()),
		"act":     map[string]any{"sub": "evil-agent"},
		"may_act": map[string]any{"sub": "payment-agent"}, // only payment-agent authorized
	})

	_, err := v.Validate(token)
	if !errors.Is(err, ErrDelegationUnauthorized) {
		t.Fatalf("expected ErrDelegationUnauthorized, got %v", err)
	}
}

func TestJWTValidator_DelegationDepthLimit(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)
	v.SetMaxDelegationDepth(1)

	// Depth 2 — exceeds limit of 1
	token := kp.buildToken(map[string]any{
		"sub": "alice@bigcorp.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
		"act": map[string]any{
			"sub": "orchestrator",
			"act": map[string]any{"sub": "original-caller"},
		},
	})

	_, err := v.Validate(token)
	if !errors.Is(err, ErrDelegationTooDeep) {
		t.Fatalf("expected ErrDelegationTooDeep, got %v", err)
	}
}

func TestJWTValidator_DelegationDepthWithinLimit(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)
	v.SetMaxDelegationDepth(2)

	// Depth 1 — within limit of 2
	token := kp.buildToken(map[string]any{
		"sub": "alice@bigcorp.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
		"act": map[string]any{"sub": "payment-agent"},
	})

	claims, err := v.Validate(token)
	if err != nil {
		t.Fatalf("depth 1 within limit 2 should pass: %v", err)
	}
	if claims.DelegationDepth() != 1 {
		t.Errorf("depth = %d, want 1", claims.DelegationDepth())
	}
}

func TestJWTValidator_DelegationUnlimited(t *testing.T) {
	kp := newTestKeyPair(t)
	v := NewJWTValidator("https://auth.example.com", "https://gw.example.com/mcp", kp.fetcher)
	// maxDelegationDepth = 0 (default) = unlimited

	token := kp.buildToken(map[string]any{
		"sub": "alice@bigcorp.com",
		"iss": "https://auth.example.com",
		"aud": "https://gw.example.com/mcp",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
		"act": map[string]any{
			"sub": "a",
			"act": map[string]any{
				"sub": "b",
				"act": map[string]any{"sub": "c"},
			},
		},
	})

	claims, err := v.Validate(token)
	if err != nil {
		t.Fatalf("unlimited depth should accept deep chains: %v", err)
	}
	if claims.DelegationDepth() != 3 {
		t.Errorf("depth = %d, want 3", claims.DelegationDepth())
	}
}
