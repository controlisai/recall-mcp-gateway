# Compliance Evidence Guide

How Recall MCP Gateway decisions become auditable compliance evidence for regulatory frameworks.

## Evidence Pipeline

```
AI Agent
  |
  v
Recall MCP Gateway (tool call intercepted)
  |-- Auto-detection: PII, credentials, destructive SQL flagged
  |-- Rule evaluation: allow / block / require_approval
  |-- Audit entry created with full context
  |
  v
Recall Engine (/decide — connected mode)
  |-- Full policy evaluation (safety, control, admissibility)
  |-- Compliance scoring impact calculated
  |-- Decision stored in console-api database
  |
  v
Console-Tenant (compliance dashboard)
  |-- Historical decision audit trail
  |-- Compliance scoring trends
  |-- Evidence export (EU AI Act, SOC 2, OWASP, ISO 42001, NIST AI RMF)
```

Every MCP tool call that passes through the proxy creates an auditable record. In connected mode, this record flows through the engine's full policy pipeline and becomes part of the platform's compliance evidence.

## What Gets Recorded

Each proxy decision audit entry contains:

| Field | Description | Compliance Value |
|-------|-------------|-----------------|
| `timestamp` | UTC time of the decision | Temporal audit trail |
| `requestId` | Unique identifier | Traceability |
| `agentId` | Which AI agent made the call | Accountability |
| `orgId` | Customer/org context | Scope of governance |
| `tool` | MCP tool invoked | Action identification |
| `server` | Upstream MCP server | Infrastructure mapping |
| `decision` | allow / block / pending_approval | Decision outcome |
| `mode` | passthrough / rule / engine / approval | Decision authority |
| `arguments` | Tool call parameters | Context for review |
| `signals` | Auto-detected risks (PII, credentials, SQL) | Risk identification |
| `latencyMs` | Decision latency | Performance monitoring |
| `resultPreview` | Truncated response | Outcome verification |

## Framework Mapping

### EU AI Act

| Article | Requirement | Proxy Evidence |
|---------|-------------|---------------|
| Art. 12 — Record-keeping | AI systems shall be capable of automatic recording of events | Every tool call logged with full context |
| Art. 14 — Human oversight | High-risk AI systems shall be designed for human oversight | `require_approval` rules enforce human review before execution |
| Art. 9 — Risk management | Continuous risk identification and mitigation | Auto-detection flags PII, credentials, destructive operations |
| Art. 13 — Transparency | Users shall be able to understand AI system output | Audit trail with decision rationale, matched rules, signals |

### SOC 2

| Criteria | Proxy Evidence |
|----------|---------------|
| CC6.1 — Logical access | Agent identity verification (Bearer tokens) |
| CC6.3 — Restriction of access | Per-agent `allowed_servers`, tool hiding rules |
| CC7.2 — Monitoring | Behavioral baselines, anomaly detection, alerting |
| CC8.1 — Change management | Rule changes logged, config hot-reload audit |

### OWASP Top 10 for LLM Applications

| Risk | Proxy Mitigation |
|------|-----------------|
| LLM01 — Prompt Injection | Content detection flags suspicious patterns in arguments |
| LLM07 — Insecure Plugin Design | Tool profiling classifies risk at startup |
| LLM08 — Excessive Agency | Rate limiting, approval flow, sequence detection |
| LLM09 — Overreliance | Behavioral baselines detect deviation from normal patterns |

### ISO 42001 (AI Management System)

| Clause | Proxy Evidence |
|--------|---------------|
| 6.1 — Risk assessment | Auto-classification of tools by risk level |
| 8.4 — AI system operation | Decision audit trail with full evaluation context |
| 9.1 — Monitoring | Baselines, anomaly detection, server health |
| 10.1 — Continual improvement | Recommendation engine suggests rules from observed behavior |

### NIST AI RMF

| Function | Proxy Evidence |
|----------|---------------|
| Govern — Accountability | Per-agent, per-org decision logging |
| Map — Risk identification | Tool profiling, content detection, sequence analysis |
| Measure — Monitoring | EWMA baselines, anomaly alerting, compliance scoring |
| Manage — Response | Rule engine, approval flow, rate limiting |

## Where to Find Evidence

### Standalone Mode (Recall MCP Gateway / Proxy Pro)

| Evidence | Location |
|----------|----------|
| Audit trail | `audit` table in gateway.db (SQLite default) or PostgreSQL (set `postgres.url` to route audit to PostgreSQL automatically); JSONL also available for log-forwarding |
| Baselines | `baselines` table in gateway.db (SQLite) or PostgreSQL — survives restarts |
| Active rules | Admin API: `GET /admin/v1/rules` |
| Anomaly alerts | Admin API: `GET /admin/v1/status` |

> **Note:** PostgreSQL is a supported alternative to SQLite for standalone deployments that require centralised storage, easier backup, or compatibility with existing database infrastructure. Set `postgres.url` — audit is routed to PostgreSQL automatically.

### Connected Mode (Recall Platform)

| Evidence | Location |
|----------|----------|
| Decision audit trail | Console-tenant: Decisions page (filterable by source: proxy/sdk) |
| Compliance scoring | Console-tenant: Compliance page (per-agent scores, band trends) |
| Evidence exports | Console-tenant: Export buttons (EU AI Act, SOC 2, OWASP, ISO 42001, NIST) |
| Drift detection | Console-tenant: Drift page (behavioral drift alerts) |
| Per-org breakdown | Console-tenant: Organizations page |
| Agent health | Console-tenant: Agents page (trust, compliance score, band) |

### Export Formats

Console-tenant exports are available as:
- **PDF** — formatted compliance report for auditors
- **CSV** — raw data for analysis
- **JSON** — machine-readable for integration with GRC tools

Each export includes:
- Time range and scope (product, org, environment)
- Decision summary (total, executed, blocked, pending, observed)
- Agent compliance scores and band transitions
- Matched rules and policy references
- Signal summary (PII detections, credential exposures, destructive operations)

## Auditor Walkthrough

### "Show me every AI action in the last 30 days"

1. Open console-tenant Decisions page
2. Set date range to last 30 days
3. Filter by source: "All" (shows both SDK and proxy decisions)
4. Export as CSV for offline analysis

### "Show me all blocked actions and why"

1. Decisions page, click "Blocked" tab
2. Each entry shows: agent, tool, matched rule, reason, timestamp
3. Click any entry for full evaluation context (attributes, narrative)

### "Show me human oversight evidence"

1. Decisions page, click "Pending" tab
2. Shows all `PENDING_APPROVAL` decisions waiting for human review
3. Resolved approvals show: who approved, when, original request
4. Evidence of Art. 14 (EU AI Act) human oversight requirement

### "Show me PII handling"

1. Proxy dashboard: PII Data Flows section
2. Shows: which tools receive PII, where it flows, risk level
3. Console-tenant: Decisions page, filter by signals containing "pii"

### "What is the compliance posture for this product?"

1. Console-tenant: Compliance page
2. Shows: aggregate score, band (HEALTHY/DEGRADED/AT_RISK/CRITICAL)
3. Per-agent scores with trend charts
4. Band transition history
5. Export compliance evidence button for the relevant framework
