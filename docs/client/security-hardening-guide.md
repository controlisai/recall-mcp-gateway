# Recall MCP Gateway — Security Hardening Guide

Production deployment checklist for securing the MCP Gateway.

---

## 1. TLS Configuration

```yaml
proxy:
  tls_cert: "/etc/ssl/gateway/cert.pem"
  tls_key: "/etc/ssl/gateway/key.pem"
```

- **Minimum TLS 1.2** enforced automatically when TLS is configured
- Use certificates from a trusted CA (not self-signed) for production
- Rotate certificates every 90 days (automate with cert-manager)
- For mTLS to upstream servers, configure per-server `auth.type: mtls`

---

## 2. Origin Validation (CORS)

**CRITICAL:** By default, `allowed_origins` is empty which allows ALL origins.

```yaml
proxy:
  allowed_origins:
    - "https://your-app.example.com"
    - "https://admin.example.com"
```

- Always configure `allowed_origins` in production
- The gateway logs a WARNING at startup if `allowed_origins` is empty with `auth_server` configured
- `Vary: Origin` header is set automatically
- CORS preflight (OPTIONS) returns 204 with appropriate headers

---

## 3. Authentication Secrets

### approval_secret

Used for:
- AEAD encryption of MCP elicitation `requestState`
- HMAC-SHA256 signing of approval URLs

```yaml
auth_server:
  approval_secret: "${APPROVAL_SECRET}"  # 32+ characters, env var recommended
```

**WARNING:** If empty, an ephemeral key is generated that does NOT survive restarts. All in-flight consent tokens become invalid after restart.

### admin_key

Controls access to admin API endpoints (status, rules, audit, approvals).

```yaml
proxy:
  approval:
    admin_key: "${ADMIN_KEY}"
```

If empty, admin endpoints are unauthenticated — for local development only.

### slack_signing_secret

Verifies inbound Slack interactive payloads.

```yaml
proxy:
  approval:
    slack_signing_secret: "${SLACK_SIGNING_SECRET}"
```

Get this from your Slack app's "Signing Secret" in the app settings.

---

## 3b. Token Exchange Signing Key

The gateway generates an ephemeral RSA-2048 signing key at startup for RFC 8693 token exchange (delegation). This key is used to sign delegated JWTs with `act` claims.

**Important:** The signing key does NOT persist across restarts. Delegated tokens issued before a restart will fail validation after restart because the public key changes.

**For production:**
- If using token exchange heavily, configure an external Authorization Server for delegation
- The gateway's built-in token exchange is suitable for development and low-volume delegation flows
- Monitor for `invalid signature` errors after restarts — they indicate expired delegated tokens

---

## 4. JWKS Cache Tuning

The gateway caches JWKS keys with a 1-hour TTL. On cache miss (new `kid`), it re-fetches immediately.

**Fallback behavior:** If the JWKS endpoint is unreachable, the gateway uses stale cached keys and logs a warning. This prevents total auth failure during brief Authorization Server outages.

**Recommendations:**
- Ensure the JWKS endpoint is highly available
- Monitor the `jwks_fetch_failed` log pattern
- Key rotation: new keys are picked up on cache miss (by `kid`)

---

## 5. Rate Limiting

### Global HTTP rate limit

```yaml
proxy:
  rate_limit: 100  # requests per second per IP (0 = disabled)
```

### Per-tool rate limits (in rules)

```yaml
rules:
  - tool: "send_*"
    action: allow
    rate_limit: "10/minute"
```

### Signed URL rate limit

The `/approvals/{id}/{action}` signed URL endpoint has a built-in per-IP rate limit of 10 requests per minute. Failed signature attempts are logged at WARN level.

---

## 6. Audit Log Retention

```yaml
proxy:
  audit:
    retention: "30d"       # entries older than this are purged
    max_entries: 100000    # cap on total entries
```

Audit goes to the gateway database automatically:
- **SQLite** (default, standalone mode) — `~/.recall-mcp-gateway/gateway.db`; recommended for single-pod deployments; survives restarts
- **PostgreSQL** (when `postgres.url` is set) — audit is routed to PostgreSQL automatically; use for multi-pod or centralised audit storage
- **JSONL** — set `audit.format: jsonl` for log-forwarding pipelines; does not support search/purge
- Monitor disk/DB growth — audit tables grow with traffic

---

## 7. Signed Approval URLs

Approval URLs use HMAC-SHA256 signatures scoped to:
- One specific approval ID
- One action (approve or deny)
- One reviewer identity
- A time limit (matches approval TTL)

**Security properties:**
- URLs cannot be forged (require `approval_secret`)
- URLs expire (time-limited)
- URLs are single-use (approval transitions from pending to resolved)
- Reviewer identity is embedded and recorded in audit

**Browser clicks:** GET requests return an HTML confirmation page
**Programmatic:** POST requests return JSON

---

## 8. Token Revocation

Revoke compromised tokens via the admin API:

```bash
curl -X POST http://gateway:8090/admin/v1/revoke \
  -H "Authorization: Bearer $ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"token": "eyJ..."}'
```

- Revoked tokens are tracked by SHA-256 hash (token itself is never stored)
- Revocation entries auto-expire when the original token's `exp` time passes
- Requires `admin_key` authentication
- **Revocations are stored in the gateway database** (SQLite or PostgreSQL) — they survive restarts
- In multi-pod PostgreSQL mode, revocations are shared across all pods via the shared database

---

## 9. Error Sanitization

The gateway never exposes internal details in error responses. All governance errors return generic messages with a structured `_meta.recall:errorCode` field.

Detailed error context is logged server-side at DEBUG level for troubleshooting.

---

## 10. Trusted Proxies

When the gateway runs behind a reverse proxy (Nginx, ALB, Cloudfront), configure trusted proxy CIDRs so rate limiting uses the real client IP from `X-Forwarded-For`:

```yaml
proxy:
  trusted_proxies:
    - "10.0.0.0/8"        # Internal network
    - "172.16.0.0/12"     # Docker default
    - "192.168.0.0/16"    # Local network
```

- **Empty list (default):** `X-Forwarded-For` is **never trusted** — safe for direct exposure
- Only requests arriving from a listed CIDR use `X-Forwarded-For` for IP identification
- Direct clients always use `RemoteAddr` regardless of any `X-Forwarded-For` header
- **Risk if misconfigured:** Attackers can spoof IPs to bypass per-IP rate limits

---

## 11. JWT Identity Requirements

When an external Authorization Server is configured, the gateway **rejects** JWTs that lack both `sub` and `client_id` claims. This prevents anonymous unrestricted access.

- `sub` claim → agent identity (preferred)
- `client_id` claim → fallback for M2M `client_credentials` tokens
- Neither present → `401 Unauthorized` (no anonymous fallback)

---

## 12. Admin Endpoint Authentication

All admin API endpoints (`/admin/v1/rules`, `/admin/v1/approvals`, etc.) enforce authentication when `admin_key` is configured:

- **Bearer token:** `Authorization: Bearer <admin_key>` — for scripts, webhooks, CI/CD
- **Dashboard session:** cookie-based auth — for browser/dashboard access
- **No `admin_key`:** endpoints are open (dev/standalone mode only)

Agent ID validation: IDs must match `^[a-zA-Z0-9@][a-zA-Z0-9._@-]{0,254}$`. Path traversal and injection characters are rejected.

---

## 13. Deployment Checklist

- [ ] TLS configured (not plain HTTP in production)
- [ ] `allowed_origins` configured (not empty)
- [ ] `approval_secret` set (not ephemeral)
- [ ] `admin_key` set (not empty)
- [ ] `trusted_proxies` set if behind a reverse proxy
- [ ] Audit retention configured (`audit.retention`, `audit.max_entries`); storage is automatic (SQLite standalone, PostgreSQL when `postgres.url` is set)
- [ ] `rate_limit` configured
- [ ] Dashboard auth enabled (`dashboard_user` + `dashboard_pass_hash`)
- [ ] Metrics enabled for monitoring
- [ ] Log level set to `info` (not `debug` in production)
- [ ] Health/readiness endpoints accessible to load balancer
- [ ] Configure `postgres.url` for production deployments that require shared state across pods or centralised audit storage
- [ ] Redis is only needed for multi-pod rate limiting and SSE relay — not required for single-pod deployments
