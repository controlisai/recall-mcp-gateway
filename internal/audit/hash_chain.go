package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
)

// HashChain computes a rolling SHA-256 hash chain for audit log integrity.
// Each entry's hash = SHA-256(previous_hash + entry_json).
type HashChain struct {
	mu       sync.Mutex
	lastHash string
}

// NewHashChain creates a chain starting from a genesis hash.
func NewHashChain() *HashChain {
	genesis := sha256.Sum256([]byte("recall-mcp-gateway-audit-genesis"))
	return &HashChain{lastHash: hex.EncodeToString(genesis[:])}
}

// NewHashChainFrom creates a chain continuing from a known last hash.
func NewHashChainFrom(lastHash string) *HashChain {
	if lastHash == "" {
		return NewHashChain()
	}
	return &HashChain{lastHash: lastHash}
}

// ComputeHash computes the hash for an entry and updates the chain.
// Sets entry.Hash and returns the computed hash.
func (c *HashChain) ComputeHash(entry *Entry) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Serialize entry without the hash field
	savedHash := entry.Hash
	entry.Hash = ""
	entryJSON, _ := json.Marshal(entry)
	entry.Hash = savedHash

	// Hash = SHA-256(previous_hash + entry_json)
	h := sha256.Sum256(append([]byte(c.lastHash), entryJSON...))
	hash := hex.EncodeToString(h[:])

	c.lastHash = hash
	entry.Hash = hash
	return hash
}

// LastHash returns the current chain tip hash.
func (c *HashChain) LastHash() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastHash
}

// VerifyChain replays a sequence of entries and verifies the hash chain.
// Returns the index of the first tampered entry, or -1 if all valid.
func VerifyChain(entries []Entry) int {
	chain := NewHashChain()
	for i := range entries {
		savedHash := entries[i].Hash
		// Recompute what the hash should be
		entries[i].Hash = ""
		expected := chain.ComputeHash(&entries[i])
		if savedHash != expected {
			entries[i].Hash = savedHash // restore
			return i
		}
		// entry.Hash is now set to expected by ComputeHash — that's fine
	}
	return -1
}
