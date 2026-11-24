package auth

import (
	"testing"
	"time"
)

func TestRevocationStore_RevokeAndCheck(t *testing.T) {
	s := NewRevocationStore()
	defer s.Close()
	s.RevokeRaw("token-abc", time.Now().Add(time.Hour))
	if !s.IsRevokedRaw("token-abc") {
		t.Fatal("revoked token should be detected")
	}
	if s.IsRevokedRaw("token-xyz") {
		t.Fatal("non-revoked token should not be detected")
	}
}

func TestRevocationStore_Count(t *testing.T) {
	s := NewRevocationStore()
	defer s.Close()
	s.RevokeRaw("t1", time.Now().Add(time.Hour))
	s.RevokeRaw("t2", time.Now().Add(time.Hour))
	if s.Count() != 2 {
		t.Fatalf("count = %d, want 2", s.Count())
	}
}

func TestRevocationStore_HashConsistency(t *testing.T) {
	s := NewRevocationStore()
	defer s.Close()
	s.Revoke("abc123", time.Now().Add(time.Hour))
	if !s.IsRevoked("abc123") {
		t.Fatal("hash-based revocation should work")
	}
}
