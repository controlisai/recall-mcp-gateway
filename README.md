# Recall MCP Gateway

[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)
[![Status: Beta](https://img.shields.io/badge/status-beta-yellow)]()
[![Go](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](https://go.dev/)
[![MCP](https://img.shields.io/badge/MCP-2026--07--28-purple.svg)]()

**Know what your AI agents do. Control what they're allowed to do.**

An open-source governance gateway for MCP tool servers. Sits between AI agents and upstream tools, adding real-time visibility, behavioral detection, and policy control to every tool call.

One binary. Zero config to start. Full governance when you need it.

```
AI Agents (Claude, Cursor, n8n, LangChain, ServiceNow, ...)
    |
    v
Recall MCP Gateway (http://localhost:8090/mcp)
    |-- Sees every tool call in real time
    |-- Auto-detects risk (PII, credentials, destructive SQL)
    |-- Learns behavior, alerts on anomalies
    |-- Blocks, approves, rate-limits based on rules
    |-- Per-agent server access control
    v
Upstream MCP Servers (databases, APIs, SaaS tools)
```

## Quick Start

### Docker

```bash
docker run -p 8090:8090 controlisai/recall-mcp-gateway:latest
```

### From Source

```bash
git clone https://github.com/controlisai/recall-mcp-gateway.git
cd recall-mcp-gateway
make build
./recall-mcp-gateway --config configs/standalone.yaml
```

Dashboard at http://localhost:8090/dashboard. The Settings tab has copy-paste connection snippets for Claude Desktop, Cursor, n8n, and other frameworks.

## Features

### Zero-Config Detection
- **Tool risk profiling** — CRITICAL/HIGH/MEDIUM/LOW from schema analysis
- **PII detection** — SSN, credit cards, emails, phone numbers in tool arguments
- **Credential detection** — API keys, tokens, passwords in arguments
- **Destructive SQL detection** — DELETE, DROP, TRUNCATE patterns
- **Suspicious sequences** — data exfiltration, bulk delete, privilege escalation patterns
- **Behavioral baselines** — EWMA-based anomaly detection with alerting

### Governance Rules
```yaml
rules:
  - tool: "db_*_delete"
    action: block
    reason: "Destructive database operations blocked"

  - tool: "slack_send_message"
    action: require_approval
    agents: ["pipeline-bot"]

  - tool: "*"
    action: block
    schedule:
      cron: "* 9-17 * * 1-5"
      inside: false
    reason: "Block outside business hours"

  - tool: "create_*"
    action: block
    agents: ["junior-agent"]
    reason: "Junior agent cannot create resources"
```

Rules persist across restarts. Create from dashboard, feed actions, or YAML config.

### Approval Workflows
- **Consent elicitation** — MCP-native input_required for user-embedded agents
- **Admin approval** — HMAC-signed URLs via webhook or Slack interactive buttons
- **Delegated approval** — route to the acting user, not the admin
- **MCP Tasks** — async approval for task-capable clients
- **Three archetypes** — autonomous (admin approves), user_embedded (user consents), delegated (user approves)

### Agent Management
- Auto-discovered from MCP handshake (zero config)
- Bearer token or JWT authentication
- Per-agent server access control
- Per-agent rule scoping
- Three archetypes with archetype-aware governance

### Security
- External Authorization Server JWT validation (RS256/ES256, JWKS)
- Protected Resource Metadata (MCP 2026-07-28 spec)
- RFC 8693 token exchange for delegation
- Tamper-proof audit logs (SHA-256 hash chain)
- Token revocation, JTI replay prevention
- Credential redaction in all logs and audit entries
- Origin validation (CORS), trusted proxy config

### Dashboard

Live dashboard at `http://localhost:8090/dashboard` with five tabs:

- **Discover** — real-time tool call feed, audit history, suspicious sequence detection
- **Control** — servers, agents, rules management with inline editing
- **Understand** — compliance score, recommendations, governance gap analysis
- **Settings** — configuration, agent connection snippets, platform connection
- **Docs** — built-in API reference, security guide, deployment guide

### Framework Support

Works with any MCP-compatible agent — point at `http://localhost:8090/mcp`:

| Framework | Status |
|-----------|--------|
| Claude Desktop / Claude Code | Supported |
| Cursor / Windsurf | Supported |
| LangChain / LangGraph | Supported |
| CrewAI | Supported |
| ServiceNow AI Agent Studio | Supported |
| Salesforce Agentforce | Supported |
| Microsoft Copilot Studio | Supported |
| n8n | Supported |
| Any MCP client | Supported |

## Deployment

Three deployment modes — same binary, different backing stores:

| Mode | Config | Dependencies | Use Case |
|------|--------|--------------|----------|
| **Standalone** | Default | None (SQLite) | Dev, single node |
| **PostgreSQL** | `postgres.url` | PostgreSQL | Single node, durable |
| **Multi-pod** | `postgres.url` + `redis.url` | PostgreSQL + Redis | Production HA |

### Docker Compose

```bash
# Standalone (SQLite)
docker compose --profile standalone up -d

# PostgreSQL
docker compose --profile postgres up -d

# Multi-pod
docker compose --profile multinode up -d
```

### Kubernetes (Helm)

```bash
helm install recall-mcp-gateway charts/recall-mcp-gateway \
  --set tls.enabled=true \
  --set ingress.host=proxy.company.com
```

See [docs/client/deployment-guide.md](docs/client/deployment-guide.md) for detailed deployment instructions.

## Configuration

The gateway uses a YAML config file for infrastructure settings. Servers, agents, and rules are managed via the dashboard or admin API after first startup.

See `configs/standalone.yaml` for a fully commented example, or the [.env.example](.env.example) for environment variable overrides.

Key settings:

```yaml
proxy:
  port: 8090
  dashboard: true
  log_level: info              # debug, info, warn, error
  rate_limit: 100              # global per-IP rate limit (req/s)

auth_server:                   # optional — enables JWT auth
  issuer: "https://auth.example.com"
  audience: "http://localhost:8090/mcp"
  jwks_url: "https://auth.example.com/.well-known/jwks.json"
```

## Admin API

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/healthz` | GET | Liveness probe |
| `/readyz` | GET | Readiness probe |
| `/admin/v1/status` | GET | Server health, alerts, baselines |
| `/admin/v1/rules` | GET/POST | List/create rules |
| `/admin/v1/rules/:index` | DELETE | Remove a rule |
| `/admin/v1/agents` | GET/POST | List/create agents |
| `/admin/v1/agents/:id` | PUT/DELETE | Update/delete agent |
| `/admin/v1/servers/add` | POST | Add MCP server |
| `/admin/v1/servers/remove/:name` | DELETE | Remove MCP server |
| `/admin/v1/approvals` | GET | List pending approvals |
| `/admin/v1/approvals/:id/approve` | GET/POST | Approve (signed URL) |
| `/admin/v1/approvals/:id/deny` | GET/POST | Deny (signed URL) |
| `/admin/v1/recommendations` | GET | Governance recommendations |
| `/admin/v1/insights` | GET | Compliance score + details |
| `/admin/v1/audit/recent` | GET | Recent audit entries |
| `/admin/v1/audit/verify` | GET | Verify audit hash chain |
| `/admin/v1/config/runtime` | PUT | Hot-reload config |
| `/metrics` | GET | Prometheus metrics |

The dashboard also exposes `/ws/audit` (SSE live feed) when `dashboard: true` is configured.

## Connected Mode (Recall Platform)

The gateway can optionally connect to the [Recall Platform](https://controlis.ai) for centralized governance across multiple gateways:

- Centralized policy management
- Fleet-wide compliance scoring
- Aggregated audit across all gateways
- Console UI for policy authoring

Connected mode activates when `engine_url` is configured. All standalone features continue to work — platform integration is additive.

## Build

```bash
make build          # local binary
make test           # unit tests
make test-all       # unit + integration tests
make lint           # golangci-lint
make release        # cross-compile all platforms
make docker-build   # Docker image
make helm-lint      # Helm chart validation
make build-full     # React dashboard + Go binary
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, coding standards, and PR process.

## Security

To report a security vulnerability, see [SECURITY.md](SECURITY.md).

## License

[AGPL-3.0](LICENSE). Copyright 2026 ControlisAI.

All standalone features are free and open source. The [Recall Platform](https://controlis.ai) (centralized governance, fleet management) is a separate commercial product.
