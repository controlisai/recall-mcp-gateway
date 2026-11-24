package detect

import "testing"

func TestContentDetector_SSN(t *testing.T) {
	d := NewContentDetector()
	signals := d.Scan(map[string]any{
		"data": "Patient SSN is 123-45-6789",
	})
	if len(signals) == 0 {
		t.Fatal("expected SSN signal")
	}
	found := false
	for _, s := range signals {
		if s.Name == "ssn" {
			found = true
			if s.Severity != "critical" {
				t.Errorf("severity = %q, want critical", s.Severity)
			}
		}
	}
	if !found {
		t.Error("ssn pattern not matched")
	}
}

func TestContentDetector_CreditCard(t *testing.T) {
	d := NewContentDetector()
	signals := d.Scan(map[string]any{
		"card": "4111-1111-1111-1111",
	})
	found := false
	for _, s := range signals {
		if s.Name == "credit_card" {
			found = true
		}
	}
	if !found {
		t.Error("credit card pattern not matched")
	}
}

func TestContentDetector_AWSKey(t *testing.T) {
	d := NewContentDetector()
	signals := d.Scan(map[string]any{
		"config": "AKIAIOSFODNN7EXAMPLE",
	})
	found := false
	for _, s := range signals {
		if s.Name == "aws_key" {
			found = true
		}
	}
	if !found {
		t.Error("AWS key pattern not matched")
	}
}

func TestContentDetector_DestructiveSQL(t *testing.T) {
	d := NewContentDetector()
	signals := d.Scan(map[string]any{
		"sql": "DELETE FROM users WHERE id = 1",
	})
	found := false
	for _, s := range signals {
		if s.Name == "destructive_sql" {
			found = true
		}
	}
	if !found {
		t.Error("destructive SQL pattern not matched")
	}
}

func TestContentDetector_SensitivePath(t *testing.T) {
	d := NewContentDetector()
	signals := d.Scan(map[string]any{
		"path": "/etc/passwd",
	})
	found := false
	for _, s := range signals {
		if s.Name == "system_file" {
			found = true
		}
	}
	if !found {
		t.Error("system file pattern not matched")
	}
}

func TestContentDetector_Clean(t *testing.T) {
	d := NewContentDetector()
	signals := d.Scan(map[string]any{
		"name": "John",
		"age":  30,
	})
	if len(signals) != 0 {
		t.Errorf("expected 0 signals, got %d", len(signals))
	}
}

func TestContentDetector_NestedMap(t *testing.T) {
	d := NewContentDetector()
	signals := d.Scan(map[string]any{
		"nested": map[string]any{
			"secret": "api_key=sk_live_abc123xyz",
		},
	})
	found := false
	for _, s := range signals {
		if s.Name == "api_key" {
			found = true
		}
	}
	if !found {
		t.Error("API key in nested map not detected")
	}
}

func TestContentDetector_PrivateKey(t *testing.T) {
	d := NewContentDetector()
	signals := d.Scan(map[string]any{
		"key": "-----BEGIN RSA PRIVATE KEY-----\nMIIE...",
	})
	found := false
	for _, s := range signals {
		if s.Name == "private_key" {
			found = true
		}
	}
	if !found {
		t.Error("private key pattern not matched")
	}
}
