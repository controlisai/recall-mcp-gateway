package approval

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// RequestState holds the context encrypted in an elicitation requestState blob.
// The client echoes this opaque string back on retry — the gateway decrypts it
// to validate the consent matches the original tool call.
type RequestState struct {
	AgentID       string    `json:"a"`             // agent that made the call
	Tool          string    `json:"t"`             // tool name
	ArgsHash      string    `json:"h"`             // SHA-256 of arguments
	Timestamp     time.Time `json:"ts"`            // when the state was created
	UpstreamState string    `json:"us,omitempty"`  // upstream's opaque requestState (passthrough)
}

const stateMaxAge = 10 * time.Minute

// StateEncryptor handles AEAD encryption/decryption of requestState blobs.
type StateEncryptor struct {
	gcm cipher.AEAD
}

var (
	ephemeralKey     []byte
	ephemeralKeyOnce sync.Once
)

// NewStateEncryptor creates an encryptor from the given secret.
// If secret is empty, generates an ephemeral key (logs a warning — states won't survive restart).
func NewStateEncryptor(secret string) (*StateEncryptor, error) {
	var key []byte
	if secret == "" {
		ephemeralKeyOnce.Do(func() {
			ephemeralKey = make([]byte, 32)
			rand.Read(ephemeralKey)
			slog.Warn("no approval_secret configured — using ephemeral key for requestState encryption (will not survive restart)", "component", "elicitation")
		})
		key = ephemeralKey
	} else {
		h := sha256.Sum256([]byte(secret))
		key = h[:]
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &StateEncryptor{gcm: gcm}, nil
}

// Encrypt serializes and encrypts a RequestState into a base64url string.
func (e *StateEncryptor) Encrypt(state RequestState) (string, error) {
	plaintext, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal state: %w", err)
	}

	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := e.gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decodes and decrypts a base64url requestState string.
// Returns an error if tampered, expired (>10 min), or wrong key.
func (e *StateEncryptor) Decrypt(encoded string) (*RequestState, error) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	var state RequestState
	if err := json.Unmarshal(plaintext, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	if time.Since(state.Timestamp) > stateMaxAge {
		return nil, fmt.Errorf("state expired (age %v > %v)", time.Since(state.Timestamp).Round(time.Second), stateMaxAge)
	}

	return &state, nil
}
