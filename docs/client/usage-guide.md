# Recall MCP Gateway — Usage Guide

After installation ([Deployment Guide](deployment-guide.md)), this guide covers day-to-day usage: managing agents, writing rules, approval workflows, and connecting frameworks.

> **Seed data vs runtime:** The `servers`, `agents`, and `rules` sections in `config.yaml` are seed data — they populate the database on **first startup only**. After that, manage everything via the dashboard or admin API.

---

## Agents

Agents are the governed actors. Each agent has an identity, optional bearer token, archetype, and server access permissions.

### Agent recognition

| Method | How | Best for |
|---|---|---|
| Bearer token | Pre-configured SHA-256 hash in dashboard or config | Company deployments |
| JWT (external AS) | RS256/ES256 validated via JWKS endpoint | Enterprise SSO, OAuth 2.1 |
| Anonymous | No auth configured | Local development |

### Archetypes

Every agent has an **archetype** that determines its governance flow. Set via the `archetype` field.

| Archetype | Description | Governance | Example clients |
|---|---|---|---|
| `autonomous` | Machine-to-machine. No human in the loop during execution. | Admin reviews via dashboard or webhook | CI/CD pipelines, data ingestion bots, scheduled tasks |
| `user_embedded` | Human user is present and can respond to prompts. | Consent form shown to user via MCP elicitation | Claude Desktop, Cursor, IDE extensions |
| `delegated` | Acting on behalf of another user (delegation chain). | Delegated user reviews via webhook | ServiceNow workflows, n8n automations acting for a user |

**Default:** Auto-registered agents get `autonomous` (most restrictive — requires admin approval for governed tools).

### Creating agents

**Via Dashboard:** Control tab → Agents → + Create Agent. Check "Generate token" to get a Bearer token.

**Via API:**

```bash
curl -X POST http://localhost:8090/admin/v1/agents \
  -H "Content-Type: application/json" \
  -d '{"id": "my-agent", "generateToken": true, "allowedServers": ["database"]}'
```

Response includes the token (shown once):

```json
{"status": "created", "agentId": "my-agent", "token": "rp_a1b2c3..."}
```

**Token is shown once.** The gateway stores only the SHA-256 hash and cannot retrieve the plaintext token later.

**Via config (seed data):**

```yaml
agents:
  - id: claude-desktop
    archetype: user_embedded
    token_hash: "sha256hexhash..."    # echo -n "rp_yourtoken" | sha256sum | cut -d' ' -f1
    allowed_servers: [database, filesystem]

  - id: pipeline-bot
    archetype: autonomous
    token_hash: "sha256hexhash..."
    allowed_servers: [database]
    blocked_tools: ["db_*_delete"]    # glob patterns — tools this agent cannot call
    hidden_tools: ["internal_*"]      # hidden from ListTools but still callable if known
    expires_at: "2027-01-01T00:00:00Z"  # token expiry (empty = never)

  - id: servicenow-agent
    archetype: delegated
    token_hash: "sha256hexhash..."
```

### Token management

| Action | How |
|---|---|
| Generate hash | `echo -n "rp_yourtoken" \| sha256sum \| cut -d' ' -f1` |
| Rotate token | `POST /admin/v1/agents/{id}/rotate-token` — returns new token, old one immediately invalidated |
| Dashboard | Expand agent → copy/rotate icons |

### Server access control

Each agent can be restricted to specific MCP servers via `allowedServers`. If empty, the agent can access all servers.

```bash
curl -X PUT http://localhost:8090/admin/v1/agents/my-agent/servers \
  -H "Content-Type: application/json" \
  -d '{"allowedServers": ["filesystem", "database"]}'
```

Or use the toggle switches in the agent detail view in the dashboard.

---

## Rules

Rules control what agents can do. **First-match-wins** evaluation — the first matching rule determines the action.

### Rule model

| Rule type | Tool | Server | Agent | Actions available |
|---|---|---|---|---|
| Specific | exact name (e.g., `delete_record`) | required | optional | All actions |
| Default | `*` | all | all | Allow or Block only |

### Actions

| Action | Effect | Requires agent? |
|---|---|---|
| `allow` | Tool call proceeds | No |
| `block` | Tool call rejected with reason | No |
| `rate_limit` | Allow up to N calls per window | No |
| `hide` | Tool hidden from agent's ListTools | No |
| `require_approval` | Requires human review before execution | Yes |
| `require_consent` | MCP consent form shown to user (user_embedded only) | Yes |

### YAML examples

```yaml
rules:
  # Allow with rate limit
  - tool: "db_query"
    server: database
    action: allow
    rate_limit: "100/hour"

  # Block destructive operations
  - tool: "db_*_delete"
    action: block
    reason: "Destructive database operations are prohibited"

  # Require admin approval for autonomous agent
  - tool: "send_email"
    server: slack
    action: require_approval
    agents: ["pipeline-bot"]
    reason: "External communication needs admin approval"
    notify: "https://hooks.slack.com/services/..."

  # Consent form for user-embedded agent
  - tool: "delete_record"
    server: database
    action: require_consent
    agents: ["claude-desktop"]
    consent_form:
      title: "Confirm Deletion"
      description: "This will permanently delete the record. Are you sure?"

  # Hide internal tools from specific agents
  - tool: "internal_*"
    action: hide
    agents: ["external-bot"]

  # Default: allow everything else
  - tool: "*"
    action: allow
```

### Per-archetype actions

A single rule can specify different actions per archetype:

```yaml
  - tool: "write_file"
    server: filesystem
    actions:
      autonomous: require_approval
      user_embedded: require_consent
      delegated: require_approval
    reason: "File writes need human oversight"
```

### Glob patterns

| Pattern | Matches |
|---|---|
| `*` | All tools |
| `db_*` | `db_query`, `db_insert`, `db_delete` |
| `db_*_delete` | `db_user_delete`, `db_record_delete` |

### Schedule-based rules

Block or allow tools based on time windows:

```yaml
  - tool: "*"
    action: block
    reason: "Tool calls blocked outside business hours"
    schedule:
      cron: "* 9-17 * * 1-5"      # Mon-Fri 9am-5pm
      timezone: "America/New_York"
      inside: false                 # block when OUTSIDE the window
```

### Parameter blocking

Block calls where arguments match a regex pattern:

```yaml
  - tool: "db_query"
    action: block
    reason: "DROP/DELETE SQL not allowed"
    param_blocks:
      - param: "query"
        pattern: "(?i)(DROP|DELETE|TRUNCATE)"
        reason: "Destructive SQL detected"
```

### Rate limiting syntax

| Format | Meaning |
|---|---|
| `"10/minute"` | 10 calls per minute per (agent, tool) |
| `"100/hour"` | 100 calls per hour |
| `"1000/day"` | 1000 calls per day |

---

## Approval Workflows

When a tool call matches a `require_approval` or `require_consent` rule, the gateway requires human review before executing the tool.

### How it works

1. Agent calls tool → rule matches `require_approval`
2. Gateway creates a pending approval and returns immediately (non-blocking)
3. Reviewer is notified via dashboard + webhook
4. Reviewer approves or denies
5. Agent retries with the same arguments → tool executes (or error on deny)

### Governance by archetype

| Archetype | Rule action | Who decides | Mechanism |
|---|---|---|---|
| `autonomous` | `require_approval` | Admin | Dashboard review or admin webhook |
| `user_embedded` | `require_consent` | End user | MCP consent form (elicitation) |
| `delegated` | `require_approval` | Delegated user | Webhook with `act.sub` identity |

### Poll vs callback mode

Configure per rule via `retry_method`:

| Method | Agent experience | Config |
|---|---|---|
| `poll` (default) | Gets error with retry hint (~30s). Retries same call. If approved: tool executes. | `retry_method: poll` |
| `callback` | Gets error saying "your system will be notified." Gateway fires webhook on resolution. | `retry_method: callback` + `status_webhook: https://...` |

```yaml
# Poll mode (default)
- tool: "deploy"
  action: require_approval
  retry_method: poll
  retry_hint_ms: 30000                # suggest retry after 30s

# Callback mode
- tool: "deploy"
  action: require_approval
  retry_method: callback
  status_webhook: "https://my-app.com/approval-status"
```

### Consent flow (user-embedded agents)

When a `user_embedded` agent calls a tool with a `require_consent` rule:

1. Gateway returns MCP `input_required` with consent form (title + description)
2. Client shows form to user → user accepts or declines
3. Agent retries with `requestState` + user response → tool executes

Unlike approval, consent is **in-band** — no external webhook or dashboard review needed. The user decides directly.

```yaml
consent_form:
  title: "Confirm Deletion"
  description: "This will permanently delete data."
```

### Approving and denying

**Via Dashboard:** Click the Approvals badge in the header bar → Approve / Deny buttons.

**Via API:**

```bash
# Approve
curl -X POST http://localhost:8090/admin/v1/approvals/{id}/approve

# Deny
curl -X POST http://localhost:8090/admin/v1/approvals/{id}/deny \
  -H "Content-Type: application/json" \
  -d '{"reason": "Not authorized for this operation"}'
```

### Signed approval URLs

Webhook notifications include HMAC-SHA256 signed approve/deny URLs that reviewers can click directly:

```
GET /approvals/{id}/approve?reviewer=alice&exp=1700000000&sig=abc123...
```

- Each URL is scoped to one approval, one action (approve/deny), one reviewer
- Time-limited — expires with the approval TTL
- Single-use — cannot be replayed

### Slack integration

When the approval webhook URL points to `hooks.slack.com`, the gateway sends Block Kit messages with interactive Approve/Deny buttons.

- Slack clicks are received at `POST /slack/callback`
- Signature verified via HMAC-SHA256 (`slack_signing_secret` config)
- Configure: `proxy.approval.slack_signing_secret` in config

### Webhooks

| Webhook | Direction | Purpose |
|---|---|---|
| `notify` (rule field) | Gateway → Admin | Approval request with signed approve/deny URLs |
| `delegation_notify` (rule field) | Gateway → Delegated user | Same, with user-specific signed URLs |
| `status_webhook` (rule field) | Gateway → Agent system | Resolution notification (callback mode) |
| `/slack/callback` | Slack → Gateway | Interactive button clicks |

### Key properties

- **Non-blocking:** Returns immediately — no held connections. Aligned with MCP 2026-07-28.
- **5-minute TTL:** If no reviewer acts within 5 minutes, the approval expires. Configurable via `proxy.approval.ttl`.
- **Args-matched:** Approval is tied to the exact (agent + tool + arguments) SHA-256 hash. Same args = same approval.
- **Idempotent:** Multiple retries with the same args return the same result — no duplicate approvals created.

---

## Framework Connection

Connect any MCP-compatible agent to the gateway. The endpoint is `http://localhost:8090/mcp`.

### Claude Code / Claude Desktop

Add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "recall-mcp-gateway": {
      "url": "http://localhost:8090/mcp"
    }
  }
}
```

### Cursor

Add to `.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "recall-mcp-gateway": {
      "url": "http://localhost:8090/mcp"
    }
  }
}
```

### LangChain (Python)

```python
from langchain_mcp_adapters import MCPToolkit

toolkit = MCPToolkit(
    server_url="http://localhost:8090/mcp",
    headers={"Authorization": "Bearer rp_your_token"}
)
tools = toolkit.get_tools()
```

### CrewAI

```python
from crewai import Agent
from crewai_tools import MCPTool

mcp_tool = MCPTool(
    server_url="http://localhost:8090/mcp",
    headers={"Authorization": "Bearer rp_your_token"}
)

agent = Agent(role="Researcher", tools=[mcp_tool])
```

### n8n

```
MCP Server Configuration:
  Server URL: http://recall-mcp-gateway:8090/mcp
  Transport: Streamable HTTP
  Authentication: Bearer Token
  Token: rp_your_token
```

### ServiceNow

```
MCP Server Connection:
  URL: http://recall-mcp-gateway:8090/mcp
  Authentication Type: Bearer Token
  Token: rp_your_token
```

### Custom (any language)

**Modern clients (MCP 2026-07-28, stateless):**

```http
POST /mcp HTTP/1.1
Content-Type: application/json
Authorization: Bearer rp_your_token
Mcp-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: read_file

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "read_file",
    "arguments": {"path": "/data/report.txt"},
    "_meta": {
      "clientInfo": {"name": "my-agent", "version": "1.0"}
    }
  }
}
```

No session, no `initialize` handshake. Each request is self-contained.

**Legacy clients (pre-2026, session-based):**

Send an `initialize` request first. The response includes an `Mcp-Session-Id` header — include it in all subsequent requests. The gateway supports both modes transparently.

---

## Dashboard

The embedded dashboard at `http://localhost:8090/dashboard` has five tabs:

| Tab | Purpose |
|---|---|
| **Discover** | Real-time tool call feed, audit history, suspicious sequence detection, detection signals |
| **Control** | Manage servers, agents, and rules. Inline editing, approval queue |
| **Understand** | Compliance score (0–1000), governance recommendations, gap analysis |
| **Settings** | Configuration, agent connection snippets, webhook URLs, platform connection |
| **Docs** | Built-in API reference, security guide, framework guide |

---

## LLM Recommendations

Optionally enable AI-powered governance recommendations in the Understand tab. The LLM analyzes your tool usage patterns and suggests rules.

```yaml
llm:
  enabled: true
  provider: "ollama"                  # ollama (local), openai, anthropic
  model: "llama3"
  api_key_env: "OPENAI_API_KEY"       # env var name containing API key (openai/anthropic)
  url: "http://localhost:11434"       # Ollama endpoint (ollama provider only)
```

**Providers:**

| Provider | Model examples | Requires |
|---|---|---|
| `ollama` | llama3, mistral, codellama | Local Ollama instance |
| `openai` | gpt-4, gpt-4o | `OPENAI_API_KEY` env var |
| `anthropic` | claude-sonnet-4-20250514 | `ANTHROPIC_API_KEY` env var |

---

## Alerts

Webhook-based anomaly alerting. Sends notifications when tool call patterns deviate from baselines.

```yaml
alerts:
  - tool: "*"                         # tool name or glob pattern
    auto_detect: true                 # use EWMA behavioral baselines for threshold
    threshold: 50                     # manual threshold (call count in window)
    window: "5m"                      # time window
    notify: "https://hooks.slack.com/services/..."  # webhook URL
```

When `auto_detect` is enabled, the gateway learns normal call rates per (agent, tool) pair and alerts on deviations. When disabled, the fixed `threshold` is used.

---

## Admin API Quick Reference

### Health & status

| Endpoint | Method | Purpose |
|---|---|---|
| `/healthz` | GET | Liveness probe |
| `/readyz` | GET | Readiness probe |
| `/admin/v1/status` | GET | Server health, alerts, baselines |

### Servers

| Endpoint | Method | Purpose |
|---|---|---|
| `/admin/v1/servers` | GET | List all servers |
| `/admin/v1/servers/add` | POST | Add MCP server |
| `/admin/v1/servers/update/{name}` | PUT | Update server config |
| `/admin/v1/servers/remove/{name}` | DELETE | Remove server |

### Agents

| Endpoint | Method | Purpose |
|---|---|---|
| `/admin/v1/agents` | GET | List all agents |
| `/admin/v1/agents` | POST | Create agent |
| `/admin/v1/agents/{id}` | PUT | Update agent |
| `/admin/v1/agents/{id}` | DELETE | Delete agent |
| `/admin/v1/agents/{id}/rotate-token` | POST | Rotate bearer token |

### Rules

| Endpoint | Method | Purpose |
|---|---|---|
| `/admin/v1/rules` | GET | List all rules |
| `/admin/v1/rules` | POST | Create rule |
| `/admin/v1/rules/{index}` | DELETE | Delete rule by position |

### Approvals

| Endpoint | Method | Purpose |
|---|---|---|
| `/admin/v1/approvals` | GET | List pending approvals |
| `/admin/v1/approvals/{id}/approve` | GET/POST | Approve (signed URL or API) |
| `/admin/v1/approvals/{id}/deny` | GET/POST | Deny (signed URL or API) |

### Audit & compliance

| Endpoint | Method | Purpose |
|---|---|---|
| `/admin/v1/audit/recent` | GET | Recent audit entries |
| `/admin/v1/audit/stats` | GET | Audit statistics |
| `/admin/v1/audit/verify` | GET | Verify hash chain integrity |
| `/admin/v1/insights` | GET | Compliance score + details |
| `/admin/v1/recommendations` | GET | Governance recommendations |

### Configuration

| Endpoint | Method | Purpose |
|---|---|---|
| `/admin/v1/config/runtime` | PUT | Hot-reload config (approval TTL, webhooks) |
| `/admin/v1/config/webhook` | GET/PUT | Webhook URL management |
| `/metrics` | GET | Prometheus metrics |
