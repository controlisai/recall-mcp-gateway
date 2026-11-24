package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

// TokenIssuer signs JWTs for token exchange (RFC 8693) and delegation flows.
// Uses RSA-2048 with RS256 algorithm.
type TokenIssuer struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	issuer     string // gateway's issuer URL
	kid        string // key ID
}

// NewTokenIssuer generates a new ephemeral RSA key pair for JWT signing.
// Keys are lost on restart — use NewTokenIssuerFromPEM for persistent keys.
func NewTokenIssuer(issuer string) (*TokenIssuer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate signing key: %w", err)
	}
	return &TokenIssuer{
		privateKey: key,
		publicKey:  &key.PublicKey,
		issuer:     issuer,
		kid:        "gateway-signing-key-1",
	}, nil
}

// NewTokenIssuerFromPEM loads an RSA private key from a PEM file for persistent signing.
// Delegated tokens survive gateway restarts when using a persistent key.
func NewTokenIssuerFromPEM(issuer, pemPath string) (*TokenIssuer, error) {
	data, err := os.ReadFile(pemPath)
	if err != nil {
		return nil, fmt.Errorf("read signing key file: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", pemPath)
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("parse PKCS8 key: %w", parseErr)
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("key in %s is not RSA", pemPath)
		}
	default:
		return nil, fmt.Errorf("unsupported PEM type: %s", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("parse RSA key: %w", err)
	}
	return &TokenIssuer{
		privateKey: key,
		publicKey:  &key.PublicKey,
		issuer:     issuer,
		kid:        "gateway-signing-key-1",
	}, nil
}

// JWKS returns the public key in JWKS format for clients to verify gateway-issued tokens.
func (ti *TokenIssuer) JWKS() map[string]any {
	n := ti.publicKey.N
	e := big.NewInt(int64(ti.publicKey.E))
	return map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"kid": ti.kid,
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(n.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(e.Bytes()),
			},
		},
	}
}

// DelegatedTokenClaims defines the claims for a delegated JWT.
type DelegatedTokenClaims struct {
	Subject  string // original user (from subject_token)
	Actor    string // service acting on behalf (from actor_token)
	Audience string // target audience
	Scopes   string // requested scopes
	TTL      time.Duration
}

// IssueDelegatedToken creates a signed JWT with an act claim for delegation.
func (ti *TokenIssuer) IssueDelegatedToken(claims DelegatedTokenClaims) (string, error) {
	now := time.Now()
	ttl := claims.TTL
	if ttl <= 0 {
		ttl = time.Hour
	}

	payload := map[string]any{
		"iss": ti.issuer,
		"sub": claims.Subject,
		"aud": claims.Audience,
		"iat": now.Unix(),
		"exp": now.Add(ttl).Unix(),
		"act": map[string]any{"sub": claims.Actor},
	}
	if claims.Scopes != "" {
		payload["scope"] = claims.Scopes
	}

	return ti.signJWT(payload)
}

func (ti *TokenIssuer) signJWT(payload map[string]any) (string, error) {
	header := map[string]string{"alg": "RS256", "kid": ti.kid, "typ": "JWT"}

	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(payload)

	segments := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(payloadJSON)

	h := sha256.Sum256([]byte(segments))
	sig, err := rsa.SignPKCS1v15(rand.Reader, ti.privateKey, crypto.SHA256, h[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	return segments + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
