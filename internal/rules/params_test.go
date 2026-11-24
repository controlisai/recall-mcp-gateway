package rules

import (
	"testing"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

func TestCheckParamBlocks_Match(t *testing.T) {
	blocks := []config.ParamBlockConfig{
		{Param: "sql", Pattern: `(?i)\bDELETE\b`, Reason: "destructive SQL"},
	}
	args := map[string]any{"sql": "DELETE FROM users"}

	matched, reason := CheckParamBlocks(blocks, args)
	if !matched {
		t.Error("expected match for DELETE")
	}
	if reason != "destructive SQL" {
		t.Errorf("reason = %q", reason)
	}
}

func TestCheckParamBlocks_NoMatch(t *testing.T) {
	blocks := []config.ParamBlockConfig{
		{Param: "sql", Pattern: `(?i)\bDELETE\b`},
	}
	args := map[string]any{"sql": "SELECT * FROM users"}

	matched, _ := CheckParamBlocks(blocks, args)
	if matched {
		t.Error("should not match SELECT")
	}
}

func TestCheckParamBlocks_MissingParam(t *testing.T) {
	blocks := []config.ParamBlockConfig{
		{Param: "sql", Pattern: `DELETE`},
	}
	args := map[string]any{"name": "John"}

	matched, _ := CheckParamBlocks(blocks, args)
	if matched {
		t.Error("should not match when param missing")
	}
}

func TestCheckParamBlocks_DefaultReason(t *testing.T) {
	blocks := []config.ParamBlockConfig{
		{Param: "path", Pattern: `/etc/passwd`},
	}
	args := map[string]any{"path": "/etc/passwd"}

	matched, reason := CheckParamBlocks(blocks, args)
	if !matched {
		t.Error("expected match")
	}
	if reason == "" {
		t.Error("expected default reason")
	}
}

func TestCheckParamBlocks_InvalidRegex(t *testing.T) {
	blocks := []config.ParamBlockConfig{
		{Param: "data", Pattern: `[invalid`},
	}
	args := map[string]any{"data": "anything"}

	matched, _ := CheckParamBlocks(blocks, args)
	if matched {
		t.Error("invalid regex should not match")
	}
}
