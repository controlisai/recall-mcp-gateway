# Recall MCP Gateway — Deployment Guide

## Overview

The gateway sits between your AI agents (Claude, Cursor, LangChain, n8n) and your MCP tool servers, adding visibility, rules, and control to every tool call.

```
AI Agent → Recall MCP Gateway (:8090) → MCP Tool Servers
                    ↓
              Dashboard + Audit
```

## Prerequisites

- **Docker** 20+ or **Kubernetes** 1.24+ with Helm 3, or any Linux/macOS host for binary install
- **Port 8090** available (configurable)
- **10MB disk** minimum for the binary; **1GB** recommended for audit storage
- **Network:** agents must reach the gateway on port 8090. The gateway must reach your MCP tool servers (HTTP URLs or local stdio commands).

### Network diagram

```
                    ┌─────────────────────────────────────┐
                    │        Your Infrastructure          │
                    │                                     │
  AI Agents ──────► │  Recall MCP Gateway (:8090)  ──────►│──► MCP Servers
  (Claude, Cursor,  │      │                              │    (DB, Slack, GitHub, etc.)
   LangChain, n8n)  │      ├── /dashboard  (browser)      │
                    │      ├── /metrics    (Prometheus)    │
                    │      └── /healthz    (load balancer) │
                    └─────────────────────────────────────┘
                              │ (optional)
                              ▼
                    Recall Platform (cloud)
                    engine + console-api
```

---

## Option 0: Binary Install (Linux / macOS)

### Install

```bash
# macOS / Linux
curl -fsSL https://get.controlis.ai/proxy | sh

# Homebrew
brew install controlisai/tap/recall-mcp-gateway
```

### Run

```bash
# With config file
recall-mcp-gateway --config config.yaml

# Dashboard-only mode (add servers via UI)
recall-mcp-gateway
```

### Run as systemd service (Linux)

```ini
# /etc/systemd/system/recall-mcp-gateway.service
[Unit]
Description=Recall MCP Gateway
After=network.target

[Service]
Type=simple
User=recall-mcp-gateway
ExecStart=/usr/local/bin/recall-mcp-gateway --config /etc/recall-mcp-gateway/config.yaml
Restart=on-failure
RestartSec=5
Environment=RECALL_LOG_LEVEL=info
Environment=RECALL_AUDIT_PATH=/var/lib/recall-mcp-gateway/audit.db

[Install]
WantedBy=multi-user.target
```

```bash
sudo useradd -r -s /bin/false recall-mcp-gateway
sudo mkdir -p /etc/recall-mcp-gateway /var/lib/recall-mcp-gateway
sudo cp config.yaml /etc/recall-mcp-gateway/
sudo chown -R recall-mcp-gateway: /var/lib/recall-mcp-gateway
sudo systemctl enable --now recall-mcp-gateway
```

### Deployment Modes

The gateway supports three deployment modes, selected by config:

| Mode | Config | Store | Redis | Use Case |
|---|---|---|---|---|
| **Standalone (SQLite)** | no `postgres`, no `redis` | local `gateway.db` | none | Dev, single node, zero dependencies |
| **Single-pod (PostgreSQL)** | `postgres.url` set | PostgreSQL | none | Single node with PG durability/backups |
| **Multi-pod (HA)** | `postgres.url` + `redis.url` | PostgreSQL (shared) | rate limiting + SSE relay | Production high-availability |

**SQLite** is single-pod only — no cross-pod sync. For multi-pod deployments, PostgreSQL is required.

### State storage

All runtime state (servers, agents, rules, audit, approvals, baselines) is stored in a single database:

- **SQLite mode:** `~/.recall-mcp-gateway/gateway.db` (single file)
- **PostgreSQL mode:** shared database at the configured `postgres.url`

| What | Backup? |
|---|---|
| Database (gateway.db or PostgreSQL) | Yes — contains all audit, rules, agents, approvals |
| `config.yaml` | Yes — infrastructure settings (proxy, auth_server, postgres, redis) |
| TLS certs / signing keys | Yes — if configured |

> **Note:** `config.yaml` sections for `servers`, `agents`, and `rules` are **seed data** — they populate the database on first startup only. After that, all changes go through the dashboard or admin API. Editing YAML after first run has no effect on these sections.

---

## Option 1: Docker (Recommended)

Same procedure for all three deployment modes — only the config file and profile name change.

### Step 1: Pull the image

```bash
docker pull controlisai/recall-mcp-gateway
```

### Step 2: Extract deployment files

```bash
docker run --rm --entrypoint sh controlisai/recall-mcp-gateway \
  -c "tar -cf - /app/configs/ /app/docker-compose.yaml" | tar -xf - --strip-components=1
```

This creates:

```
configs/
  standalone.yaml     ← SQLite (zero deps)
  postgres.yaml       ← PostgreSQL (production)
  multinode.yaml      ← PostgreSQL + Redis (HA)
docker-compose.yaml   ← compose with profiles
```

### Step 3: Edit the config for your mode

Pick the config that matches your deployment mode and edit it:

```bash
vim configs/standalone.yaml   # or postgres.yaml or multinode.yaml
```

**What to change:**

**Required (all modes):**

| Setting | Where | What to do |
|---|---|---|
| `proxy.dashboard_pass_hash` | All configs | **Change immediately.** Default is `admin`. See password hash section below. |
| `proxy.trusted_proxies` | Behind load balancer | CIDR list of reverse proxy IPs (e.g., `["10.0.0.0/8"]`). Empty = never trust X-Forwarded-For. |

**Required (PostgreSQL / multi-pod):**

| Setting | Where | What to do |
|---|---|---|
| `postgres.url` | `postgres.yaml`, `multinode.yaml` | Your PostgreSQL connection string |
| `redis.url` | `multinode.yaml` only | Your Redis connection string |

**Security (recommended for production):**

| Setting | Default | What to do |
|---|---|---|
| `proxy.tls_cert` / `proxy.tls_key` | (disabled) | Enable HTTPS — paths to TLS certificate and key |
| `proxy.allowed_origins` | (allow all) | Restrict to your app's origin URLs for CORS |
| `proxy.rate_limit` | `100` | Per-IP request rate limit (req/s). Tune for your load. |
| `proxy.max_body_size` | `10MB` | Max request body size |

**Auth server (external JWT — optional):**

| Setting | When needed | What to do |
|---|---|---|
| `auth_server.issuer` | Using external OAuth/OIDC | Your Authorization Server URL |
| `auth_server.audience` | Using external OAuth/OIDC | Expected JWT audience (typically gateway's MCP URL) |
| `auth_server.jwks_url` | Using external OAuth/OIDC | JWKS endpoint (auto-discovered from issuer if omitted) |
| `auth_server.approval_secret` | Consent/approval flows | 32+ char secret for request state encryption |
| `auth_server.signing_key_file` | Token exchange (delegation) | Path to PEM-encoded RSA private key |

**Approval webhooks (optional):**

| Setting | When needed | What to do |
|---|---|---|
| `proxy.approval.admin_key` | Admin approval API | Shared secret for approval endpoints |
| `proxy.approval.slack_signing_secret` | Slack integration | Slack app signing secret for interactive callbacks |

**Seed data (optional, first run only):**

| Setting | Where | What to do |
|---|---|---|
| `servers` | All configs | Pre-configure MCP servers (or add via dashboard later) |
| `agents` | All configs | Pre-configure agents with `token_hash` (or create via API later) |
| `rules` | All configs | Pre-configure governance rules (or add via dashboard later) |

### Step 4: Run

```bash
# Standalone (SQLite)
docker compose --profile standalone up -d

# PostgreSQL (single-pod)
docker compose --profile postgres up -d

# Multi-pod HA (PostgreSQL + Redis — 2 pods for testing)
docker compose --profile multinode up -d
```

Dashboard: http://localhost:8090/dashboard/

> **Scaling beyond 2 pods:** The Docker Compose multinode profile runs 2 gateway pods with fixed port mappings — suitable for testing cross-pod sync. For production multi-pod deployments with 3+ pods and load balancing, use **Kubernetes + Helm**:
>
> ```bash
> helm install recall-mcp-gateway oci://registry-1.docker.io/controlisai/recall-mcp-gateway \
>   --set replicaCount=5 \
>   --set postgres.enabled=true \
>   --set postgres.url="postgres://user:pass@pg:5432/recall_gateway" \
>   --set redis.enabled=true \
>   --set redis.url="redis://redis:6379"
> ```

### Stop / restart

```bash
# Stop
docker compose --profile <mode> down

# Stop and remove data
docker compose --profile <mode> down -v

# Restart (state persists)
docker compose --profile <mode> restart
```

### Generate a password hash

```bash
# Using htpasswd (Apache utilities)
htpasswd -nbBC 10 "" yourpassword | cut -d: -f2

# Using Python
python3 -c "import bcrypt; print(bcrypt.hashpw(b'yourpassword', bcrypt.gensalt()).decode())"

# Using Docker
docker run --rm python:3-alpine sh -c "pip install bcrypt && \
  python3 -c \"import bcrypt; print(bcrypt.hashpw(b'yourpassword', bcrypt.gensalt()).decode())\""
```

---

## Option 2: Kubernetes (Helm) — Production Multi-Pod

Helm is for **production Kubernetes deployments** with PostgreSQL + Redis + multiple replicas. For dev/single-pod, use Docker Compose instead.

### Install

```bash
helm install recall-mcp-gateway oci://registry-1.docker.io/controlisai/recall-mcp-gateway \
  --set postgres.url="postgres://user:pass@pg:5432/recall_gateway?sslmode=require" \
  --set redis.url="redis://redis:6379" \
  --set proxy.dashboardPassHash='$2a$10$...'
```

### Key Helm values

```yaml
replicaCount: 3                       # number of gateway pods

proxy:
  dashboardPassHash: "$2a$10$..."     # REQUIRED — bcrypt hash of password
  rateLimit: 100
  logLevel: "info"

# PostgreSQL — REQUIRED (shared durable store for all pods)
postgres:
  url: "postgres://user:pass@pg:5432/recall_gateway?sslmode=require"
  maxConns: 25

# Redis — REQUIRED (cross-pod rate limiting + SSE relay)
redis:
  url: "redis://redis:6379"

# Audit tuning
audit:
  retention: 30d
  maxEntries: 100000

# Approval tuning
approval:
  ttl: "5m"

# TLS
tls:
  enabled: true
  existingSecret: "proxy-tls"         # k8s TLS secret

# External JWT auth (optional)
# authServer:
#   enabled: true
#   issuer: "https://auth.example.com"
#   audience: "http://proxy:8090/mcp"
```

### GitOps (ArgoCD / Flux)

The chart is a standard Helm chart — works with any GitOps tool out of the box. ArgoCD example:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: recall-mcp-gateway
spec:
  source:
    chart: recall-mcp-gateway
    repoURL: registry-1.docker.io/controlisai
    targetRevision: "1.0.0"
    helm:
      values: |
        replicaCount: 3
        postgres:
          url: "postgres://user:pass@pg:5432/recall_gateway"
        redis:
          url: "redis://redis:6379"
        proxy:
          dashboardPassHash: "$2a$10$..."
  destination:
    server: https://kubernetes.default.svc
    namespace: recall
```

The chart includes `values.schema.json` for validation — ArgoCD UI will show a form with required fields and valid options.

### Security features (enabled by default)

- `readOnlyRootFilesystem: true`
- `runAsNonRoot: true`
- `capabilities.drop: ["ALL"]`
- `allowPrivilegeEscalation: false`
- Startup probe: 5 minute budget for slow connections
- PodDisruptionBudget: created when `replicaCount > 1`

### Prometheus integration

Prometheus annotations are added automatically:

```yaml
prometheus.io/scrape: "true"
prometheus.io/port: "8090"
prometheus.io/path: "/metrics"
```

Disable with `proxy.metricsEnabled: false` if you don't want the endpoint exposed.

---

## Configuration Reference

### Config file (YAML)

```yaml
proxy:
  port: 8090
  dashboard: true

  # Authentication
  dashboard_user: admin
  dashboard_pass_hash: "$2a$10$..."   # bcrypt hash — NOT plaintext

  # Limits
  max_body_size: "10MB"               # max request body (KB, MB, GB)
  rate_limit: 100                     # global HTTP req/s per IP (0=disabled)

  # Observability
  log_level: "info"                   # debug, info, warn, error
  metrics_enabled: true               # Prometheus /metrics endpoint

  # Connected mode (Recall Platform)
  engine_url: ""                      # engine /decide endpoint
  console_api_url: ""                 # console-api for auto-registration
  api_key: ""                         # SDK API key (scoped to product)
  skip_local_rules: true              # skip proxy rules when engine connected
  product: "my-gateway"               # informational label

  # Audit
  audit:
    retention: 30d
    max_entries: 100000

  # TLS
  tls_cert: /path/to/cert.pem
  tls_key: /path/to/key.pem

# PostgreSQL (optional — omit for standalone SQLite mode)
# When set, the gateway uses PostgreSQL instead of SQLite for all state.
# Required for multi-pod HA deployments.
postgres:
  url: "postgres://user:pass@host:5432/recall_gateway?sslmode=require"
  max_conns: 25                       # connection pool size (default 25)

# Redis (optional — only needed for multi-pod deployments)
# Used for cross-pod rate limiting and live dashboard SSE relay.
# NOT used for config sync — that uses PostgreSQL LISTEN/NOTIFY.
redis:
  url: "redis://redis:6379"

# MCP servers (seed data — populates DB on first startup only)
# After first run, manage servers via dashboard or admin API.
servers:
  - name: database
    transport: http
    url: http://db-mcp:3000/mcp
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]

# Agent service accounts (seed data — populates DB on first startup only)
agents:
  - id: support-bot
    token_hash: "sha256hexhash..."    # SHA-256 of bearer token
    allowed_servers: [database]
    org_id: "my-org"                  # optional: org context for platform

# Governance rules (seed data — populates DB on first startup only)
rules:
  - tool: "write_file"
    action: require_approval
    reason: "File writes need review"
  - tool: "db_*_delete"
    action: block
    reason: "No destructive database ops"
```

### Agent token setup

Agents authenticate with `Authorization: Bearer <token>`. The config stores the SHA-256 hash, not the raw token.

**Dashboard-created agents:** Token shown once on creation — copy it immediately.

**Config file agents:**
```bash
# Generate a token
TOKEN=$(openssl rand -hex 32)
echo "Token: rp_$TOKEN"

# Compute SHA-256 hash for config
echo -n "rp_$TOKEN" | sha256sum | cut -d' ' -f1
```

Put the hash in your config:
```yaml
agents:
  - id: my-agent
    token_hash: "the-sha256-hash-here"
```

Give the `rp_...` token to your agent. It sends `Authorization: Bearer rp_...` on every request.

---

## MCP Server Authentication

Most MCP servers require authentication. The gateway supports 5 auth methods for upstream connections:

### Bearer Token

```yaml
servers:
  - name: github-mcp
    transport: http
    url: https://api.github.com/mcp
    auth:
      type: bearer
      token: "${GITHUB_TOKEN}"          # env var expansion supported
```

### Basic Auth

```yaml
servers:
  - name: jira-mcp
    transport: http
    url: https://jira.company.com/mcp
    auth:
      type: basic
      username: "admin"
      password: "${JIRA_PASSWORD}"
```

### Custom Headers

```yaml
servers:
  - name: internal-api
    transport: http
    url: https://api.internal.com/mcp
    auth:
      type: headers
      headers:
        X-API-Key: "${API_KEY}"
        X-Tenant-Id: "my-tenant"
```

### OAuth 2.0 Client Credentials

```yaml
servers:
  - name: servicenow-mcp
    transport: http
    url: https://instance.service-now.com/mcp
    auth:
      type: oauth_client_credentials
      token_url: "https://instance.service-now.com/oauth_token.do"
      client_id: "${SN_CLIENT_ID}"
      client_secret: "${SN_CLIENT_SECRET}"
      scopes: ["useraccount"]
```

The gateway auto-caches tokens, refreshes 30 seconds before expiry, and retries on 401.

### mTLS (Mutual TLS)

```yaml
servers:
  - name: secure-mcp
    transport: http
    url: https://secure.company.com/mcp
    auth:
      type: mtls
      client_cert: /etc/certs/client.pem
      client_key: /etc/certs/client-key.pem
      ca_cert: /etc/certs/ca.pem         # optional: custom CA
```

### Stdio Servers (Local Processes)

Stdio servers don't need auth — they run as local subprocesses:

```yaml
servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
    env:
      NODE_ENV: production
```

> **Note:** Stdio servers require the command to be available in the container/host PATH. In Docker, install Node.js tools in a custom image or use HTTP transport instead.

---

## Rule Configuration Reference

Rules define governance for tool calls. Evaluated in order — first match wins.

### Rule fields

| Field | Required | Description |
|---|---|---|
| `tool` | Yes | Tool name or glob pattern (`*`, `db_*`, `*_delete`) |
| `action` | Yes | `allow`, `block`, `require_approval`, `hide` |
| `reason` | No | Human-readable reason (shown to agent on block/approval) |
| `agents` | No | List of agent IDs this rule applies to (empty = all) |
| `server` | No | Server name this rule applies to (empty = all) |
| `classification` | No | Tool classification: `DESTRUCTIVE`, `EXTERNAL_COMM`, `CODE_EXECUTION`, etc. |
| `rate_limit` | No | Rate limit: `"5/minute"`, `"100/hour"`, `"10/second"` |
| `notify` | No | Webhook URL to notify when this rule triggers |
| `schedule` | No | Time-based activation (see below) |
| `param_blocks` | No | Block based on argument content (see below) |

### Actions

| Action | Behavior |
|---|---|
| `allow` | Tool call proceeds (use to whitelist before a later block rule) |
| `block` | Tool call rejected — agent receives error with reason |
| `require_approval` | Tool call held until a human approves/denies in the dashboard |
| `hide` | Tool removed from the agent's tool list (agent doesn't know it exists) |

### Glob patterns

```yaml
rules:
  - tool: "*"                   # matches all tools
  - tool: "db_*"                # matches db_query, db_insert, db_delete
  - tool: "*_delete"            # matches user_delete, record_delete
  - tool: "send_*"             # matches send_email, send_slack
```

### Rate limiting

```yaml
rules:
  - tool: "send_email"
    action: allow
    rate_limit: "5/minute"          # max 5 per minute per agent
  - tool: "db_query"
    action: allow
    rate_limit: "100/hour"
```

When exceeded, the tool call is blocked with a `rate_limited` decision until the window resets.

### Schedule-based rules

```yaml
rules:
  # Block all tools outside business hours
  - tool: "*"
    action: block
    reason: "Outside business hours"
    schedule:
      cron: "* 9-17 * * 1-5"       # Mon-Fri 9am-5pm
      timezone: "America/New_York"   # optional (default: UTC)
      inside: false                  # block when OUTSIDE this window
```

### Parameter blocking

```yaml
rules:
  - tool: "db_query"
    action: block
    reason: "Destructive SQL detected"
    param_blocks:
      - param: "query"
        pattern: "DELETE|DROP|TRUNCATE"
        reason: "Destructive SQL operation"
```

### Agent-scoped rules

```yaml
rules:
  # Only the admin agent can write files
  - tool: "write_file"
    action: allow
    agents: ["admin-agent"]
  - tool: "write_file"
    action: block
    reason: "Only admin-agent can write files"
```

### Complete example

```yaml
rules:
  # Whitelist read operations
  - tool: "db_query"
    action: allow
    rate_limit: "100/hour"

  # Require approval for writes
  - tool: "db_insert"
    action: require_approval
    reason: "Database writes need review"
    notify: "https://hooks.slack.com/services/..."

  # Block destructive operations
  - tool: "db_*_delete"
    action: block
    reason: "Destructive database operations are prohibited"

  # Block outside business hours
  - tool: "*"
    action: block
    reason: "Tool calls blocked outside business hours"
    schedule:
      cron: "* 9-17 * * 1-5"
      inside: false

  # Hide internal tools from external agents
  - tool: "internal_*"
    action: hide
    agents: ["external-bot"]
```

---

## Webhook Configuration

Webhooks notify external systems (Slack, Teams, PagerDuty) when events occur.

### Two webhook types

| Type | Triggers When | Payload |
|---|---|---|
| **Approval webhook** | A tool call requires human approval | Agent ID, tool, arguments, approve/deny URLs |
| **Alert webhook** | Anomaly detected (rate spike, PII, off-hours) | Alert type, severity, agent, tool, baseline deviation |

### Configure via dashboard

Go to **Settings → Notifications**. Enter webhook URLs and click Save.

> Note: Dashboard-saved URLs are runtime-only. Add to YAML config for persistence across restarts.

### Configure via YAML

```yaml
# In config.yaml — no specific field; use the API or dashboard
# Webhook URLs are set at runtime via:
#   PUT /admin/v1/config/webhook
#   {"approvalWebhookUrl": "...", "alertWebhookUrl": "..."}
```

### Configure via API

```bash
# Save webhook URLs
curl -X PUT http://localhost:8090/admin/v1/config/webhook \
  -H "Content-Type: application/json" \
  -d '{"approvalWebhookUrl":"https://hooks.slack.com/...", "alertWebhookUrl":"https://hooks.slack.com/..."}'

# Test delivery
curl -X POST http://localhost:8090/admin/v1/config/webhook-test \
  -H "Content-Type: application/json" \
  -d '{"url":"https://hooks.slack.com/...", "type":"approval"}'

# Read current URLs
curl http://localhost:8090/admin/v1/config/webhook
```

### Slack example payload (approval)

```json
{
  "approvalId": "apr_abc123",
  "agentId": "support-bot",
  "tool": "send_email",
  "arguments": {"to": "customer@example.com", "subject": "..."},
  "approveUrl": "http://gateway:8090/admin/v1/approvals/apr_abc123/approve",
  "denyUrl": "http://gateway:8090/admin/v1/approvals/apr_abc123/deny",
  "expiresAt": "2026-10-22T12:30:00Z"
}
```

---

## Dashboard Overview

After login, the dashboard has 5 tabs:

### Monitor
- **Live Feed** — real-time stream of every tool call with decision, latency, signals
- **Pending Approvals** — tool calls waiting for human review (from `require_approval` rules)
- **Alerts & Signals** — anomaly detection: rate spikes, PII exposure, off-hours activity, credential leaks

### Control
- **Servers & Tools** — add/edit/remove MCP servers, view tool risk profiles, connection health
- **Agents** — create agents with tokens, set server access permissions
- **Rules** — create/edit governance rules (block, approve, rate limit, schedule)

### Insights
- **Compliance Preview** — approximate compliance score (0-1000) based on rule coverage, signal rate, error rate
- **Detected Sequences** — auto-discovered tool call patterns (suspicious sequences flagged)
- **Governance Insights** — recommendations for improving governance posture

### Settings
- **Connect Your Agent** — proxy endpoint URL and framework connection snippets
- **Platform Connection** — connect to Recall Platform for full governance
- **Notifications** — webhook URLs for approvals and alerts
- **Audit Storage** — storage info, retention, max entries, purge

### Docs
- **Quick Start, Agents, Rules, Servers, Approvals, Platform, API Reference, Framework Guide** — all accessible in-app

---

## Logging

The gateway outputs structured JSON logs to stderr.

### Log format

```json
{"time":"2026-10-22T14:30:00Z","level":"INFO","msg":"tool call blocked","component":"proxy","tool":"db_delete","agent":"support-bot"}
```

### Log levels

| Level | What's logged |
|---|---|
| `debug` | Tool evaluation details, health check pings, baseline updates |
| `info` | Connections, rule matches, state changes (default) |
| `warn` | Blocked calls, auth failures, circuit breaker trips |
| `error` | Upstream failures, engine unavailable, database errors |

### Docker log rotation

The production Docker Compose includes log rotation (50MB × 3 files). For `docker run`, add:

```bash
docker run -d \
  --log-driver json-file \
  --log-opt max-size=50m \
  --log-opt max-file=3 \
  ...
```

### Log aggregation

Logs are JSON on stderr — pipe to any log aggregator:

```bash
# Datadog
docker logs recall-mcp-gateway 2>&1 | datadog-agent ...

# ELK
docker logs recall-mcp-gateway 2>&1 | filebeat ...

# CloudWatch (use awslogs driver)
docker run --log-driver=awslogs --log-opt awslogs-group=recall-mcp-gateway ...
```

---

## Backup and Restore

### What to back up

**SQLite mode (standalone):**

| File | Location | Priority |
|---|---|---|
| `gateway.db` | `~/.recall-mcp-gateway/gateway.db` or `/data/gateway.db` (Docker) | **Critical** — all state (audit, agents, rules, servers, approvals, baselines) |
| `config.yaml` | Your config path | High — infrastructure settings |
| TLS certs / signing keys | As configured | High — if using TLS or token exchange |

**PostgreSQL mode:** Back up your PostgreSQL database using standard `pg_dump`. The `config.yaml` is the only local file to preserve.

### Backup script (SQLite mode)

```bash
#!/bin/bash
BACKUP_DIR=/backups/recall-mcp-gateway/$(date +%Y%m%d)
mkdir -p $BACKUP_DIR

# Docker
docker cp recall-mcp-gateway:/data/gateway.db $BACKUP_DIR/

# Binary install
cp ~/.recall-mcp-gateway/gateway.db $BACKUP_DIR/
cp /etc/recall-mcp-gateway/config.yaml $BACKUP_DIR/
```

### Backup (PostgreSQL mode)

```bash
pg_dump -U recall recall_gateway > /backups/recall-mcp-gateway-$(date +%Y%m%d).sql
```

### Restore

**SQLite:** Stop the gateway, copy `gateway.db` back, restart.

**PostgreSQL:** Restore with `psql recall_gateway < backup.sql` — the gateway picks up state on next startup.

---

## Updating the Gateway

### Docker

```bash
docker pull controlisai/recall-mcp-gateway:latest
docker stop recall-mcp-gateway && docker rm recall-mcp-gateway
# Re-run with same volumes — state persists on the volume
docker run -d --name recall-mcp-gateway -p 8090:8090 \
  -v recall-data:/data \
  controlisai/recall-mcp-gateway:latest
```

### Helm

```bash
helm upgrade recall-mcp-gateway ./charts/recall-mcp-gateway \
  --set image.tag=v1.1.0
```

The PVC preserves audit data and state across upgrades. Schema migrations run automatically on startup.

### Binary

```bash
curl -fsSL https://get.controlis.ai/proxy | sh    # overwrites binary
sudo systemctl restart recall-mcp-gateway                # if using systemd
```

`gateway.db` (SQLite mode) or the PostgreSQL database is preserved across updates. All state carries over.

---

## Example Configs

The gateway ships with example configs for common scenarios:

| Config | Mode | Use Case |
|---|---|---|
| `configs/standalone.yaml` | Standalone SQLite | Single pod, zero dependencies |
| `configs/postgres.yaml` | Single-pod PG | PostgreSQL for durability/backups |
| `configs/multinode.yaml` | Multi-pod HA | PG + Redis, 2 pods (test). Use Helm for 3+ pods. |

All three configs are included in the Docker image. Extract with:
```bash
docker run --rm --entrypoint sh controlisai/recall-mcp-gateway \
  -c "cat /app/configs/standalone.yaml"
```

---

## First Login

1. Open http://your-host:8090/dashboard/
2. Default credentials: **admin / admin**
3. Change the password immediately:
   - Generate a new bcrypt hash (see above)
   - Update `dashboard_pass_hash` in config or `RECALL_DASHBOARD_PASS_HASH` env var
   - Restart the gateway

---

## Health Checks

| Endpoint | Purpose | Response |
|---|---|---|
| `GET /healthz` | Liveness | `{"status":"ok"}` |
| `GET /readyz` | Readiness (servers connected) | `{"status":"ready"}` |
| `GET /metrics` | Prometheus metrics | Prometheus text format |

---

## Upgrading to Recall Platform

The gateway is a standalone product that optionally connects to the Recall Platform for full governance:

| Capability | Gateway (standalone) | Platform (connected) |
|---|---|---|
| Tool call visibility | Yes | Yes |
| Local rules (block/approve) | Yes | Yes (or skip via `skip_local_rules`) |
| PII/credential detection | Yes | Yes |
| Behavioral baselines | Yes (EWMA) | Yes (drift detection) |
| Compliance scoring | Preview (approximate) | Full (EWMA, outcome-aware) |
| Policy management | YAML rules | Console UI + OPA policies |
| Multi-org governance | No | Yes (org → profile → policy) |
| Audit exports | JSONL/SQLite | PDF/CSV compliance reports |

To connect:
1. Create a product in the Recall console
2. Generate an SDK API key
3. Add to gateway config:
   ```yaml
   proxy:
     engine_url: "http://engine:8084"
     console_api_url: "http://console-api:8082"
     api_key: "cai_rcl_sdk_prod_..."
   ```
4. Restart — the gateway now defers governance decisions to the platform engine

---

## Troubleshooting

### Gateway won't start
- Check config YAML syntax: `recall-mcp-gateway --config config.yaml`
- Check port 8090 is available: `lsof -i :8090`

### Can't login to dashboard
- Default: admin / admin
- If password hash is set, verify it: the hash must be a valid bcrypt hash (starts with `$2a$` or `$2b$`)
- Env var `RECALL_DASHBOARD_PASS_HASH` overrides config file

### MCP servers not connecting
- Check server URLs are reachable from the gateway container
- For stdio servers: ensure the command is available in the container PATH
- Check `/admin/v1/status` for connection status and circuit breaker state

### Audit database growing large
- Default retention: 30 days, max 100K entries
- Adjust via config: `audit.retention: 7d` and `audit.max_entries: 50000`
- Manual purge: `DELETE /admin/v1/audit/purge`

### Logs too verbose
- Set `log_level: warn` in config or `RECALL_LOG_LEVEL=warn` env var
- Logs are JSON to stderr — pipe to your log aggregator
