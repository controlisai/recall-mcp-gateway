package approval

import (
	"strings"
	"testing"
	"time"
)

func TestStateEncryptor_RoundTrip(t *testing.T) {
	enc, err := NewStateEncryptor("test-secret-key")
	if err != nil {
		t.Fatal(err)
	}

	original := RequestState{
		AgentID:   "agent-1",
		Tool:      "db_write",
		ArgsHash:  "abc123",
		Timestamp: time.Now(),
	}

	encoded, err := enc.Encrypt(original)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if encoded == "" {
		t.Fatal("expected non-empty encoded string")
	}

	decoded, err := enc.Decrypt(encoded)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decoded.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want agent-1", decoded.AgentID)
	}
	if decoded.Tool != "db_write" {
		t.Errorf("Tool = %q, want db_write", decoded.Tool)
	}
	if decoded.ArgsHash != "abc123" {
		t.Errorf("ArgsHash = %q, want abc123", decoded.ArgsHash)
	}
}

func TestStateEncryptor_TamperDetection(t *testing.T) {
	enc, _ := NewStateEncryptor("test-secret-key")

	state := RequestState{AgentID: "agent-1", Tool: "tool1", Timestamp: time.Now()}
	encoded, _ := enc.Encrypt(state)

	// Tamper with the ciphertext
	tampered := encoded[:len(encoded)-4] + "XXXX"
	_, err := enc.Decrypt(tampered)
	if err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
}

func TestStateEncryptor_WrongKey(t *testing.T) {
	enc1, _ := NewStateEncryptor("key-one")
	enc2, _ := NewStateEncryptor("key-two")

	state := RequestState{AgentID: "agent-1", Tool: "tool1", Timestamp: time.Now()}
	encoded, _ := enc1.Encrypt(state)

	_, err := enc2.Decrypt(encoded)
	if err == nil {
		t.Fatal("expected error for wrong key")
	}
}

func TestStateEncryptor_Expired(t *testing.T) {
	enc, _ := NewStateEncryptor("test-secret-key")

	state := RequestState{
		AgentID:   "agent-1",
		Tool:      "tool1",
		Timestamp: time.Now().Add(-15 * time.Minute), // 15 min ago — beyond 10 min max
	}
	encoded, _ := enc.Encrypt(state)

	_, err := enc.Decrypt(encoded)
	if err == nil {
		t.Fatal("expected error for expired state")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %q, want 'expired'", err.Error())
	}
}

func TestStateEncryptor_FreshState(t *testing.T) {
	enc, _ := NewStateEncryptor("test-secret-key")

	state := RequestState{
		AgentID:   "agent-1",
		Tool:      "tool1",
		Timestamp: time.Now().Add(-5 * time.Minute), // 5 min ago — within 10 min max
	}
	encoded, _ := enc.Encrypt(state)

	decoded, err := enc.Decrypt(encoded)
	if err != nil {
		t.Fatalf("expected fresh state to be valid: %v", err)
	}
	if decoded.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want agent-1", decoded.AgentID)
	}
}

func TestStateEncryptor_UpstreamState(t *testing.T) {
	enc, _ := NewStateEncryptor("test-secret-key")

	state := RequestState{
		AgentID:       "agent-1",
		Tool:          "tool1",
		Timestamp:     time.Now(),
		UpstreamState: "opaque-upstream-blob-12345",
	}
	encoded, _ := enc.Encrypt(state)

	decoded, err := enc.Decrypt(encoded)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decoded.UpstreamState != "opaque-upstream-blob-12345" {
		t.Errorf("UpstreamState = %q, want opaque-upstream-blob-12345", decoded.UpstreamState)
	}
}

func TestStateEncryptor_EphemeralKey(t *testing.T) {
	// Empty secret → ephemeral key (should work, just logs a warning)
	enc, err := NewStateEncryptor("")
	if err != nil {
		t.Fatal(err)
	}

	state := RequestState{AgentID: "agent-1", Tool: "tool1", Timestamp: time.Now()}
	encoded, err := enc.Encrypt(state)
	if err != nil {
		t.Fatalf("Encrypt with ephemeral key: %v", err)
	}

	decoded, err := enc.Decrypt(encoded)
	if err != nil {
		t.Fatalf("Decrypt with ephemeral key: %v", err)
	}
	if decoded.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want agent-1", decoded.AgentID)
	}
}

func TestStateEncryptor_UniqueNonce(t *testing.T) {
	enc, _ := NewStateEncryptor("test-secret-key")
	state := RequestState{AgentID: "agent-1", Tool: "tool1", Timestamp: time.Now()}

	// Same state encrypted twice should produce different ciphertexts (random nonce)
	e1, _ := enc.Encrypt(state)
	e2, _ := enc.Encrypt(state)
	if e1 == e2 {
		t.Error("expected different ciphertexts for same plaintext (nonce should differ)")
	}
}
