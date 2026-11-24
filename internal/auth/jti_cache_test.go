package auth

import (
	"testing"
	"time"
)

func TestJTICache_FirstUseAccepted(t *testing.T) {
	c := NewJTICache()
	defer c.Close()
	if err := c.CheckAndRecord("jti-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("first use should succeed: %v", err)
	}
}

func TestJTICache_ReplayRejected(t *testing.T) {
	c := NewJTICache()
	defer c.Close()
	c.CheckAndRecord("jti-2", time.Now().Add(time.Hour))
	if err := c.CheckAndRecord("jti-2", time.Now().Add(time.Hour)); err != ErrTokenReplay {
		t.Fatalf("replay should be rejected: %v", err)
	}
}

func TestJTICache_DifferentIDsAccepted(t *testing.T) {
	c := NewJTICache()
	defer c.Close()
	c.CheckAndRecord("jti-a", time.Now().Add(time.Hour))
	if err := c.CheckAndRecord("jti-b", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("different jti should succeed: %v", err)
	}
}
