# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# recall-mcp-gateway

MCP governance gateway that sits between AI agents and upstream MCP tool servers, adding visibility, auto-detection, and control.

## Architecture Overview

```
cmd/gateway/main.go       Entry point — flags, config, HTTP server, wiring (all DI here)

internal/
  proxy/                     Agent-facing MCP server (streamable HTTP). Two-layer eval:
                             local rules (fast) → engine /decide (connected mode, full policy)
  upstream/                  Upstream MCP server pool: mcp-go client wrapper, circuit breaker,
                             auto-reconnect, idle cleanup, merged tool registry
  config/                    YAML config loading, validation, persistence
  audit/                     AuditStore interface + SQLite (default) and JSONL/memory impls.
                             Compliance score persistence also lives here.
  detect/                    Zero-config auto-detection: tool risk profiling, content regex
                             (PII, creds, SQL), suspicious sequences, workflow discovery,
                             argument diffing, PII data flow tracking, error correlation
  rules/                     First-match-wins rule engine with glob matching + sliding-window
                             rate limiter per (agent, tool)
  baseline/                  EWMA behavioral baselines per (agent, tool) + server latency.
                             JSON file persistence survives restarts.
  alerts/                    6 anomaly types with cooldown + webhook delivery
  approval/                  In-memory pending approvals with TTL + reviewer webhooks
  recommend/                 4 deterministic recommendation generators + optional LLM,
                             apply/dismiss actions
  review/                    Tool review/acknowledgment tracking
  engine/                    Recall engine /decide bridge (connected mode only)
  platform/                  Console-api client for agent health, drift, orgs (connected mode)
  dashboard/                 go:embed React v2 dashboard + SSE feed
  admin/                     Admin REST API handlers (health, status, reload, rules, approvals, etc.)
  auth/                      5 upstream auth providers: bearer, basic, headers, oauth_client_credentials, mtls
  tracing/                   OpenTelemetry trace setup (OTLP exporter)
  sanitize/                  Credential redaction for audit/logs
  jsonrpc/                   JSON-RPC 2.0 message types
  metrics/                   Prometheus metrics collectors + /metrics endpoint
  redis/                     Redis client for multi-pod rate limiting + SSE relay
  store/                     DB abstraction: SQLite + PostgreSQL backends, dialect helpers,
                             LISTEN/NOTIFY sync, schema migrations

configs/                     Shipping configs (standalone, postgres, multinode) + dev configs in configs/dev/
test/integration/            Full lifecycle integration tests
```

## Build & Test

```bash
make build                  # produces ./recall-mcp-gateway
make test                   # unit tests: go test -race -count=1 ./internal/...
make test-integration       # integration tests: go test -race -count=1 ./test/integration/...
make test-all               # both
make lint                   # golangci-lint run ./...
make clean                  # remove binary
make docker-build           # build Docker image
make docker-up              # docker compose up (standalone profile)
make docker-up-postgres     # docker compose up (postgres profile)
make docker-up-multinode    # docker compose up (multinode profile)
make release                # cross-compile all platforms + package + checksums
make run                    # build + run with configs/standalone.yaml
make run-production         # build + run with configs/postgres.yaml
make vuln-check             # govulncheck ./...
make helm-lint              # lint Helm chart
make helm-template          # render Helm templates
make docker-down            # stop all docker compose profiles

# React dashboard v2 (web/)
make web-install            # cd web && npm install
make web-dev                # cd web && npm run dev (Vite on :5173, proxies to :8090)
make web-build              # cd web && npm run build (outputs to internal/dashboard/static/)
make web-typecheck          # cd web && npx tsc --noEmit
make build-full             # web-build + build (React + Go in one step)

# Run a single test by name
go test -race -run TestName ./internal/package/...

# Run tests in a specific package
go test -race -count=1 ./internal/rules/...

# Build check (no binary output)
go build ./...
```

## Running

```bash
./recall-mcp-gateway --config configs/standalone.yaml
# Or: make run (builds + runs with configs/standalone.yaml)
# Dashboard at http://localhost:8090/dashboard
# MCP endpoint at http://localhost:8090/mcp
```

## Coding Standards

- Go 1.26+ (see go.mod)
- Standard library preferred over external dependencies
- Error wrapping with `fmt.Errorf("context: %w", err)`
- Package-level types exported, internal helpers unexported
- No `log.Fatal` outside of `main()`
- All HTTP handlers return JSON with `Content-Type: application/json`
- Health endpoints return only `{"status":"ok"}` — no version, no internals
- Tests in every package, `go test -race` clean
- HTTP: stdlib `net/http` (NOT Fiber). Admin/dashboard routes via `http.ServeMux`
- Key dependency: `github.com/mark3labs/mcp-go v1.3.0` for MCP protocol handling
- Storage: `modernc.org/sqlite` (pure Go, no CGO) for audit + compliance persistence
- Observability: OpenTelemetry (OTLP trace export), Prometheus metrics at `/metrics`

## Open Source & Licensing

The gateway is open source under **AGPL-3.0**. All standalone features are unconditionally enabled.

| Mode | Config | Features |
|------|--------|----------|
| Standalone | no `engine_url` | Full dashboard, rules, baselines, detection, approval workflows, all auth providers |
| Connected (Recall Platform) | `engine_url` configured | + engine /decide integration, platform policy sync, centralized compliance |

Connected mode code (`internal/engine/`, `internal/platform/`) is in the repo but inert unless `engine_url` is configured with a Recall Platform subscription.

## Key Design Decisions

1. **Detect first, block later** — default is observe mode, signals shown, nothing blocked
2. **Two-layer evaluation** — proxy rules (fast, local) + engine /decide (connected, full policy)
3. **Proxy rules preserved on upgrade** — standalone to connected adds, never replaces
4. **DB-only architecture** — gateway.db is the single source of truth for servers, agents, rules. config.yaml is infrastructure-only. First-run seeds DB from YAML, then YAML is ignored.
5. **SSE for dashboard** — no WebSocket dependency, EventSource reconnects automatically
6. **mcp-go v1.3.0 for MCP 2026-07-28** — Stateless protocol, dual-era support (modern + legacy clients), server/discover, routable headers, subscriptions/listen
7. **Auto-reconnect on session errors** — detects dead sessions (404, connection reset, etc.), singleflight reconnect per server, retries the failed call once on fresh session. Max 3 concurrent reconnects globally, jittered to prevent thundering herd
8. **Circuit breaker + health check** — per-server circuit breaker (CLOSED→OPEN after 3 failures, HALF_OPEN probe after 30s). Background health check pings every 30s in parallel. Dashboard shows circuit state
9. **Idle cleanup + graceful shutdown** — sessions idle >5min closed automatically, lazy reconnect on next call. Graceful shutdown waits up to 10s for in-flight calls to drain
10. **Observability** — error classification in audit (session_error, auth_error, timeout, network, circuit_open, application_error), per-server reconnect metrics, persistent failure alerts (>5min), circuit state logging

## Known mcp-go v1.3.0 Workarounds

**MRTP arguments lost on consent retry:** mcp-go v1.3.0 has a bug in `CallToolParams.UnmarshalJSON` where `arguments` are parsed as a JSON array of key names (`["table","id"]`) instead of the actual map (`{"table":"users","id":"..."}`) when `requestState` and `inputResponses` are present in the same request. This causes `req.GetArguments()` to return nil on MRTP retry calls.

**Fix — two-layer workaround:**

1. **HTTP handler wrapper** (`cmd/gateway/main.go`): Intercepts the raw HTTP POST body BEFORE mcp-go reads it. Parses `params.arguments` from the JSON-RPC body and caches them in a `sync.Map` keyed by `"rawargs:" + toolName`. Restores body via `io.NopCloser(bytes.NewReader)` for mcp-go.

2. **Tool handler fallback** (`internal/proxy/server.go`): When `req.GetArguments()` returns nil, retrieves args from the `rawArgsCache` by tool name. Also skips argsHash re-verification when args are still nil (trusts AEAD-encrypted `requestState` which is tamper-proof).

**Why the HTTP wrapper is needed:** mcp-go creates `HTTPRequest` (copies headers) at line 413 of `streamable_http.go` BEFORE calling `httpContextFunc` at line 722. Context values set in `httpContextFunc` don't propagate to the tool handler because mcp-go creates new context chains for session handling. The HTTP wrapper runs at the `net/http` level, before mcp-go's `ServeHTTP`, so the body is parsed while still intact.

**MCP spec compliance:** The MCP 2026-07-28 spec requires "the client retries the original call — same tool name, same arguments, every leg." The HTTP wrapper preserves this contract by extracting the client's args before mcp-go corrupts them.

**Impact:** Consent flow works fully end-to-end. `consentPhase` audit field shows `"requested"` on initial call and `"granted"` on retry. Tool executes on upstream with correct arguments. If mcp-go fixes this in a future version, the workaround is harmless — when `GetArguments()` returns non-nil, the cache is not used.

**Tracking:** This should be reported upstream to github.com/mark3labs/mcp-go as a bug in `CallToolParams.UnmarshalJSON` when `MultiRoundTripParams` fields are present.

## MCP 2026-07-28 Spec Compliance

The gateway implements the MCP 2026-07-28 stateless protocol specification:

- **Dual-era support:** Accepts both modern (2026-07-28, `_meta` per-request) and legacy (pre-2026, `initialize` handshake) clients. Detection is per-request, not per-agent — same agent can upgrade without configuration change.
- **Stateless core:** No session state for modern clients. Agent identity resolved per-request: Bearer token → `_meta.clientInfo.name` → session (legacy) → anonymous.
- **server/discover:** Handled automatically by mcp-go v1.3.0. Returns gateway capabilities, supported versions, and instructions.
- **Routable headers:** `Mcp-Method`, `Mcp-Name`, `MCP-Protocol-Version` validated on incoming requests and emitted on upstream calls (mcp-go handles both).
- **Streamable HTTP:** Single POST endpoint, no GET required for modern. `subscriptions/listen` for tool list change notifications.
- **TTL caching:** Respects upstream `ttlMs`/`cacheScope`. Lazy re-fetch on stale. Emits configurable `client_ttl_ms` to clients.
- **Roots/Sampling:** Not used. Gracefully ignored during deprecation window.
- **OTel trace propagation:** Extracts `traceparent`/`tracestate` from `_meta`, creates child spans, captures trace ID in audit.
- **Auth hardening:** RFC 9207 `iss` validation, credential issuer binding, `application_type` in DCR, PKCE S256 metadata gate.
- **x-mcp-header:** `mcp_header_mode` config (passthrough/generate/managed). Passthrough mode default — mcp-go validates incoming headers.

**Upstream era detection:** Per-server, detected at connection time via `server/discover`. Modern upstreams get 2026-07-28 headers + subscriptions. Legacy upstreams get `initialize` handshake + TTL-only caching.

## Auth Providers (internal/auth/)

6 auth types for upstream MCP server connections:
- **none** — no auth (default)
- **bearer** — `Authorization: Bearer <token>`, env var expansion `${VAR}`
- **basic** — `Authorization: Basic <base64>`, env var expansion
- **headers** — arbitrary static headers, env var expansion
- **oauth_client_credentials** — token fetch from token_url, auto-cache, refresh 30s before expiry, InvalidateToken on 401
- **mtls** — client cert/key + optional CA cert, custom http.Transport via WithHTTPBasicClient, Reload() for cert rotation

Interfaces: `Provider` (Headers/Name), `TransportProvider` (HTTPClient), `RefreshableProvider` (InvalidateToken), `ReloadableProvider` (Reload)

## Auth & Security Features

- External AS JWT validation (RS256/ES256, JWKS cache with stale fallback, full claim validation: aud/iss/exp/nbf/sub/act/may_act)
- Protected Resource Metadata endpoint + 401 challenge (MCP 2026-07-28 spec)
- `auth_server` config entity (issuer, audience, jwks_url, approval_secret, max_delegation_depth)
- `require_consent` rule action — MCP elicitation (InputRequestBuilder, AEAD requestState)
- `require_approval` rule action — admin HITL (webhook, poll/callback, Slack-ready signed URLs)
- Consent fallback: autonomous agents get require_approval instead of elicitation
- Signed approval URLs — HMAC-SHA256 scoped to one approval/action/reviewer, time-limited, single-use
- MCP Tasks extension — async approval for task-capable clients, immediate `_meta` for others
- Slack callback handler — `POST /slack/callback` with HMAC-SHA256 signature verification
- Delegation: ActingFor/DelegationDepth in audit + approvals, may_act validation, configurable depth limits
- RFC 8693 token exchange — ephemeral RSA-2048 signing, `POST /token`, delegated JWT with `act` claim
- Scope step-up — `required_scopes` on rules, `_meta.recall:requiredScopes` hint for re-authorization
- JWT jti replay prevention — `JTICache` with auto-cleanup
- Tamper-proof audit logs — SHA-256 hash chain, `GET /admin/v1/audit/verify`, verified on startup
- Token revocation — `POST /admin/v1/revoke`, SHA-256 hash tracking
- Signed URL replay protection — single-use with expiry-based cleanup
- OAuth token hashing — SHA-256 in memory/DB, plaintext only in HTTP response
- Trusted proxy config — CIDR list for X-Forwarded-For trust
- Admin endpoint auth — `admin_key` Bearer token OR dashboard session cookie
- Agent ID validation — regex rejects path traversal, injection, control characters
- Origin validation middleware — AllowedOrigins config, CORS preflight
- Error message sanitization — `governanceError()` with `_meta.recall:errorCode`, no internal details

## Agent Archetype System

Archetype is configured per agent (`AgentConfig.Archetype`), NOT inferred from JWT claims:

```yaml
agents:
  - id: pipeline-bot
    archetype: autonomous        # admin approves
  - id: claude-desktop
    archetype: user_embedded     # consent elicitation
  - id: alice-service
    archetype: delegated         # user approves
```

**Values:** `autonomous` (default for auto-registered), `user_embedded`, `delegated`

**Simplified rule model:** Rules use specific tool + server + agent. No regex patterns, no classification filters.

| Rule type | Tool | Server | Agent | Actions available |
|-----------|------|--------|-------|-------------------|
| **Specific rule** | exact name (e.g., `delete_record`) | required (e.g., `database`) | optional | All actions incl. governance |
| **Default rule** | `*` | all | all | Allow or Block only |

**Approval flow by archetype:**

| Archetype | MCP Response | Who Decides | Webhooks |
|-----------|-------------|-------------|----------|
| User-embedded | input_required (elicitation) | User (via consent form) | None |
| Autonomous (M2M) | task (async) or _meta (immediate) | Admin (via signed URL) | notify → admin |
| Delegated | task (async) or _meta (immediate) | Delegated user (via signed URL) | delegationNotify → user |

## Server Management (internal/admin/, internal/upstream/)

Dynamic server lifecycle management via dashboard and API:
- **Server Edit** — `PUT /admin/v1/servers/update/{name}` with merge semantics (only non-zero fields overwrite). Secrets redacted in responses.
- **Connection History** — `ConnectionEvent` ring buffer (50 events per server). Events at every state transition.
- **Persistent Metrics** — Connection events logged as audit entries, restored on startup.

## Resilience (internal/upstream/)

- **Auto-reconnect** — detects 12 session error patterns, singleflight per server, retries call once on fresh session
- **Circuit breaker** — per-server, CLOSED→OPEN after 3 failures, HALF_OPEN probe after 30s
- **Health check** — background ping every 30s in parallel, triggers reconnect on circuit open
- **Idle cleanup** — sessions idle >5min closed, lazy reconnect on next call
- **Auth resilience** — OAuth token refresh on 401, mTLS cert reload on TLS error
- **Graceful shutdown** — 10s drain timeout for in-flight calls
- **Concurrency limits** — max 3 concurrent reconnects globally, jittered

## Compliance & Governance Model

- **Score pillars** — 3 GRC-aligned: Tool Governance (400), Signal Safety (300), Ops Readiness (300) = 1000 max
- **Tool Governance** — % of HIGH/CRITICAL tools governed. Per-agent, per-server.
- **Signal Safety** — Inverse of unacknowledged signal rate (PII, credentials, destructive SQL)
- **Ops Readiness** — 4 infra checks at 75 pts each (baselines, audit persistence, agent scoping, observability)
- **Recommendation engine** — 4 types (unprotected_critical_tool, pii_exposure, destructive_sql, rate_limit)
- **Sequence Alerts** — detected suspicious call sequences (data exfiltration, bulk delete, privilege escalation)
- **Score Trend** — 7-day sparkline, daily high scores persisted in `compliance_scores` table

## Storage Architecture

**DB-only architecture:** The database is the **single source of truth** for servers, agents, rules, and all runtime state. `config.yaml` is infrastructure-only. On first startup, if DB tables are empty, they are seeded from YAML. After that, YAML data sections are ignored.

### Three Deployment Modes

| Mode | Config | Store | Redis | Use Case |
|---|---|---|---|---|
| **Standalone (SQLite)** | no `postgres`, no `redis` | local `~/.recall-mcp-gateway/gateway.db` | none | Dev, single node, zero deps |
| **Single-pod (PostgreSQL)** | `postgres.url` set, no `redis` | PostgreSQL | none | Single node, PG durability |
| **Multi-pod** | `postgres.url` + `redis.url` | PostgreSQL (shared) | rate limit + SSE only | Production HA |

### Database Tables (13 tables, identical schema on both backends)

| Table | Contents |
|-------|----------|
| `servers` | Upstream MCP server configs |
| `agents` | All agent configs |
| `rules` | Governance rules (position-ordered) |
| `audit` | Audit log entries + hash chain |
| `compliance_scores` | Daily compliance score history |
| `approvals` | Pending/resolved approval entries |
| `revocations` | Revoked token hashes with TTL |
| `jti_cache` | JWT replay prevention cache |
| `reviews` | Tool review/acknowledgment records |
| `baselines` | EWMA behavioral baselines per (agent, tool) |
| `server_health` | Server latency baselines |
| `oauth_tokens` | Built-in OAuth client registrations |
| `schema_version` | Migration tracking |

### Multi-Pod Sync (PostgreSQL + LISTEN/NOTIFY)

All pods share one PostgreSQL database. Cross-pod notification via PG `LISTEN/NOTIFY` (channel: `config_changed`).

**Redis** is limited to 2 ephemeral use cases (multi-pod only):
- **Rate limiting** — sorted sets for sliding-window counters
- **SSE live feed relay** — pub/sub to broadcast dashboard events across pods

### SQL Dialect

Both SQLite and PostgreSQL supported via `database/sql` with driver detection at runtime (`store.IsPostgres(db)`). DML divergence handled by:
- `store.ExecDialect(db, sqliteSQL, pgSQL, args...)`
- `store.ExecDialectTx(tx, sqliteSQL, pgSQL, isPG, args...)`
- `store.RebindQuery(db, query)` — rewrites `?` → `$1,$2,...` for PG

### Restart Resilience

All runtime state survives restart via the database. On startup: open DB → seed empty tables from YAML (first-run only) → load servers/agents/rules/baselines → restore audit hash chain → load pending approvals.

**Not persisted (by design):** upstream TCP connections (rebuilt), circuit breaker state (resets to closed), dashboard sessions (re-login required), rate limiter counters (transient), SSE subscriptions (auto-reconnect).

## React Dashboard (web/)

22 components, all 5 tabs (Discover, Control, Understand, Settings, Docs), ~118KB gzipped.

**Stack:** Vite 6 + React 19 + TypeScript 5 + Tailwind 4 + Zustand 5 + Radix UI + Lucide icons

**Architecture:**
```
web/
  src/
    components/   Shell.tsx, TopBar.tsx, StatsStrip.tsx (+ tab components)
    hooks/        use-sse.ts (EventSource + auto-reconnect), use-theme.ts (dark/light)
    stores/       gateway-store.ts (SSE init config), feed-store.ts (live entries), stats-store.ts (audit-backed 24h stats)
    lib/          types.ts (Entry, Server, InitMessage), api.ts (typed fetch wrappers)
```

**Routing:** Hash-based (`#discover`, `#control`, etc.) via Radix Tabs — no React Router.

**Data flow:** `useSSE()` → EventSource `/ws/audit` → `init` event hydrates stores → `entry` events prepend to feed (cap 500). Stats refreshed from `/admin/v1/audit/stats?from=24h` every 60s.

**Serving:** Vite builds to `internal/dashboard/static/`. `//go:embed all:static` picks it up automatically.

**Dev workflow:** Terminal 1: `make run`, Terminal 2: `make web-dev` (Vite on :5173 proxies API to :8090).

**Styling rules:**
- CSS custom properties for theme: `--bg`, `--surface`, `--border`, `--text`, `--muted`, `--accent`, `--green`, `--amber`, `--red`, `--purple`
- Font: Outfit body, JetBrains Mono for code/numbers — use literal font stack `MONO` constant
- All clickable elements: minimum 32px touch target, 15px font for navigation items
- Badge text uses `--muted`, only icons get signature colors

## CI/CD

GitHub Actions workflows in `.github/workflows/`:
- **ci.yml** — runs on push and PR: build, test, vet
- **release.yml** — release workflow: cross-compile, package, Docker image, Helm chart

## Module Path

`github.com/controlisai/recall-mcp-gateway`
