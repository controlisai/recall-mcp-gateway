package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJWKSFetcher_RSAKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	jwks := buildJWKS(rsaJWK("test-rsa-key", &key.PublicKey))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwks)
	}))
	defer srv.Close()

	f := NewJWKSFetcher(srv.URL)
	pub, err := f.GetKey("test-rsa-key")
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("expected *rsa.PublicKey, got %T", pub)
	}
	if rsaPub.N.Cmp(key.PublicKey.N) != 0 {
		t.Error("RSA N mismatch")
	}
}

func TestJWKSFetcher_ECKey(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	jwks := buildJWKS(ecJWK("test-ec-key", &key.PublicKey))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwks)
	}))
	defer srv.Close()

	f := NewJWKSFetcher(srv.URL)
	pub, err := f.GetKey("test-ec-key")
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("expected *ecdsa.PublicKey, got %T", pub)
	}
	if ecPub.X.Cmp(key.PublicKey.X) != 0 {
		t.Error("EC X mismatch")
	}
}

func TestJWKSFetcher_KidNotFound(t *testing.T) {
	jwks := buildJWKS() // empty keys array
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwks)
	}))
	defer srv.Close()

	f := NewJWKSFetcher(srv.URL)
	_, err := f.GetKey("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing kid")
	}
}

func TestJWKSFetcher_RefreshOnKidMiss(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	fetchCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/json")
		if fetchCount >= 2 {
			// Second fetch includes the new key (simulates rotation)
			w.Write(buildJWKS(rsaJWK("rotated-key", &key.PublicKey)))
		} else {
			w.Write(buildJWKS()) // First fetch: empty
		}
	}))
	defer srv.Close()

	f := NewJWKSFetcher(srv.URL)

	// First attempt: kid-miss triggers re-fetch, still not found
	_, err := f.GetKey("rotated-key")
	if err == nil {
		t.Fatal("expected error on first attempt")
	}

	// Force cache to be stale so next GetKey re-fetches
	f.mu.Lock()
	f.fetchedAt = f.fetchedAt.Add(-2 * f.ttl)
	f.mu.Unlock()

	// Second attempt: should find the rotated key
	_, err = f.GetKey("rotated-key")
	if err != nil {
		t.Fatalf("expected rotated key on second attempt: %v", err)
	}
}

func TestJWKSFetcher_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewJWKSFetcher(srv.URL)
	_, err := f.GetKey("any")
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestJWKSFetcher_KeyCount(t *testing.T) {
	key1, _ := rsa.GenerateKey(rand.Reader, 2048)
	key2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	jwks := buildJWKS(rsaJWK("k1", &key1.PublicKey), ecJWK("k2", &key2.PublicKey))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(jwks)
	}))
	defer srv.Close()

	f := NewJWKSFetcher(srv.URL)
	f.GetKey("k1") // trigger fetch
	if f.KeyCount() != 2 {
		t.Errorf("expected 2 keys, got %d", f.KeyCount())
	}
}

// --- test helpers ---

func rsaJWK(kid string, pub *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA",
		"kid": kid,
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func ecJWK(kid string, pub *ecdsa.PublicKey) map[string]string {
	byteLen := (pub.Curve.Params().BitSize + 7) / 8
	xBytes := pub.X.Bytes()
	yBytes := pub.Y.Bytes()
	// Pad to full length
	xPadded := make([]byte, byteLen)
	yPadded := make([]byte, byteLen)
	copy(xPadded[byteLen-len(xBytes):], xBytes)
	copy(yPadded[byteLen-len(yBytes):], yBytes)

	return map[string]string{
		"kty": "EC",
		"kid": kid,
		"use": "sig",
		"alg": "ES256",
		"crv": "P-256",
		"x":   base64.RawURLEncoding.EncodeToString(xPadded),
		"y":   base64.RawURLEncoding.EncodeToString(yPadded),
	}
}

func buildJWKS(keys ...map[string]string) []byte {
	jwks := struct {
		Keys []map[string]string `json:"keys"`
	}{Keys: keys}
	if jwks.Keys == nil {
		jwks.Keys = []map[string]string{}
	}
	b, _ := json.Marshal(jwks)
	return b
}
