package detect

import "regexp"

// ContentPattern defines a regex pattern to match against tool call argument values.
type ContentPattern struct {
	Name     string
	Pattern  *regexp.Regexp
	Category string // pii, credential, destructive, sensitive_path
	Severity string // warning, high, critical
}

// BuiltInContentPatterns are the default patterns that ship with the proxy.
var BuiltInContentPatterns = []ContentPattern{
	// PII
	{Name: "ssn", Pattern: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), Category: "pii", Severity: "critical"},
	{Name: "credit_card", Pattern: regexp.MustCompile(`\b\d{4}[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}\b`), Category: "pii", Severity: "critical"},
	{Name: "email_address", Pattern: regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`), Category: "pii", Severity: "warning"},
	{Name: "phone", Pattern: regexp.MustCompile(`\b\+?1?\d{10,14}\b`), Category: "pii", Severity: "warning"},

	// Credentials
	{Name: "api_key", Pattern: regexp.MustCompile(`(?i)(api[_\-]?key|apikey|secret[_\-]?key)\s*[:=]\s*\S+`), Category: "credential", Severity: "critical"},
	{Name: "bearer_token", Pattern: regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9\-._~+/]+=*`), Category: "credential", Severity: "critical"},
	{Name: "aws_key", Pattern: regexp.MustCompile(`AKIA[0-9A-Z]{16}`), Category: "credential", Severity: "critical"},
	{Name: "private_key", Pattern: regexp.MustCompile(`-----BEGIN (RSA |EC )?PRIVATE KEY-----`), Category: "credential", Severity: "critical"},

	// Destructive intent
	{Name: "destructive_sql", Pattern: regexp.MustCompile(`(?i)\b(DELETE\s+FROM|DROP\s+(TABLE|DATABASE)|TRUNCATE\s+TABLE|ALTER\s+TABLE)\b`), Category: "destructive", Severity: "critical"},

	// Sensitive paths
	{Name: "env_file", Pattern: regexp.MustCompile(`(?i)\.(env|pem|key|credentials|secret|p12|pfx)\b`), Category: "sensitive_path", Severity: "high"},
	{Name: "system_file", Pattern: regexp.MustCompile(`/etc/(passwd|shadow|hosts)|\.ssh/`), Category: "sensitive_path", Severity: "critical"},
}

// ClassificationKeywords maps tool name/description keywords to classifications.
var ClassificationKeywords = map[string][]string{
	"DESTRUCTIVE":    {"delete", "remove", "drop", "truncate", "destroy", "purge", "wipe"},
	"WRITE":          {"create", "update", "write", "insert", "set", "modify", "put", "patch", "add"},
	"READ_ONLY":      {"list", "get", "read", "fetch", "search", "find", "count", "describe", "show", "select"},
	"EXTERNAL_COMM":  {"send", "email", "message", "post", "notify", "publish", "sms", "slack", "webhook"},
	"CODE_EXECUTION": {"execute", "run", "eval", "exec", "shell", "command", "script"},
	"DATA_TRANSFER":  {"upload", "transfer", "export", "download", "import", "sync"},
}

// ClassificationPriority determines which classification wins when multiple match.
// Higher index = higher priority.
var ClassificationPriority = []string{
	"READ_ONLY",
	"WRITE",
	"DATA_TRANSFER",
	"EXTERNAL_COMM",
	"CODE_EXECUTION",
	"DESTRUCTIVE",
}

// RiskByClassification maps classification to base risk level.
var RiskByClassification = map[string]string{
	"READ_ONLY":      "LOW",
	"WRITE":          "MEDIUM",
	"DATA_TRANSFER":  "HIGH",
	"EXTERNAL_COMM":  "HIGH",
	"CODE_EXECUTION": "CRITICAL",
	"DESTRUCTIVE":    "CRITICAL",
}
