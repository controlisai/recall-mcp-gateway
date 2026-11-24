package audit

import (
	"testing"
	"time"
)

func TestHashChain_DeterministicHashes(t *testing.T) {
	c1 := NewHashChain()
	c2 := NewHashChain()

	e1 := Entry{Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Tool: "test", AgentID: "a1", Decision: "allow"}
	e2 := Entry{Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Tool: "test", AgentID: "a1", Decision: "allow"}

	h1 := c1.ComputeHash(&e1)
	h2 := c2.ComputeHash(&e2)

	if h1 != h2 {
		t.Errorf("same entries should produce same hash: %s != %s", h1, h2)
	}
}

func TestHashChain_DifferentEntriesDifferentHashes(t *testing.T) {
	c := NewHashChain()
	e1 := Entry{Tool: "tool1", Decision: "allow"}
	e2 := Entry{Tool: "tool2", Decision: "block"}

	h1 := c.ComputeHash(&e1)
	h2 := c.ComputeHash(&e2)

	if h1 == h2 {
		t.Error("different entries should produce different hashes")
	}
}

func TestHashChain_VerifyChainIntact(t *testing.T) {
	entries := []Entry{
		{Tool: "t1", Decision: "allow"},
		{Tool: "t2", Decision: "block"},
		{Tool: "t3", Decision: "allow"},
	}

	chain := NewHashChain()
	for i := range entries {
		chain.ComputeHash(&entries[i])
	}

	tampered := VerifyChain(entries)
	if tampered != -1 {
		t.Errorf("intact chain should return -1, got %d", tampered)
	}
}

func TestHashChain_DetectTampering(t *testing.T) {
	entries := []Entry{
		{Tool: "t1", Decision: "allow"},
		{Tool: "t2", Decision: "block"},
		{Tool: "t3", Decision: "allow"},
	}

	chain := NewHashChain()
	for i := range entries {
		chain.ComputeHash(&entries[i])
	}

	// Tamper with middle entry
	entries[1].Decision = "allow" // was "block"

	tampered := VerifyChain(entries)
	if tampered == -1 {
		t.Error("tampered chain should be detected")
	}
}
