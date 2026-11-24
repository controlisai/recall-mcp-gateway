package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTokenIssuer_IssueDelegatedToken(t *testing.T) {
	issuer, err := NewTokenIssuer("http://gateway:8090")
	if err != nil {
		t.Fatalf("create issuer: %v", err)
	}

	token, err := issuer.IssueDelegatedToken(DelegatedTokenClaims{
		Subject:  "alice@example.com",
		Actor:    "bob-service",
		Audience: "http://gateway:8090/mcp",
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}

	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	json.Unmarshal(payload, &claims)

	if claims["sub"] != "alice@example.com" {
		t.Errorf("sub = %v, want alice@example.com", claims["sub"])
	}
	if claims["iss"] != "http://gateway:8090" {
		t.Errorf("iss = %v, want http://gateway:8090", claims["iss"])
	}
	act, ok := claims["act"].(map[string]any)
	if !ok || act["sub"] != "bob-service" {
		t.Errorf("act.sub = %v, want bob-service", claims["act"])
	}
}

func TestTokenIssuer_JWKS(t *testing.T) {
	issuer, _ := NewTokenIssuer("http://gateway:8090")
	jwks := issuer.JWKS()
	keys, ok := jwks["keys"].([]map[string]any)
	if !ok || len(keys) != 1 {
		t.Fatal("expected 1 key in JWKS")
	}
	if keys[0]["alg"] != "RS256" {
		t.Errorf("alg = %v, want RS256", keys[0]["alg"])
	}
}
