package approval

import (
	"net/url"
	"testing"
	"time"
)

func TestSignedURL_GenerateAndValidate(t *testing.T) {
	gen := NewSignedURLGenerator("test-secret", "http://localhost:8090")
	exp := time.Now().Add(5 * time.Minute)

	approveURL, denyURL := gen.GeneratePair("apr-123", "admin", exp)

	// Parse the approve URL
	u, err := url.Parse(approveURL)
	if err != nil {
		t.Fatalf("parse approve URL: %v", err)
	}
	if u.Path != "/approvals/apr-123/approve" {
		t.Errorf("path = %q, want /approvals/apr-123/approve", u.Path)
	}

	// Validate approve
	p := ParseQuery("apr-123", "approve", u.Query())
	if err := gen.Validate(p); err != nil {
		t.Fatalf("validate approve: %v", err)
	}
	if p.Reviewer != "admin" {
		t.Errorf("reviewer = %q, want admin", p.Reviewer)
	}

	// Validate deny
	du, _ := url.Parse(denyURL)
	dp := ParseQuery("apr-123", "deny", du.Query())
	if err := gen.Validate(dp); err != nil {
		t.Fatalf("validate deny: %v", err)
	}
}

func TestSignedURL_ExpiredRejected(t *testing.T) {
	gen := NewSignedURLGenerator("test-secret", "http://localhost:8090")
	exp := time.Now().Add(-1 * time.Minute) // already expired

	approveURL, _ := gen.GeneratePair("apr-456", "admin", exp)
	u, _ := url.Parse(approveURL)
	p := ParseQuery("apr-456", "approve", u.Query())

	err := gen.Validate(p)
	if err == nil {
		t.Fatal("expected error for expired link")
	}
	if err.Error() != "approval link expired" {
		t.Errorf("error = %q, want 'approval link expired'", err.Error())
	}
}

func TestSignedURL_WrongSignatureRejected(t *testing.T) {
	gen := NewSignedURLGenerator("test-secret", "http://localhost:8090")
	exp := time.Now().Add(5 * time.Minute)

	approveURL, _ := gen.GeneratePair("apr-789", "admin", exp)
	u, _ := url.Parse(approveURL)
	p := ParseQuery("apr-789", "approve", u.Query())
	p.Sig = "tampered-signature"

	err := gen.Validate(p)
	if err == nil {
		t.Fatal("expected error for tampered signature")
	}
	if err.Error() != "invalid signature" {
		t.Errorf("error = %q, want 'invalid signature'", err.Error())
	}
}

func TestSignedURL_WrongIDRejected(t *testing.T) {
	gen := NewSignedURLGenerator("test-secret", "http://localhost:8090")
	exp := time.Now().Add(5 * time.Minute)

	approveURL, _ := gen.GeneratePair("apr-original", "admin", exp)
	u, _ := url.Parse(approveURL)
	// Use the signature from apr-original but claim it's for apr-different
	p := ParseQuery("apr-different", "approve", u.Query())

	err := gen.Validate(p)
	if err == nil {
		t.Fatal("expected error for mismatched ID")
	}
}

func TestSignedURL_WrongActionRejected(t *testing.T) {
	gen := NewSignedURLGenerator("test-secret", "http://localhost:8090")
	exp := time.Now().Add(5 * time.Minute)

	approveURL, _ := gen.GeneratePair("apr-abc", "admin", exp)
	u, _ := url.Parse(approveURL)
	// Use approve URL's signature but claim it's for deny
	p := ParseQuery("apr-abc", "deny", u.Query())

	err := gen.Validate(p)
	if err == nil {
		t.Fatal("expected error for mismatched action")
	}
}

func TestSignedURL_DifferentSecretRejected(t *testing.T) {
	gen1 := NewSignedURLGenerator("secret-1", "http://localhost:8090")
	gen2 := NewSignedURLGenerator("secret-2", "http://localhost:8090")
	exp := time.Now().Add(5 * time.Minute)

	approveURL, _ := gen1.GeneratePair("apr-xyz", "admin", exp)
	u, _ := url.Parse(approveURL)
	p := ParseQuery("apr-xyz", "approve", u.Query())

	// Validate with different secret
	err := gen2.Validate(p)
	if err == nil {
		t.Fatal("expected error for different secret")
	}
}

func TestSignedURL_EmailReviewerRoundTrips(t *testing.T) {
	gen := NewSignedURLGenerator("test-secret", "http://localhost:8090")
	exp := time.Now().Add(5 * time.Minute)

	approveURL, _ := gen.GeneratePair("apr-deleg", "alice@example.com", exp)
	u, _ := url.Parse(approveURL)
	p := ParseQuery("apr-deleg", "approve", u.Query())

	if p.Reviewer != "alice@example.com" {
		t.Errorf("reviewer = %q, want alice@example.com", p.Reviewer)
	}
	if err := gen.Validate(p); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestSignedURL_ReplayRejected(t *testing.T) {
	gen := NewSignedURLGenerator("test-secret", "http://localhost:8090")
	exp := time.Now().Add(5 * time.Minute)

	approveURL, _ := gen.GeneratePair("apr-replay", "admin", exp)
	u, _ := url.Parse(approveURL)
	p := ParseQuery("apr-replay", "approve", u.Query())

	// First validation should succeed
	if err := gen.Validate(p); err != nil {
		t.Fatalf("first validate should succeed: %v", err)
	}

	// Second validation with same signature should be rejected
	err := gen.Validate(p)
	if err == nil {
		t.Fatal("expected replay rejection on second validate")
	}
	if err.Error() != "signature already used" {
		t.Errorf("error = %q, want 'signature already used'", err.Error())
	}
}

func TestSignedURL_ExpiredSignatureCleanedUp(t *testing.T) {
	gen := NewSignedURLGenerator("test-secret", "http://localhost:8090")
	// Manually insert an expired used signature
	gen.mu.Lock()
	gen.usedSignatures["old-sig"] = time.Now().Add(-1 * time.Minute).Unix()
	gen.mu.Unlock()

	// Validate a new URL — should trigger cleanup of the expired entry
	exp := time.Now().Add(5 * time.Minute)
	approveURL, _ := gen.GeneratePair("apr-cleanup", "admin", exp)
	u, _ := url.Parse(approveURL)
	p := ParseQuery("apr-cleanup", "approve", u.Query())
	if err := gen.Validate(p); err != nil {
		t.Fatalf("validate should succeed: %v", err)
	}

	gen.mu.Lock()
	_, oldExists := gen.usedSignatures["old-sig"]
	gen.mu.Unlock()
	if oldExists {
		t.Error("expired signature should have been cleaned up")
	}
}

func TestSignedURL_InvalidAction(t *testing.T) {
	gen := NewSignedURLGenerator("test-secret", "http://localhost:8090")
	err := gen.Validate(SignedURLParams{
		ID: "apr-1", Action: "delete", Reviewer: "admin", Exp: time.Now().Add(time.Minute).Unix(), Sig: "x",
	})
	if err == nil || err.Error() != "invalid action: delete" {
		t.Errorf("expected 'invalid action: delete', got %v", err)
	}
}
