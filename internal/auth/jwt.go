package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"strings"
	"time"
)

// JWT validation errors.
var (
	ErrMalformedToken  = errors.New("malformed token")
	ErrInvalidSignature = errors.New("invalid signature")
	ErrTokenExpired    = errors.New("token expired")
	ErrTokenNotYetValid = errors.New("token not yet valid")
	ErrInvalidIssuer   = errors.New("invalid issuer")
	ErrInvalidAudience = errors.New("invalid audience")
	ErrUnsupportedAlg      = errors.New("unsupported algorithm")
	ErrDelegationUnauthorized = errors.New("delegation not authorized by may_act")
	ErrDelegationTooDeep   = errors.New("delegation chain exceeds max depth")
)

const clockSkew = 30 * time.Second
const maxActDepth = 5

// JWTClaims holds the validated claims extracted from a JWT.
type JWTClaims struct {
	Subject   string      // "sub" claim
	Issuer    string      // "iss" claim
	Audience  []string    // "aud" claim (normalized to slice)
	Expiry    time.Time   // "exp" claim
	NotBefore time.Time   // "nbf" claim
	IssuedAt  time.Time   // "iat" claim
	Actor     *ActorClaim // "act" claim (optional, for active delegation)
	MayAct    *ActorClaim // "may_act" claim (optional, future authorization for delegation)
	ClientID  string      // "client_id" or "azp" claim (for M2M)
	Scopes    []string    // "scope" claim (space-separated string → slice)
	ID        string      // "jti" claim (JWT ID, for replay prevention)
}

// ActorClaim represents a delegation actor from the JWT "act" claim (RFC 8693).
type ActorClaim struct {
	Subject string      // "sub" within the act object
	Issuer  string      // "iss" within the act object (optional)
	Actor   *ActorClaim // nested act (delegation chain)
}

// DelegationDepth returns the nesting depth of the delegation chain.
// 0 = no delegation, 1 = single delegation, etc.
func (c *JWTClaims) DelegationDepth() int {
	depth := 0
	act := c.Actor
	for act != nil {
		depth++
		act = act.Actor
	}
	return depth
}

// RevocationChecker is the interface for token revocation checks.
// Both *RevocationStore (SQLite/memory) and *RedisRevocationStore satisfy this interface.
type RevocationChecker interface {
	IsRevokedRaw(token string) bool
}

// JTIChecker is the interface for JWT ID replay prevention.
// Both *JTICache (SQLite/memory) and *RedisJTICache satisfy this interface.
type JTIChecker interface {
	CheckAndRecord(jti string, expiresAt time.Time) error
}

// JWTValidator validates JWT bearer tokens from an external Authorization Server.
type JWTValidator struct {
	issuer             string
	audience           string
	jwks               *JWKSFetcher
	maxDelegationDepth int // 0 = unlimited, >0 = max allowed act nesting
	revocations        RevocationChecker
	jtiCache           JTIChecker
}

// NewJWTValidator creates a validator for the given issuer and audience.
func NewJWTValidator(issuer, audience string, jwks *JWKSFetcher) *JWTValidator {
	return &JWTValidator{
		issuer:   issuer,
		audience: audience,
		jwks:     jwks,
	}
}

// SetMaxDelegationDepth sets the maximum allowed delegation chain depth.
// 0 = unlimited (default). Tokens exceeding this depth are rejected.
func (v *JWTValidator) SetMaxDelegationDepth(depth int) {
	v.maxDelegationDepth = depth
}

// SetRevocationStore enables token revocation checking.
// Accepts any RevocationChecker (SQLite, in-memory, or Redis).
func (v *JWTValidator) SetRevocationStore(store RevocationChecker) {
	v.revocations = store
}

// SetJTICache enables JWT ID replay prevention.
// Accepts any JTIChecker (SQLite, in-memory, or Redis).
func (v *JWTValidator) SetJTICache(cache JTIChecker) {
	v.jtiCache = cache
}

// Validate parses and validates a JWT token string. Returns claims on success.
func (v *JWTValidator) Validate(tokenString string) (*JWTClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, ErrMalformedToken
	}

	// Decode header
	headerBytes, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: header decode: %v", ErrMalformedToken, err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("%w: header parse: %v", ErrMalformedToken, err)
	}

	// Get the signing key
	key, err := v.jwks.GetKey(header.Kid)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}

	// Verify signature
	sigBytes, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: signature decode: %v", ErrMalformedToken, err)
	}

	signedContent := parts[0] + "." + parts[1]
	if err := verifySignature(header.Alg, key, []byte(signedContent), sigBytes); err != nil {
		return nil, err
	}

	// Check revocation (after signature is verified, before claims processing)
	if v.revocations != nil && v.revocations.IsRevokedRaw(tokenString) {
		return nil, fmt.Errorf("%w: token has been revoked", ErrInvalidSignature)
	}

	// Decode and validate claims
	payloadBytes, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: payload decode: %v", ErrMalformedToken, err)
	}

	var raw rawClaims
	if err := json.Unmarshal(payloadBytes, &raw); err != nil {
		return nil, fmt.Errorf("%w: payload parse: %v", ErrMalformedToken, err)
	}

	claims, err := raw.toClaims()
	if err != nil {
		return nil, err
	}

	// Validate issuer
	if claims.Issuer != v.issuer {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrInvalidIssuer, claims.Issuer, v.issuer)
	}

	// Validate audience
	if !containsAudience(claims.Audience, v.audience) {
		return nil, fmt.Errorf("%w: %v does not contain %q", ErrInvalidAudience, claims.Audience, v.audience)
	}

	// Validate expiry
	now := time.Now()
	if !claims.Expiry.IsZero() && now.After(claims.Expiry.Add(clockSkew)) {
		return nil, ErrTokenExpired
	}

	// Validate not-before (RFC 7519 §4.1.5)
	if !claims.NotBefore.IsZero() && now.Before(claims.NotBefore.Add(-clockSkew)) {
		return nil, ErrTokenNotYetValid
	}

	// Validate may_act authorization: if both act and may_act are present,
	// the actor (act.sub) must match the authorized actor (may_act.sub).
	if claims.Actor != nil && claims.MayAct != nil {
		if claims.Actor.Subject != claims.MayAct.Subject {
			return nil, fmt.Errorf("%w: actor %q not authorized (may_act allows %q)",
				ErrDelegationUnauthorized, claims.Actor.Subject, claims.MayAct.Subject)
		}
	}

	// Validate delegation depth (configurable policy)
	if v.maxDelegationDepth > 0 && claims.DelegationDepth() > v.maxDelegationDepth {
		return nil, fmt.Errorf("%w: depth %d exceeds max %d",
			ErrDelegationTooDeep, claims.DelegationDepth(), v.maxDelegationDepth)
	}

	// JWT ID replay prevention (if jti claim is present and cache is configured)
	if claims.ID != "" && v.jtiCache != nil {
		if err := v.jtiCache.CheckAndRecord(claims.ID, claims.Expiry); err != nil {
			return nil, err
		}
	}

	return claims, nil
}

// rawClaims is the intermediate JSON representation before validation.
type rawClaims struct {
	Sub      string          `json:"sub"`
	Iss      string          `json:"iss"`
	Aud      json.RawMessage `json:"aud"` // string or []string
	Exp      *float64        `json:"exp"`
	Iat      *float64        `json:"iat"`
	Nbf      *float64        `json:"nbf"`
	Jti      string          `json:"jti,omitempty"`
	Act      json.RawMessage `json:"act,omitempty"`
	MayAct   json.RawMessage `json:"may_act,omitempty"`
	ClientID string          `json:"client_id,omitempty"`
	Azp      string          `json:"azp,omitempty"`
	Scope    string          `json:"scope,omitempty"`
}

func (r *rawClaims) toClaims() (*JWTClaims, error) {
	c := &JWTClaims{
		Subject:  r.Sub,
		Issuer:   r.Iss,
		ClientID: r.ClientID,
		ID:       r.Jti,
	}

	if c.ClientID == "" {
		c.ClientID = r.Azp
	}

	// Parse audience (string or []string)
	if len(r.Aud) > 0 {
		var audStr string
		if err := json.Unmarshal(r.Aud, &audStr); err == nil {
			c.Audience = []string{audStr}
		} else {
			var audArr []string
			if err := json.Unmarshal(r.Aud, &audArr); err == nil {
				c.Audience = audArr
			}
		}
	}

	if r.Exp != nil {
		c.Expiry = time.Unix(int64(*r.Exp), 0)
	}
	if r.Nbf != nil {
		c.NotBefore = time.Unix(int64(*r.Nbf), 0)
	}
	if r.Iat != nil {
		c.IssuedAt = time.Unix(int64(*r.Iat), 0)
	}

	// Parse scope (space-separated string)
	if r.Scope != "" {
		c.Scopes = strings.Fields(r.Scope)
	}

	// Parse act claim (active delegation)
	if len(r.Act) > 0 {
		act, err := parseActorClaim(r.Act, 0)
		if err != nil {
			return nil, fmt.Errorf("parse act claim: %w", err)
		}
		c.Actor = act
	}

	// Parse may_act claim (future authorization for delegation)
	if len(r.MayAct) > 0 && string(r.MayAct) != "null" {
		mayAct, err := parseActorClaim(r.MayAct, 0)
		if err != nil {
			return nil, fmt.Errorf("parse may_act claim: %w", err)
		}
		c.MayAct = mayAct
	}

	return c, nil
}

func parseActorClaim(raw json.RawMessage, depth int) (*ActorClaim, error) {
	if depth >= maxActDepth {
		return nil, fmt.Errorf("act claim nesting exceeds max depth %d", maxActDepth)
	}

	var a struct {
		Sub string          `json:"sub"`
		Iss string          `json:"iss,omitempty"`
		Act json.RawMessage `json:"act,omitempty"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}

	claim := &ActorClaim{
		Subject: a.Sub,
		Issuer:  a.Iss,
	}

	if len(a.Act) > 0 && string(a.Act) != "null" {
		nested, err := parseActorClaim(a.Act, depth+1)
		if err != nil {
			return nil, err
		}
		claim.Actor = nested
	}

	return claim, nil
}

func containsAudience(audiences []string, target string) bool {
	for _, a := range audiences {
		if a == target {
			return true
		}
	}
	return false
}

// verifySignature verifies the JWT signature using the given algorithm and key.
func verifySignature(alg string, key crypto.PublicKey, signed, sig []byte) error {
	switch alg {
	case "RS256":
		return verifyRSA(crypto.SHA256, key, signed, sig)
	case "RS384":
		return verifyRSA(crypto.SHA384, key, signed, sig)
	case "RS512":
		return verifyRSA(crypto.SHA512, key, signed, sig)
	case "ES256":
		return verifyEC(sha256.New, 32, key, signed, sig)
	case "ES384":
		return verifyEC(sha512.New384, 48, key, signed, sig)
	case "ES512":
		return verifyEC(sha512.New, 66, key, signed, sig)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAlg, alg)
	}
}

func verifyRSA(hash crypto.Hash, key crypto.PublicKey, signed, sig []byte) error {
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: key is not RSA", ErrInvalidSignature)
	}

	h := hash.New()
	h.Write(signed)
	digest := h.Sum(nil)

	if err := rsa.VerifyPKCS1v15(rsaKey, hash, digest, sig); err != nil {
		return ErrInvalidSignature
	}
	return nil
}

func verifyEC(hashFunc func() hash.Hash, keySize int, key crypto.PublicKey, signed, sig []byte) error {
	ecKey, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: key is not EC", ErrInvalidSignature)
	}

	h := hashFunc()
	h.Write(signed)
	digest := h.Sum(nil)

	// EC signatures in JWTs are r||s concatenated (not ASN.1 DER)
	if len(sig) != 2*keySize {
		return ErrInvalidSignature
	}

	r := new(big.Int).SetBytes(sig[:keySize])
	s := new(big.Int).SetBytes(sig[keySize:])

	if !ecdsa.Verify(ecKey, digest, r, s) {
		return ErrInvalidSignature
	}
	return nil
}
