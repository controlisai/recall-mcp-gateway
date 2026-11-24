package approval

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// SignedURLParams contains the fields embedded in a signed approval URL.
type SignedURLParams struct {
	ID       string // approval entry ID
	Action   string // "approve" or "deny"
	Reviewer string // identity of the approver (e.g., "admin", "alice@example.com")
	Exp      int64  // Unix timestamp when the link expires
	Sig      string // HMAC-SHA256 signature, base64url encoded
}

// SignedURLGenerator creates and validates signed approval URLs.
type SignedURLGenerator struct {
	key     []byte // SHA-256 of approval_secret
	baseURL string // e.g., "http://localhost:8090"

	mu             sync.Mutex
	usedSignatures map[string]int64 // sig → exp unix timestamp (replay protection)
}

// NewSignedURLGenerator creates a generator with the given secret and base URL.
func NewSignedURLGenerator(secret, baseURL string) *SignedURLGenerator {
	h := sha256.Sum256([]byte(secret))
	return &SignedURLGenerator{key: h[:], baseURL: baseURL, usedSignatures: make(map[string]int64)}
}

// Generate creates a signed approval URL for the given parameters.
func (g *SignedURLGenerator) Generate(id, action, reviewer string, expiresAt time.Time) string {
	exp := expiresAt.Unix()
	sig := g.computeHMAC(id, action, reviewer, exp)

	return fmt.Sprintf("%s/approvals/%s/%s?reviewer=%s&exp=%d&sig=%s",
		g.baseURL, url.PathEscape(id), url.PathEscape(action),
		url.QueryEscape(reviewer), exp, url.QueryEscape(sig))
}

// GeneratePair returns both approve and deny URLs for a given approval.
func (g *SignedURLGenerator) GeneratePair(id, reviewer string, expiresAt time.Time) (approveURL, denyURL string) {
	return g.Generate(id, "approve", reviewer, expiresAt),
		g.Generate(id, "deny", reviewer, expiresAt)
}

// Validate checks a signed URL's parameters. Returns nil if valid.
// Always computes the HMAC to avoid timing side-channels.
// Signatures are single-use: a validated signature is rejected on subsequent attempts.
func (g *SignedURLGenerator) Validate(p SignedURLParams) error {
	validAction := p.Action == "approve" || p.Action == "deny"
	notExpired := p.Exp > time.Now().Unix()

	// Always compute HMAC regardless of other validation results (constant-time)
	expected := g.computeHMAC(p.ID, p.Action, p.Reviewer, p.Exp)
	sigValid := subtle.ConstantTimeCompare([]byte(expected), []byte(p.Sig)) == 1

	if !validAction {
		return fmt.Errorf("invalid action: %s", p.Action)
	}
	if !notExpired {
		return fmt.Errorf("approval link expired")
	}
	if !sigValid {
		return fmt.Errorf("invalid signature")
	}

	// Replay protection: reject if signature was already consumed
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cleanupExpired()
	if _, used := g.usedSignatures[p.Sig]; used {
		return fmt.Errorf("signature already used")
	}
	g.usedSignatures[p.Sig] = p.Exp
	return nil
}

// cleanupExpired removes expired signatures from the used set. Must be called with mu held.
func (g *SignedURLGenerator) cleanupExpired() {
	now := time.Now().Unix()
	for sig, exp := range g.usedSignatures {
		if exp < now {
			delete(g.usedSignatures, sig)
		}
	}
}

// ParseQuery extracts SignedURLParams from URL query parameters.
func ParseQuery(id, action string, q url.Values) SignedURLParams {
	exp, _ := strconv.ParseInt(q.Get("exp"), 10, 64)
	return SignedURLParams{
		ID:       id,
		Action:   action,
		Reviewer: q.Get("reviewer"),
		Exp:      exp,
		Sig:      q.Get("sig"),
	}
}

func (g *SignedURLGenerator) computeHMAC(id, action, reviewer string, exp int64) string {
	mac := hmac.New(sha256.New, g.key)
	fmt.Fprintf(mac, "%s:%s:%s:%d", id, action, reviewer, exp)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
