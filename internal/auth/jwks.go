package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// JWKSFetcher fetches and caches JSON Web Key Sets from a JWKS endpoint.
// Keys are cached with a TTL; a kid-miss triggers an immediate re-fetch
// to handle key rotation gracefully.
type JWKSFetcher struct {
	jwksURL    string
	httpClient *http.Client

	mu        sync.RWMutex
	keys      map[string]crypto.PublicKey
	fetchedAt time.Time
	ttl       time.Duration
}

// NewJWKSFetcher creates a fetcher for the given JWKS URL.
func NewJWKSFetcher(jwksURL string) *JWKSFetcher {
	return &JWKSFetcher{
		jwksURL: jwksURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		keys: make(map[string]crypto.PublicKey),
		ttl:  1 * time.Hour,
	}
}

// GetKey returns the public key for the given kid. If the kid is not cached
// or the cache is stale, it re-fetches the JWKS. A kid-miss after a fresh
// fetch returns an error.
func (f *JWKSFetcher) GetKey(kid string) (crypto.PublicKey, error) {
	// Fast path: check cache under read lock
	f.mu.RLock()
	if key, ok := f.keys[kid]; ok && time.Since(f.fetchedAt) < f.ttl {
		f.mu.RUnlock()
		return key, nil
	}
	f.mu.RUnlock()

	// Slow path: re-fetch under write lock with double-check
	f.mu.Lock()
	defer f.mu.Unlock()

	// Double-check: another goroutine may have refreshed
	if key, ok := f.keys[kid]; ok && time.Since(f.fetchedAt) < f.ttl {
		return key, nil
	}

	if err := f.fetchKeysLocked(); err != nil {
		return nil, fmt.Errorf("jwks fetch: %w", err)
	}

	key, ok := f.keys[kid]
	if !ok {
		return nil, fmt.Errorf("jwks: kid %q not found", kid)
	}
	return key, nil
}

// KeyCount returns the number of cached keys.
func (f *JWKSFetcher) KeyCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.keys)
}

// fetchKeysLocked fetches and parses the JWKS. Must be called with f.mu held for writing.
// If the fetch fails and we have cached keys, falls back to the stale cache with a warning.
func (f *JWKSFetcher) fetchKeysLocked() error {
	resp, err := f.httpClient.Get(f.jwksURL)
	if err != nil {
		if len(f.keys) > 0 {
			slog.Warn("JWKS fetch failed, using stale cache",
				"component", "jwks", "url", f.jwksURL, "error", err,
				"cache_age", time.Since(f.fetchedAt).Round(time.Second))
			return nil
		}
		return fmt.Errorf("HTTP GET %s: %w", f.jwksURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if len(f.keys) > 0 {
			slog.Warn("JWKS fetch returned non-200, using stale cache",
				"component", "jwks", "url", f.jwksURL, "status", resp.StatusCode,
				"cache_age", time.Since(f.fetchedAt).Round(time.Second))
			return nil
		}
		return fmt.Errorf("HTTP GET %s: status %d", f.jwksURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var jwks struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("parse JWKS: %w", err)
	}

	keys := make(map[string]crypto.PublicKey, len(jwks.Keys))
	for _, raw := range jwks.Keys {
		kid, key, err := parseJWK(raw)
		if err != nil {
			continue // skip unsupported key types
		}
		if kid != "" {
			keys[kid] = key
		}
	}

	f.keys = keys
	f.fetchedAt = time.Now()
	return nil
}

// jwkJSON is the common JWK fields we need to parse.
type jwkJSON struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	// RSA fields
	N string `json:"n"`
	E string `json:"e"`
	// EC fields
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// parseJWK parses a single JWK entry and returns its kid and public key.
// Supports RSA (RS256, RS384, RS512) and EC (ES256, ES384, ES512) keys.
func parseJWK(raw json.RawMessage) (string, crypto.PublicKey, error) {
	var j jwkJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return "", nil, err
	}

	// Skip non-signing keys
	if j.Use != "" && j.Use != "sig" {
		return "", nil, fmt.Errorf("key use %q is not sig", j.Use)
	}

	switch j.Kty {
	case "RSA":
		key, err := parseRSAKey(j)
		return j.Kid, key, err
	case "EC":
		key, err := parseECKey(j)
		return j.Kid, key, err
	default:
		return "", nil, fmt.Errorf("unsupported key type %q", j.Kty)
	}
}

func parseRSAKey(j jwkJSON) (*rsa.PublicKey, error) {
	nb, err := base64URLDecode(j.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eb, err := base64URLDecode(j.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}

	n := new(big.Int).SetBytes(nb)
	e := 0
	for _, b := range eb {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

func parseECKey(j jwkJSON) (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	switch j.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported curve %q", j.Crv)
	}

	xb, err := base64URLDecode(j.X)
	if err != nil {
		return nil, fmt.Errorf("decode x: %w", err)
	}
	yb, err := base64URLDecode(j.Y)
	if err != nil {
		return nil, fmt.Errorf("decode y: %w", err)
	}

	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(xb),
		Y:     new(big.Int).SetBytes(yb),
	}, nil
}

func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
