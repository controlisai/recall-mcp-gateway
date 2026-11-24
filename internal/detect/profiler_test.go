package detect

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestProfiler_DestructiveTool(t *testing.T) {
	p := NewProfiler()
	p.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete a user")),
	})

	profile := p.GetProfile("delete_user")
	if profile == nil {
		t.Fatal("profile not found")
	}
	if profile.Classification != "DESTRUCTIVE" {
		t.Errorf("classification = %q, want DESTRUCTIVE", profile.Classification)
	}
	if profile.RiskLevel != "CRITICAL" {
		t.Errorf("risk = %q, want CRITICAL", profile.RiskLevel)
	}
}

func TestProfiler_ReadOnlyTool(t *testing.T) {
	p := NewProfiler()
	p.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("list_users", mcp.WithDescription("List all users")),
	})

	profile := p.GetProfile("list_users")
	if profile == nil {
		t.Fatal("profile not found")
	}
	if profile.Classification != "READ_ONLY" {
		t.Errorf("classification = %q, want READ_ONLY", profile.Classification)
	}
	if profile.RiskLevel != "LOW" {
		t.Errorf("risk = %q, want LOW", profile.RiskLevel)
	}
}

func TestProfiler_WriteTool(t *testing.T) {
	p := NewProfiler()
	p.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("update_record", mcp.WithDescription("Update a record")),
	})

	profile := p.GetProfile("update_record")
	if profile == nil {
		t.Fatal("profile not found")
	}
	if profile.Classification != "WRITE" {
		t.Errorf("classification = %q, want WRITE", profile.Classification)
	}
}

func TestProfiler_ExternalComm(t *testing.T) {
	p := NewProfiler()
	p.ProfileTools("comms", []mcp.Tool{
		mcp.NewTool("send_email", mcp.WithDescription("Send an email message")),
	})

	profile := p.GetProfile("send_email")
	if profile == nil {
		t.Fatal("profile not found")
	}
	if profile.Classification != "EXTERNAL_COMM" {
		t.Errorf("classification = %q, want EXTERNAL_COMM", profile.Classification)
	}
	if profile.RiskLevel != "HIGH" {
		t.Errorf("risk = %q, want HIGH", profile.RiskLevel)
	}
}

func TestProfiler_CodeExecution(t *testing.T) {
	p := NewProfiler()
	p.ProfileTools("dev", []mcp.Tool{
		mcp.NewTool("execute_code", mcp.WithDescription("Execute arbitrary code")),
	})

	profile := p.GetProfile("execute_code")
	if profile == nil {
		t.Fatal("profile not found")
	}
	if profile.Classification != "CODE_EXECUTION" {
		t.Errorf("classification = %q, want CODE_EXECUTION", profile.Classification)
	}
	if profile.RiskLevel != "CRITICAL" {
		t.Errorf("risk = %q, want CRITICAL", profile.RiskLevel)
	}
}

func TestProfiler_UnknownDefaultsToReadOnly(t *testing.T) {
	p := NewProfiler()
	p.ProfileTools("misc", []mcp.Tool{
		mcp.NewTool("foobar", mcp.WithDescription("Does something")),
	})

	profile := p.GetProfile("foobar")
	if profile == nil {
		t.Fatal("profile not found")
	}
	if profile.Classification != "READ_ONLY" {
		t.Errorf("classification = %q, want READ_ONLY (default)", profile.Classification)
	}
}

func TestProfiler_ProfilesByRisk(t *testing.T) {
	p := NewProfiler()
	p.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete user")),
		mcp.NewTool("list_users", mcp.WithDescription("List users")),
		mcp.NewTool("update_record", mcp.WithDescription("Update record")),
	})

	byRisk := p.ProfilesByRisk()
	if len(byRisk["CRITICAL"]) != 1 {
		t.Errorf("CRITICAL = %d, want 1", len(byRisk["CRITICAL"]))
	}
	if len(byRisk["LOW"]) != 1 {
		t.Errorf("LOW = %d, want 1", len(byRisk["LOW"]))
	}
}
