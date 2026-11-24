// Auto-generated from vanilla dashboard docs
export const docs: Record<string, string> = {
  quickstart: `<div class="docs-content">
<h2>Quick Start</h2>
<p>Recall MCP Gateway sits between your AI agents and MCP tool servers, adding visibility, rules, and control.</p>

<h3>1. Install</h3>
<pre><code>curl -fsSL https://get.controlis.ai/proxy | sh
# or
brew install controlisai/tap/recall-mcp-gateway</code></pre>

<h3>2. Start the proxy</h3>
<pre><code>recall-mcp-gateway --config config.yaml</code></pre>
<p>The proxy starts on port <code>8090</code>. Add MCP servers via the dashboard or admin API. Servers in config.yaml are seed data — they populate the database on first startup only.</p>

<h3>3. Open the dashboard</h3>
<p>Go to <a href="/dashboard/" target="_blank">http://localhost:8090/dashboard/</a> — you'll see every tool call in real-time.</p>
<p><b>Default login:</b> <code>admin</code> / <code>admin</code>. Change the password hash in your config file (see Configuration below).</p>

<h3>4. Add your first rule</h3>
<p>Go to the <b>Control</b> tab → <b>Rules</b> → <b>+ Add Rule</b>. Select a tool from the dropdown and choose an action:</p>
<pre><code>Tool: write_file (auto-fills server)
Agent: All agents
Action: Block
Reason: File writes need review</code></pre>
<p>For governance rules (approval, consent), select a specific agent — the action dropdown adapts to the agent's archetype.</p>

<h3>5. Connect agents (Teams)</h3>
<p>For company deployments, create agents with tokens:</p>
<pre><code>curl -X POST http://localhost:8090/admin/v1/agents \\\\
  -H "Content-Type: application/json" \\\\
  -d '{"id":"my-agent","generateToken":true}'</code></pre>
<p>Give the returned <code>rp_xxx</code> token to your agent. It sends it as <code>Authorization: Bearer rp_xxx</code> on every MCP request.</p>
<p><b>The returned token is shown once — copy it immediately.</b> The gateway stores only the SHA-256 hash; it cannot be retrieved later.</p>

<h3>Configuration</h3>
<p>Key config fields in your <code>config.yaml</code>:</p>
<pre><code>proxy:
  dashboard_user: admin
  dashboard_pass_hash: "$2a$10$..."  # bcrypt hash of your password
  max_body_size: "10MB"
  rate_limit: 100                    # req/s per IP, 0=disabled
  log_level: "info"                  # debug/info/warn/error
  metrics_enabled: true              # /metrics endpoint
  skip_local_rules: true             # skip proxy rules when engine connected
  product: "my-gateway"              # informational, shows in logs</code></pre>
<p>To generate a bcrypt hash for <code>dashboard_pass_hash</code>:</p>
<pre><code>htpasswd -bnBC 10 "" "your-password" | tr -d ':'</code></pre>
<h3>Deployment Modes</h3>
<p>Three modes, selected by config:</p>
<ul>
<li><b>Standalone (SQLite)</b> — zero dependencies, all state in local <code>gateway.db</code></li>
<li><b>PostgreSQL</b> — add <code>postgres.url</code> to config for durable production storage</li>
<li><b>Multi-pod HA</b> — add <code>postgres.url</code> + <code>redis.url</code> for multi-pod with cross-pod sync</li>
</ul>
</div>`,
  architecture: `<div class="docs-content">
<h2>Architecture</h2>
<p>Recall MCP Gateway is a transparent control plane that sits between AI agents and upstream MCP tool servers. Every tool call flows through the gateway for visibility, policy evaluation, and approval control.</p>

<div style="overflow-x:auto;padding:12px 0">
<svg viewBox="0 0 780 360" fill="none" style="width:100%;max-width:780px;height:auto;font-family:Outfit,system-ui,sans-serif">
  <!-- Background regions -->
  <rect x="4" y="4" width="772" height="352" rx="12" fill="none" stroke="currentColor" stroke-width="1" opacity="0.08"/>

  <!-- Left: AI Agents -->
  <rect x="24" y="60" width="140" height="240" rx="10" fill="rgba(59,130,246,0.06)" stroke="rgba(59,130,246,0.2)" stroke-width="1"/>
  <text x="94" y="44" text-anchor="middle" fill="currentColor" font-size="12" font-weight="700" letter-spacing="0.05em" opacity="0.5">AI AGENTS</text>
  <rect x="40" y="80" width="108" height="32" rx="6" fill="rgba(59,130,246,0.12)" stroke="rgba(59,130,246,0.35)" stroke-width="1"/>
  <text x="94" y="100" text-anchor="middle" fill="currentColor" font-size="11" font-weight="500">Claude</text>
  <rect x="40" y="124" width="108" height="32" rx="6" fill="rgba(59,130,246,0.12)" stroke="rgba(59,130,246,0.35)" stroke-width="1"/>
  <text x="94" y="144" text-anchor="middle" fill="currentColor" font-size="11" font-weight="500">Cursor</text>
  <rect x="40" y="168" width="108" height="32" rx="6" fill="rgba(59,130,246,0.12)" stroke="rgba(59,130,246,0.35)" stroke-width="1"/>
  <text x="94" y="188" text-anchor="middle" fill="currentColor" font-size="11" font-weight="500">LangChain</text>
  <rect x="40" y="212" width="108" height="32" rx="6" fill="rgba(59,130,246,0.12)" stroke="rgba(59,130,246,0.35)" stroke-width="1"/>
  <text x="94" y="232" text-anchor="middle" fill="currentColor" font-size="11" font-weight="500">n8n / CrewAI</text>
  <rect x="40" y="256" width="108" height="32" rx="6" fill="rgba(59,130,246,0.08)" stroke="rgba(59,130,246,0.2)" stroke-width="1" stroke-dasharray="4 3"/>
  <text x="94" y="276" text-anchor="middle" fill="currentColor" font-size="11" opacity="0.4">Any MCP client</text>

  <!-- Arrow: Agents → Gateway -->
  <path d="M164 180 L224 180" stroke="currentColor" stroke-width="1.5" opacity="0.3"/>
  <polygon points="222,175 232,180 222,185" fill="currentColor" opacity="0.3"/>
  <text x="198" y="172" text-anchor="middle" fill="currentColor" font-size="9" opacity="0.4">POST</text>
  <text x="198" y="162" text-anchor="middle" fill="currentColor" font-size="9" opacity="0.4">/mcp</text>

  <!-- Center: Gateway -->
  <rect x="234" y="30" width="300" height="300" rx="10" fill="rgba(168,85,247,0.04)" stroke="rgba(168,85,247,0.25)" stroke-width="1.5"/>
  <text x="384" y="22" text-anchor="middle" fill="currentColor" font-size="12" font-weight="700" letter-spacing="0.05em" opacity="0.5">RECALL MCP GATEWAY</text>

  <!-- Gateway internals -->
  <rect x="254" y="48" width="260" height="30" rx="5" fill="rgba(59,130,246,0.1)" stroke="rgba(59,130,246,0.3)" stroke-width="1"/>
  <text x="384" y="67" text-anchor="middle" fill="currentColor" font-size="11" font-weight="600">Identity &amp; Auth (Bearer / mTLS / OAuth)</text>

  <rect x="254" y="88" width="126" height="30" rx="5" fill="rgba(245,158,11,0.1)" stroke="rgba(245,158,11,0.3)" stroke-width="1"/>
  <text x="317" y="107" text-anchor="middle" fill="currentColor" font-size="11" font-weight="600">Rules Engine</text>
  <rect x="388" y="88" width="126" height="30" rx="5" fill="rgba(239,68,68,0.08)" stroke="rgba(239,68,68,0.3)" stroke-width="1"/>
  <text x="451" y="107" text-anchor="middle" fill="currentColor" font-size="11" font-weight="600">Rate Limiter</text>

  <rect x="254" y="128" width="126" height="30" rx="5" fill="rgba(34,197,94,0.08)" stroke="rgba(34,197,94,0.3)" stroke-width="1"/>
  <text x="317" y="147" text-anchor="middle" fill="currentColor" font-size="11" font-weight="600">Auto-Detect</text>
  <rect x="388" y="128" width="126" height="30" rx="5" fill="rgba(168,85,247,0.08)" stroke="rgba(168,85,247,0.3)" stroke-width="1"/>
  <text x="451" y="147" text-anchor="middle" fill="currentColor" font-size="11" font-weight="600">Approval Flow</text>

  <rect x="254" y="168" width="126" height="30" rx="5" fill="rgba(59,130,246,0.06)" stroke="rgba(59,130,246,0.2)" stroke-width="1"/>
  <text x="317" y="187" text-anchor="middle" fill="currentColor" font-size="11" font-weight="600">Baselines</text>
  <rect x="388" y="168" width="126" height="30" rx="5" fill="rgba(59,130,246,0.06)" stroke="rgba(59,130,246,0.2)" stroke-width="1"/>
  <text x="451" y="187" text-anchor="middle" fill="currentColor" font-size="11" font-weight="600">Signals</text>

  <!-- Audit + Dashboard -->
  <rect x="254" y="212" width="260" height="30" rx="5" fill="rgba(245,158,11,0.06)" stroke="rgba(245,158,11,0.2)" stroke-width="1"/>
  <text x="384" y="231" text-anchor="middle" fill="currentColor" font-size="11" font-weight="600">Audit Log (DB)</text>

  <rect x="254" y="254" width="260" height="30" rx="5" fill="rgba(168,85,247,0.06)" stroke="rgba(168,85,247,0.2)" stroke-width="1"/>
  <text x="384" y="273" text-anchor="middle" fill="currentColor" font-size="11" font-weight="600">Dashboard (SSE real-time)</text>

  <!-- Platform connection (optional) -->
  <rect x="254" y="296" width="260" height="26" rx="5" fill="none" stroke="currentColor" stroke-width="1" stroke-dasharray="4 3" opacity="0.15"/>
  <text x="384" y="313" text-anchor="middle" fill="currentColor" font-size="10" opacity="0.35">Optional: Recall Platform (engine /decide)</text>

  <!-- Arrow: Gateway → Servers -->
  <path d="M534 180 L594 180" stroke="currentColor" stroke-width="1.5" opacity="0.3"/>
  <polygon points="592,175 602,180 592,185" fill="currentColor" opacity="0.3"/>
  <text x="564" y="172" text-anchor="middle" fill="currentColor" font-size="9" opacity="0.4">MCP</text>

  <!-- Right: Upstream Servers -->
  <rect x="604" y="60" width="152" height="240" rx="10" fill="rgba(34,197,94,0.05)" stroke="rgba(34,197,94,0.2)" stroke-width="1"/>
  <text x="680" y="44" text-anchor="middle" fill="currentColor" font-size="12" font-weight="700" letter-spacing="0.05em" opacity="0.5">MCP SERVERS</text>
  <rect x="618" y="80" width="124" height="32" rx="6" fill="rgba(34,197,94,0.1)" stroke="rgba(34,197,94,0.3)" stroke-width="1"/>
  <text x="680" y="100" text-anchor="middle" fill="currentColor" font-size="11" font-weight="500">Filesystem</text>
  <rect x="618" y="124" width="124" height="32" rx="6" fill="rgba(34,197,94,0.1)" stroke="rgba(34,197,94,0.3)" stroke-width="1"/>
  <text x="680" y="144" text-anchor="middle" fill="currentColor" font-size="11" font-weight="500">Database</text>
  <rect x="618" y="168" width="124" height="32" rx="6" fill="rgba(34,197,94,0.1)" stroke="rgba(34,197,94,0.3)" stroke-width="1"/>
  <text x="680" y="188" text-anchor="middle" fill="currentColor" font-size="11" font-weight="500">GitHub</text>
  <rect x="618" y="212" width="124" height="32" rx="6" fill="rgba(34,197,94,0.1)" stroke="rgba(34,197,94,0.3)" stroke-width="1"/>
  <text x="680" y="232" text-anchor="middle" fill="currentColor" font-size="11" font-weight="500">Slack / Email</text>
  <rect x="618" y="256" width="124" height="32" rx="6" fill="rgba(34,197,94,0.06)" stroke="rgba(34,197,94,0.15)" stroke-width="1" stroke-dasharray="4 3"/>
  <text x="680" y="276" text-anchor="middle" fill="currentColor" font-size="11" opacity="0.4">Any MCP server</text>
</svg>
</div>

<h3>MCP Spec Alignment</h3>
<table>
<tr><th>Capability</th><th>Status</th><th>Details</th></tr>
<tr><td><b>Spec version</b></td><td>MCP 2026-07-28</td><td>Latest stateless protocol specification</td></tr>
<tr><td><b>Transport</b></td><td>Streamable HTTP</td><td>Single <code>POST /mcp</code> endpoint. No SSE GET required for modern clients.</td></tr>
<tr><td><b>Protocol model</b></td><td>Stateless</td><td>No server-side session state for modern clients. Each request is self-contained.</td></tr>
<tr><td><b>Dual-era support</b></td><td>Yes</td><td>Accepts both modern (2026-07-28) and legacy (pre-2026, <code>initialize</code> handshake) clients transparently.</td></tr>
<tr><td><b>server/discover</b></td><td>Automatic</td><td>Returns gateway capabilities, supported protocol versions, and connection instructions.</td></tr>
<tr><td><b>Routable headers</b></td><td>Validated</td><td><code>Mcp-Method</code>, <code>Mcp-Name</code>, <code>MCP-Protocol-Version</code> validated on incoming, emitted on upstream.</td></tr>
<tr><td><b>Tool change notifications</b></td><td>Yes</td><td>Subscribes to upstream <code>notifications/tools/list_changed</code>. Relays to connected clients.</td></tr>
<tr><td><b>TTL caching</b></td><td>Yes</td><td>Respects upstream <code>ttlMs</code> / <code>cacheScope</code>. Lazy re-fetch on stale. Configurable <code>client_ttl_ms</code>.</td></tr>
<tr><td><b>Roots/Sampling</b></td><td>Not used</td><td>Gracefully ignored during deprecation window.</td></tr>
</table>

<h3>Modern vs Legacy Clients</h3>
<p>The gateway auto-detects client protocol era per-request (not per-agent). The same agent can upgrade from legacy to modern without reconfiguration.</p>
<table>
<tr><th>Feature</th><th>Modern (2026-07-28)</th><th>Legacy (pre-2026)</th></tr>
<tr><td>Session state</td><td>None — stateless</td><td>Session via <code>initialize</code> handshake</td></tr>
<tr><td>Identity resolution</td><td>Bearer token → <code>_meta.clientInfo.name</code> → anonymous</td><td>Bearer token → session → anonymous</td></tr>
<tr><td>Headers</td><td><code>Mcp-Method</code>, <code>Mcp-Name</code>, <code>MCP-Protocol-Version</code> required</td><td>No routable headers</td></tr>
<tr><td>Tool list changes</td><td><code>subscriptions/listen</code> SSE stream</td><td>Not supported</td></tr>
<tr><td>Response streaming</td><td>SSE or JSON (client choice)</td><td>JSON only</td></tr>
</table>

<h3>MCP Header Mode</h3>
<p>The <code>mcp_header_mode</code> config controls how the gateway handles <code>x-mcp-*</code> headers between agents and upstream servers:</p>
<table>
<tr><th>Mode</th><th>Behavior</th><th>Use Case</th></tr>
<tr><td><code>passthrough</code> (default)</td><td>Validates incoming headers, forwards to upstream unchanged</td><td>Standards-compliant deployments</td></tr>
<tr><td><code>generate</code></td><td>Gateway generates headers from request content (method, tool name)</td><td>Legacy servers that don't emit headers</td></tr>
<tr><td><code>managed</code></td><td>Gateway manages all headers — strips incoming, generates outgoing</td><td>Full control over header semantics</td></tr>
</table>
<p>Can be set globally or per-server via <code>mcp_header_mode</code> in server config.</p>

<h3>Upstream Server Detection</h3>
<p>Each upstream MCP server is probed at connection time via <code>server/discover</code>:</p>
<ul>
<li><b>Modern upstream:</b> Gets 2026-07-28 headers + tool change subscriptions</li>
<li><b>Legacy upstream:</b> Gets <code>initialize</code> handshake + TTL-only caching</li>
</ul>
<p>Server era is shown in the dashboard (Control → Servers) as a <span style="color:var(--green)">modern</span> or <span style="color:var(--amber)">legacy</span> badge.</p>

<h3>Upstream Auth Providers</h3>
<p>Five authentication methods for upstream MCP server connections:</p>
<table>
<tr><th>Type</th><th>Config Key</th><th>Details</th></tr>
<tr><td><code>bearer</code></td><td><code>auth.token</code></td><td><code>Authorization: Bearer &lt;token&gt;</code>. Supports <code>\\\${ENV_VAR}</code> expansion.</td></tr>
<tr><td><code>basic</code></td><td><code>auth.username</code>, <code>auth.password</code></td><td>HTTP Basic Auth. Supports env var expansion.</td></tr>
<tr><td><code>headers</code></td><td><code>auth.headers</code></td><td>Arbitrary static headers (e.g., <code>X-API-Key</code>). Supports env var expansion.</td></tr>
<tr><td><code>oauth_client_credentials</code></td><td><code>auth.token_url</code>, <code>auth.client_id</code>, <code>auth.client_secret</code></td><td>OAuth2 client credentials flow. Auto-caches tokens, refreshes 30s before expiry.</td></tr>
<tr><td><code>mtls</code></td><td><code>auth.client_cert</code>, <code>auth.client_key</code></td><td>Mutual TLS with client certificate. Optional CA cert for self-signed servers.</td></tr>
</table>

<h3>Resilience</h3>
<ul>
<li><b>Circuit breaker:</b> Per-server. CLOSED → OPEN after 3 consecutive failures. HALF_OPEN probe after 30s. Dashboard shows circuit state.</li>
<li><b>Auto-reconnect:</b> Detects 12 session error patterns. Singleflight per server (no thundering herd). Retries the failed call once on fresh session.</li>
<li><b>Idle cleanup:</b> Sessions idle &gt;5 minutes auto-closed. Lazy reconnect on next call.</li>
<li><b>Graceful shutdown:</b> 10-second drain for in-flight calls before termination.</li>
<li><b>Concurrency limits:</b> Max 3 concurrent reconnects globally, jittered to prevent storms.</li>
</ul>

<h3>Auto-Detection Engine</h3>
<p>The gateway automatically classifies every tool and detects risks without configuration:</p>
<ul>
<li><b>Tool classification:</b> READ_ONLY, WRITE, DESTRUCTIVE, CODE_EXECUTION, EXTERNAL_COMM, DATA_TRANSFER — inferred from tool name and schema</li>
<li><b>Risk levels:</b> CRITICAL, HIGH, MEDIUM, LOW — based on classification + input patterns</li>
<li><b>Content detection:</b> PII (SSN, credit card, email), credentials (API keys, passwords, tokens), SQL injection patterns</li>
<li><b>Sequence tracking:</b> Suspicious multi-tool patterns per agent (e.g., read → write → delete)</li>
<li><b>Behavioral baselines:</b> EWMA-tracked per (agent, tool) pair. Alerts on anomalous call rates, argument drift, new fields</li>
</ul>

<h3>Observability</h3>
<p>The gateway supports OTel tracing (W3C trace propagation from MCP <code>_meta</code>) and Prometheus metrics (<code>/metrics</code> endpoint). See <a style="color:var(--accent);cursor:pointer" onclick="showDoc('observability')">Observability docs</a> for configuration and available metrics.</p>

<h3>Decision Model</h3>
<p>Every tool call is evaluated against rules using <b>first-match-wins</b> ordering:</p>
<table>
<tr><th>Decision</th><th>Meaning</th><th>Agent Experience</th></tr>
<tr><td><code>allow</code></td><td>Proceed to upstream server</td><td>Normal response</td></tr>
<tr><td><code>block</code></td><td>Rejected by rule</td><td>MCP error response with reason</td></tr>
<tr><td><code>rate_limited</code></td><td>Sliding window exceeded</td><td>MCP error with retry hint</td></tr>
<tr><td><code>pending_approval</code></td><td>Waiting for human reviewer</td><td>Connection held open until approved/denied/expired (5min TTL)</td></tr>
</table>
<p>When connected to the Recall Platform, a second evaluation layer (engine <code>/decide</code>) runs after local rules. <b>Most-restrictive-wins</b> across both layers.</p>

<h3>Default Action</h3>
<p>When no rule matches a tool call, the <code>default_action</code> config determines the outcome:</p>
<ul>
<li><code>allow</code> (default) — tool call proceeds. "Detect first, block later" philosophy.</li>
<li><code>block</code> — all unmatched tools are blocked. Explicit allowlist mode.</li>
</ul>
<p>Visible in Settings → Gateway Mode.</p>
</div>`,
  agents: `<div class="docs-content">
<h2>Agents & Tokens</h2>
<p>Agents are the governed actors. Each agent has an identity, optional token (SHA-256 hashed), and server access permissions.</p>

<h3>Agent Recognition</h3>
<table>
<tr><th>Method</th><th>How</th><th>Best For</th></tr>
<tr><td>Bearer token</td><td>Pre-configured hash in dashboard or config</td><td>Company deployments</td></tr>
<tr><td>JWT (external AS)</td><td>RS256/ES256 validated via JWKS endpoint</td><td>Enterprise SSO, OAuth 2.1</td></tr>
<tr><td>Anonymous</td><td>No auth configured</td><td>Local development</td></tr>
</table>
<p style="font-size:12px;color:var(--muted)">JWT agent identity: <code>sub</code> claim → agent ID. If no <code>sub</code>, falls back to <code>client_id</code>. If neither is present, the request is <b>rejected</b> (no anonymous access with external AS). See <a style="color:var(--accent);cursor:pointer" onclick="showDoc('security')">Security</a> for JWT config.</p>

<h3>Agent Archetypes</h3>
<p>Every agent has an <b>archetype</b> that determines its governance flow. Set via the <code>archetype</code> field in agent config.</p>
<table>
<tr><th>Archetype</th><th>Description</th><th>Governance</th><th>Example Clients</th></tr>
<tr><td><code>autonomous</code></td><td>Machine-to-machine. No human in the loop during execution.</td><td>Admin reviews via dashboard or webhook</td><td>CI/CD pipelines, data ingestion bots, scheduled tasks</td></tr>
<tr><td><code>user_embedded</code></td><td>Human user is present and can respond to prompts.</td><td>Consent form shown to user via MCP elicitation</td><td>Claude Desktop, Cursor, IDE extensions</td></tr>
<tr><td><code>delegated</code></td><td>Acting on behalf of another user (delegation chain).</td><td>Delegated user reviews via webhook</td><td>ServiceNow workflows, n8n automations acting for a user</td></tr>
</table>
<p><b>Default:</b> Auto-registered agents get <code>autonomous</code> (most restrictive — requires admin approval for governed tools).</p>
<pre><code>agents:
  - id: claude-desktop
    archetype: user_embedded
  - id: pipeline-bot
    archetype: autonomous
  - id: servicenow-agent
    archetype: delegated</code></pre>

<h3>Creating Agents</h3>
<p><b>Via Dashboard:</b> Control tab → Agents → + Create Agent. Check "Generate token" to get a Bearer token.</p>
<p><b>Via API:</b></p>
<pre><code>POST /admin/v1/agents
{
  "id": "servicenow-agent",
  "generateToken": true,
  "allowedServers": ["servicenow-mcp"]
}</code></pre>
<p>Response includes the token (shown once):</p>
<pre><code>{"status":"created","agentId":"servicenow-agent","token":"rp_a1b2c3..."}</code></pre>
<p><b>Token is shown once. Store it securely.</b> The gateway stores only the SHA-256 hash and cannot retrieve the plaintext token later.</p>

<h3>Token Management</h3>
<ul>
<li><b>Rotate:</b> <code>POST /admin/v1/agents/{id}/rotate-token</code> — generates a new token (shown once). Old token immediately invalidated.</li>
<li><b>Dashboard:</b> Expand an agent → copy/rotate icons</li>
</ul>

<h3>Config File (token_hash)</h3>
<p>You can pre-configure agents in <code>config.yaml</code> using a pre-computed hash:</p>
<pre><code>agents:
  - id: my-agent
    token_hash: "sha256hexhash..."  # echo -n "your-token" | sha256sum</code></pre>
<p>This lets you provision agents without exposing plaintext tokens in config files.</p>

<h3>Server Access Control</h3>
<p>Each agent can be restricted to specific MCP servers via <code>allowedServers</code>. If empty, the agent can access all servers.</p>
<pre><code>PUT /admin/v1/agents/{id}/servers
{"allowedServers": ["filesystem", "database"]}</code></pre>
<p>Or use the toggle switches in the agent detail view in the dashboard.</p>

<h3>Agent Sources</h3>
<table>
<tr><th>Source</th><th>Meaning</th><th>Persists</th></tr>
<tr><td><code>config.yaml</code></td><td>Defined in YAML config file</td><td>Seed only (first run)</td></tr>
<tr><td><code>dashboard</code></td><td>Created via dashboard or API</td><td>Yes (gateway database)</td></tr>
</table>
</div>`,
  rules: `<div class="docs-content">
<h2>Rules</h2>
<p>Rules control what agents can do. First-match-wins evaluation — the first matching rule determines the action.</p>

<h3>Rule Model</h3>
<p>Rules use <b>specific tool + server + agent</b>. No regex patterns — tools are selected from discovered tools, server is auto-filled.</p>
<table>
<tr><th>Rule Type</th><th>Tool</th><th>Server</th><th>Agent</th><th>Actions</th></tr>
<tr><td>Specific</td><td>exact name</td><td>required</td><td>optional</td><td>All actions</td></tr>
<tr><td>Default</td><td><code>*</code></td><td>all</td><td>all</td><td>Allow or Block</td></tr>
</table>

<h3>Actions by Agent Type</h3>
<table>
<tr><th>Agent Selection</th><th>Available Actions</th></tr>
<tr><td>All agents</td><td>Allow, Block, Rate Limit, Hide</td></tr>
<tr><td>Autonomous agent</td><td>+ Require Approval (admin reviews in dashboard or via webhook)</td></tr>
<tr><td>User-embedded agent</td><td>+ Require Consent (MCP elicitation form)</td></tr>
<tr><td>Delegated agent</td><td>+ Require Approval (delegated user reviews)</td></tr>
</table>

<h3>Creating Rules</h3>
<p><b>Via Dashboard:</b> Control tab → Rules → + Add Rule. Select tool, agent, and action. The default <code>*</code> rule is managed via the Allow/Block toggle in the rules header.</p>
<p><b>Via API:</b></p>
<pre><code>POST /admin/v1/rules
{
  "tool": "delete_record",
  "server": "database",
  "action": "require_approval",
  "agents": ["pipeline-bot"],
  "reason": "Destructive op needs admin approval"
}</code></pre>

<h3>Agent-Scoped Governance</h3>
<p>Approval and consent rules require a specific agent. The agent's archetype determines the governance flow:</p>
<pre><code>{
  "tool": "delete_record",
  "server": "database",
  "action": "require_consent",
  "agents": ["claude-desktop"],
  "consent_form": {
    "title": "Confirm Deletion",
    "description": "This will permanently delete data."
  }
}</code></pre>

<h3>Persistence</h3>
<p>Rules persist to the gateway database automatically (SQLite or PostgreSQL depending on your deployment mode).</p>
</div>`,
  servers: `<div class="docs-content">
<h2>MCP Servers</h2>
<p>MCP servers are the upstream tool providers. The proxy connects to them and exposes their tools to agents.</p>

<h3>Add via Dashboard</h3>
<p>Control tab → Servers → + Add. Supports both transport types:</p>
<ul>
<li><b>stdio:</b> Local process (command + args)</li>
<li><b>HTTP:</b> Remote server (URL)</li>
</ul>

<h3>Seed via Config (First Run)</h3>
<pre><code>servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]

  - name: database
    transport: http
    url: http://database-mcp:3100/mcp</code></pre>
<p>Config file servers are seed data — they populate the database on first startup only. After that, manage servers via the dashboard or admin API.</p>

<h3>Server Status</h3>
<p>The dashboard shows server health in real-time:</p>
<ul>
<li><b>Connected</b> (green) — server is reachable and tools are registered</li>
<li><b>Reconnecting</b> (amber) — connection lost, retrying</li>
<li><b>Down</b> (red) — server unreachable</li>
</ul>

<h3>Server Authentication</h3>
<p>Configure auth per server to connect to protected MCP servers:</p>
<pre><code>servers:
  - name: github-mcp
    transport: http
    url: https://mcp.github.com/sse
    auth:
      type: bearer
      token: "\\\${GITHUB_TOKEN}"       # env var expansion

  - name: internal-api
    transport: http
    url: https://internal.corp/mcp
    auth:
      type: oauth_client_credentials
      token_url: https://auth.corp/oauth/token
      client_id: "\\\${CLIENT_ID}"
      client_secret: "\\\${CLIENT_SECRET}"
      scopes: "api read"

  - name: secure-server
    transport: http
    url: https://secure.corp/mcp
    auth:
      type: mtls
      client_cert: /etc/certs/client.pem
      client_key: /etc/certs/client-key.pem
      ca_cert: /etc/certs/ca.pem</code></pre>

<h3>Server Type</h3>
<p>Optional <code>server_type</code> field for organizational grouping: <code>code_tools</code>, <code>data_tools</code>, <code>communication</code>, <code>infrastructure</code>, <code>monitoring</code>, <code>custom</code>.</p>

<h3>Server Latency Tracking</h3>
<p>The proxy tracks mean latency per server using EWMA (Exponentially Weighted Moving Average). Visible in the dashboard stats and used for health status classification.</p>

<h3>Circuit Breaker</h3>
<p>Each server has an independent circuit breaker. After 3 consecutive failures, the circuit opens (blocking calls) and probes every 30s. Dashboard shows circuit state per server.</p>
<p style="font-size:12px;color:var(--muted);margin-top:8px">For upstream auth configuration (bearer, OAuth, mTLS, etc.), see <a style="color:var(--accent);cursor:pointer" onclick="showDoc('architecture')">Architecture → Upstream Auth Providers</a>.</p>
</div>`,
  approvals: `<div class="docs-content">
<h2>Approval Flow</h2>
<p>When a tool call matches a <code>require_approval</code> rule, the gateway requires human review before executing the tool. The gateway uses a <b>non-blocking, stateless</b> approval model aligned with MCP 2026-07-28.</p>

<h3>How It Works</h3>
<div style="overflow-x:auto;padding:8px 0">
<svg viewBox="0 0 720 380" fill="none" style="width:100%;max-width:720px;height:auto;font-family:Outfit,system-ui,sans-serif">
  <!-- Step 1: Agent calls -->
  <rect x="20" y="12" width="200" height="38" rx="7" fill="rgba(59,130,246,0.1)" stroke="rgba(59,130,246,0.4)" stroke-width="1.5"/>
  <text x="120" y="36" text-anchor="middle" fill="currentColor" font-size="12" font-weight="600">1. Agent calls tool</text>
  <!-- Arrow -->
  <path d="M220 31 L268 31" stroke="currentColor" stroke-width="1.5" opacity="0.3"/>
  <polygon points="266,26 276,31 266,36" fill="currentColor" opacity="0.3"/>
  <!-- Step 2: Rule match -->
  <rect x="278" y="12" width="200" height="38" rx="7" fill="rgba(245,158,11,0.1)" stroke="rgba(245,158,11,0.4)" stroke-width="1.5"/>
  <text x="378" y="36" text-anchor="middle" fill="currentColor" font-size="12" font-weight="600">2. Rule: require_approval</text>
  <!-- Arrow -->
  <path d="M478 31 L526 31" stroke="currentColor" stroke-width="1.5" opacity="0.3"/>
  <polygon points="524,26 534,31 524,36" fill="currentColor" opacity="0.3"/>
  <!-- Step 3: Return immediately -->
  <rect x="536" y="12" width="170" height="38" rx="7" fill="rgba(168,85,247,0.1)" stroke="rgba(168,85,247,0.4)" stroke-width="1.5"/>
  <text x="621" y="30" text-anchor="middle" fill="currentColor" font-size="12" font-weight="600">3. Return pending</text>
  <text x="621" y="44" text-anchor="middle" fill="currentColor" font-size="10" opacity="0.5">(non-blocking)</text>

  <!-- Divider -->
  <line x1="20" y1="70" x2="700" y2="70" stroke="currentColor" stroke-width="1" opacity="0.08"/>
  <text x="360" y="88" text-anchor="middle" fill="currentColor" font-size="11" opacity="0.35">Meanwhile: reviewer is notified via dashboard + webhook</text>

  <!-- Poll path -->
  <text x="30" y="122" fill="currentColor" font-size="11" font-weight="700" opacity="0.5">POLL MODE (default)</text>
  <rect x="20" y="132" width="200" height="34" rx="6" fill="rgba(59,130,246,0.06)" stroke="rgba(59,130,246,0.2)" stroke-width="1"/>
  <text x="120" y="153" text-anchor="middle" fill="currentColor" font-size="11">Agent gets retry hint (~30s)</text>
  <path d="M220 149 L268 149" stroke="currentColor" stroke-width="1.5" opacity="0.3"/>
  <polygon points="266,144 276,149 266,154" fill="currentColor" opacity="0.3"/>
  <rect x="278" y="132" width="200" height="34" rx="6" fill="rgba(34,197,94,0.08)" stroke="rgba(34,197,94,0.25)" stroke-width="1"/>
  <text x="378" y="153" text-anchor="middle" fill="currentColor" font-size="11">Reviewer approves/denies</text>
  <path d="M478 149 L526 149" stroke="currentColor" stroke-width="1.5" opacity="0.3"/>
  <polygon points="524,144 534,149 524,154" fill="currentColor" opacity="0.3"/>
  <rect x="536" y="132" width="170" height="34" rx="6" fill="rgba(59,130,246,0.06)" stroke="rgba(59,130,246,0.2)" stroke-width="1"/>
  <text x="621" y="153" text-anchor="middle" fill="currentColor" font-size="11">Agent retries same args</text>
  <!-- Arrow down from retry -->
  <path d="M621 166 L621 186" stroke="currentColor" stroke-width="1.5" opacity="0.3"/>
  <polygon points="616,184 621,194 626,184" fill="currentColor" opacity="0.3"/>
  <!-- Result -->
  <rect x="536" y="196" width="170" height="34" rx="6" fill="rgba(34,197,94,0.12)" stroke="rgba(34,197,94,0.4)" stroke-width="1.5"/>
  <text x="621" y="217" text-anchor="middle" fill="currentColor" font-size="12" font-weight="600">&#x2713; Tool executes</text>

  <!-- Callback path -->
  <text x="30" y="258" fill="currentColor" font-size="11" font-weight="700" opacity="0.5">CALLBACK MODE</text>
  <rect x="20" y="268" width="200" height="34" rx="6" fill="rgba(59,130,246,0.06)" stroke="rgba(59,130,246,0.2)" stroke-width="1"/>
  <text x="120" y="289" text-anchor="middle" fill="currentColor" font-size="11">Agent gets "will be notified"</text>
  <path d="M220 285 L268 285" stroke="currentColor" stroke-width="1.5" opacity="0.3"/>
  <polygon points="266,280 276,285 266,290" fill="currentColor" opacity="0.3"/>
  <rect x="278" y="268" width="200" height="34" rx="6" fill="rgba(34,197,94,0.08)" stroke="rgba(34,197,94,0.25)" stroke-width="1"/>
  <text x="378" y="289" text-anchor="middle" fill="currentColor" font-size="11">Reviewer approves/denies</text>
  <path d="M478 285 L526 285" stroke="currentColor" stroke-width="1.5" opacity="0.3"/>
  <polygon points="524,280 534,285 524,290" fill="currentColor" opacity="0.3"/>
  <rect x="536" y="268" width="170" height="34" rx="6" fill="rgba(245,158,11,0.08)" stroke="rgba(245,158,11,0.3)" stroke-width="1"/>
  <text x="621" y="289" text-anchor="middle" fill="currentColor" font-size="11">Webhook fires to agent</text>

  <!-- Deny path note -->
  <rect x="20" y="326" width="686" height="38" rx="6" fill="rgba(239,68,68,0.05)" stroke="rgba(239,68,68,0.15)" stroke-width="1"/>
  <text x="360" y="342" text-anchor="middle" fill="currentColor" font-size="11" opacity="0.5">If denied or expired (5 min TTL): agent gets error on next retry (poll) or deny webhook (callback)</text>
  <text x="360" y="356" text-anchor="middle" fill="currentColor" font-size="10" opacity="0.35">Approval matched by SHA-256 hash of (agent + tool + arguments) — same args = same approval</text>
</svg>
</div>

<h3>Retry Methods</h3>
<p>Configure per rule via the <code>retry_method</code> field:</p>
<table>
<tr><th>Method</th><th>Agent Experience</th><th>Config</th></tr>
<tr><td><code>poll</code> (default)</td><td>Gets error with retry hint (default 30s). Retries same call. If approved: tool executes. If denied/expired: error returned.</td><td><code>retry_method: poll</code><br><code>retry_hint_ms: 30000</code></td></tr>
<tr><td><code>callback</code></td><td>Gets error saying "your system will be notified." Gateway fires <code>status_webhook</code> URL on resolution with approval status.</td><td><code>retry_method: callback</code><br><code>status_webhook: https://...</code></td></tr>
</table>

<h3>Key Properties</h3>
<ul>
<li><b>Non-blocking:</b> The gateway returns immediately — no held connections, no sessions. Fully stateless, aligned with MCP 2026-07-28.</li>
<li><b>5-minute TTL:</b> If no reviewer acts within 5 minutes, the approval expires.</li>
<li><b>Args-matched:</b> Approval is tied to the exact (agent + tool + arguments) SHA-256 hash. A retry with the same args matches the existing approval.</li>
<li><b>Idempotent:</b> Multiple retries with the same args return the same result — no duplicate approvals created.</li>
</ul>
<p style="font-size:12px;color:var(--muted);margin-top:8px"><b>Legacy note:</b> Pre-2026 clients that use <code>initialize</code> sessions still get the legacy blocking behavior (connection held open until resolved) for backward compatibility.</p>

<h3>Governance by Archetype</h3>
<p>The agent's archetype determines which governance flow applies:</p>
<table>
<tr><th>Archetype</th><th>Rule Action</th><th>Who Decides</th><th>Mechanism</th></tr>
<tr><td><code>autonomous</code></td><td><code>require_approval</code></td><td>Admin</td><td>Dashboard review or admin webhook</td></tr>
<tr><td><code>user_embedded</code></td><td><code>require_consent</code></td><td>End user</td><td>MCP consent form (elicitation)</td></tr>
<tr><td><code>delegated</code></td><td><code>require_approval</code></td><td>Delegated user</td><td>Webhook with <code>act.sub</code> identity</td></tr>
</table>
<p style="font-size:12px;color:var(--muted)">See <a style="color:var(--accent);cursor:pointer" onclick="showDoc('agents')">Agents → Archetypes</a> for definitions and examples.</p>

<h3>Consent Flow (User-Embedded Agents)</h3>
<p>When a <code>user_embedded</code> agent calls a tool with a <code>require_consent</code> rule, the gateway returns an MCP <b>elicitation</b> (<code>input_required</code>) with a consent form:</p>
<ol>
<li>Agent calls tool → rule matches <code>require_consent</code></li>
<li>Gateway returns <code>input_required</code> with consent form (title + description)</li>
<li>Client shows form to user → user accepts or declines</li>
<li>Agent retries with <code>requestState</code> + user response → tool executes</li>
</ol>
<p>Configure the consent form in the rule:</p>
<pre><code>consent_form:
  title: "Confirm Deletion"
  description: "This will permanently delete data."</code></pre>
<p style="font-size:12px;color:var(--muted)">Unlike approval, consent is in-band — no external webhook or dashboard review needed. The user decides directly.</p>

<h3>Approving/Denying</h3>
<p><b>Via Dashboard:</b> Click the Approvals badge in the header bar → Approve / Deny buttons in the dropdown panel</p>
<p><b>Via API:</b></p>
<pre><code># Approve
POST /admin/v1/approvals/{id}/approve

# Deny
POST /admin/v1/approvals/{id}/deny
{"reason": "Not authorized for this operation"}</code></pre>

<h3>Signed Approval URLs</h3>
<p>Webhook notifications include HMAC-SHA256 signed approve/deny URLs that reviewers can click directly (browser or Slack):</p>
<pre><code>GET /approvals/{id}/approve?reviewer=alice&amp;exp=1700000000&amp;sig=abc123...</code></pre>
<ul>
<li>Each URL is scoped to one approval, one action (approve/deny), one reviewer identity</li>
<li>Time-limited — expires with the approval TTL</li>
<li>Rate-limited to 10 requests/minute per IP</li>
</ul>

<h3>Slack Integration</h3>
<p>When the admin webhook URL points to <code>hooks.slack.com</code>, the gateway sends Block Kit messages with interactive Approve/Deny buttons.</p>
<ul>
<li>Slack clicks are received at <code>POST /slack/callback</code></li>
<li>Signature verified via HMAC-SHA256 (<code>slack_signing_secret</code> config)</li>
<li>Async <code>response_url</code> delivery for guaranteed Slack response</li>
</ul>

<h3>Webhooks</h3>
<table>
<tr><th>Webhook</th><th>Direction</th><th>Purpose</th></tr>
<tr><td><code>notify</code> (rule field)</td><td>Gateway → Admin</td><td>Approval request with signed approve/deny URLs</td></tr>
<tr><td><code>delegation_notify</code> (rule field)</td><td>Gateway → Delegated user</td><td>Same, with user-specific signed URLs</td></tr>
<tr><td><code>status_webhook</code> (rule field)</td><td>Gateway → Agent system</td><td>Resolution notification (callback mode)</td></tr>
<tr><td><code>/slack/callback</code></td><td>Slack → Gateway</td><td>Interactive button clicks</td></tr>
</table>
</div>`,
  security: `<div class="docs-content">
<h2>Security</h2>
<p>The gateway provides layered security for both client-facing (agent authentication) and server-facing (upstream auth) connections.</p>

<h3>Client Authentication (JWT)</h3>
<p>For enterprise deployments, configure an external Authorization Server (AS) to validate agent JWTs:</p>
<pre><code>auth_server:
  issuer: "https://auth.example.com"
  audience: "https://gateway.example.com/mcp"
  jwks_url: "https://auth.example.com/.well-known/jwks.json"
  approval_secret: "32-char-secret-for-signed-urls!!"
  max_delegation_depth: 3</code></pre>
<p>The gateway validates:</p>
<ul>
<li><b>Signature:</b> RS256 or ES256, verified against JWKS endpoint (auto-cached, fallback on failure)</li>
<li><b>Claims:</b> <code>iss</code> (issuer match), <code>aud</code> (audience match), <code>exp</code>/<code>nbf</code> (time validity)</li>
<li><b>Identity:</b> <code>sub</code> → agent ID, <code>act.sub</code> → acting-for (delegation), <code>client_id</code> → M2M fallback</li>
<li><b>Delegation:</b> <code>act</code> (active delegation), <code>may_act</code> (authorized delegation). Depth enforced by <code>max_delegation_depth</code></li>
</ul>

<h3>Token Security</h3>
<ul>
<li><b>JTI replay prevention:</b> JWT <code>jti</code> claim tracked — duplicate JWTs rejected within their expiry window</li>
<li><b>Token revocation:</b> <code>POST /admin/v1/revoke</code> with SHA-256 hash — revoked tokens rejected immediately</li>
<li><b>Static token expiry:</b> <code>expires_at</code> field on agent config (RFC 3339) — expired tokens rejected at auth time</li>
</ul>

<h3>Origin Validation (CORS)</h3>
<p>Restrict which origins can access the MCP endpoint:</p>
<pre><code>proxy:
  allowed_origins:
    - "https://app.example.com"
    - "http://localhost:3000"</code></pre>
<ul>
<li>Requests with disallowed <code>Origin</code> header get HTTP 403</li>
<li>CORS preflight (<code>OPTIONS</code>) handled automatically</li>
<li>Requests with no <code>Origin</code> header (server-to-server) are allowed</li>
</ul>

<h3>Trusted Proxies</h3>
<p>When the gateway runs behind a reverse proxy (Nginx, ALB, Cloudfront), configure trusted proxy CIDRs to correctly identify client IPs from <code>X-Forwarded-For</code>:</p>
<pre><code>proxy:
  trusted_proxies:
    - "10.0.0.0/8"        # Internal network
    - "172.16.0.0/12"     # Docker
    - "192.168.0.0/16"    # Local</code></pre>
<ul>
<li>Empty list (default) = never trust <code>X-Forwarded-For</code> — safe for direct exposure</li>
<li>Only requests from listed CIDRs use <code>X-Forwarded-For</code> for rate limiting</li>
<li>Direct clients always use <code>RemoteAddr</code> regardless of headers</li>
</ul>

<h3>Admin Endpoint Authentication</h3>
<p>When <code>admin_key</code> is configured, all admin API endpoints (<code>/admin/v1/rules</code>, <code>/admin/v1/approvals</code>, etc.) require authentication:</p>
<ul>
<li><b>Bearer token:</b> <code>Authorization: Bearer &lt;admin_key&gt;</code> for scripts/webhooks</li>
<li><b>Dashboard session:</b> cookie-based auth for browser access</li>
<li>No <code>admin_key</code> configured = open access (dev/standalone mode)</li>
</ul>

<h3>Protected Resource Metadata</h3>
<p>The gateway publishes OAuth 2.0 Protected Resource Metadata at:</p>
<pre><code>GET /.well-known/oauth-protected-resource</code></pre>
<p>Returns the resource identifier and authorization server URL. Unauthenticated requests to <code>/mcp</code> receive a <code>401</code> with <code>WWW-Authenticate: Bearer resource_metadata="..."</code> header, directing clients to discover the AS.</p>

<h3>Delegation & Token Exchange</h3>
<p>For delegation chains (agent acting on behalf of a user):</p>
<ul>
<li><b>JWT <code>act</code> claim:</b> Nested actor chain — each level has a <code>sub</code> identifying the actor</li>
<li><b>JWT <code>may_act</code> claim:</b> Pre-authorized delegation — gateway validates the subject is allowed to delegate to the actor</li>
<li><b>Depth limit:</b> <code>max_delegation_depth</code> config (default: no limit). Chains exceeding the limit are rejected</li>
<li><b>Audit trail:</b> <code>actingFor</code> and <code>delegationDepth</code> recorded in every audit entry</li>
</ul>
<p><b>RFC 8693 Token Exchange:</b> The gateway can issue delegated tokens via <code>POST /token</code> with <code>grant_type=urn:ietf:params:oauth:grant-type:token-exchange</code>. Validates subject + actor JWTs, issues a new JWT with <code>act</code> claim.</p>
<p style="font-size:12px;color:var(--muted)">The gateway's signing key is ephemeral (regenerated on restart). For persistent delegated tokens, use an external AS.</p>

<h3>Audit Integrity</h3>
<p>Audit entries are chained using SHA-256 hashes — each entry includes the hash of the previous entry, forming a tamper-evident chain.</p>
<pre><code>GET /admin/v1/audit/verify</code></pre>
<p>Returns chain integrity status. Any modified or deleted entry breaks the chain from that point forward.</p>
</div>`,
  platform: `<div class="docs-content">
<h2>Platform Connection</h2>
<p>Connect the proxy to the Recall Platform for full governance — policy engine, compliance scoring, multi-org governance, and audit persistence.</p>

<h3>What You Need</h3>
<table>
<tr><th>Service</th><th>URL</th><th>Auth</th><th>Purpose</th></tr>
<tr><td>Console API</td><td>Cloud</td><td>Service token</td><td>Config sync, dashboard data</td></tr>
<tr><td>Console UI</td><td>Cloud</td><td>—</td><td>Link target for "Open Console"</td></tr>
<tr><td>Engine</td><td>Your infra</td><td>API key</td><td>/decide on every tool call</td></tr>
</table>

<h3>Connect</h3>
<p>Settings tab → Platform Connection → fill in the fields → Connect.</p>
<p>Or via API:</p>
<pre><code>POST /admin/v1/platform/connect
{
  "consoleApiUrl": "https://api.controlis.ai",
  "serviceToken": "your-service-token",
  "tenantId": "your-tenant-id",
  "consoleTenantUrl": "https://console.controlis.ai",
  "engineUrl": "http://recall-engine:8084",
  "apiKey": "cai_rcl_eng_dev_xxx",
  "defaultOrgId": "default"
}</code></pre>

<h3>Sync to Platform</h3>
<p>After connecting, sync your proxy data to the platform:</p>
<pre><code>POST /admin/v1/platform/sync
{"productId": "your-product-id"}</code></pre>
<p>This registers your agents, tools (as intents), and rules (as a starter policy) in the platform.</p>

<h3>Two-Layer Evaluation</h3>
<p>After connecting, every tool call is evaluated twice:</p>
<ul>
<li><b>Layer 1:</b> Proxy rules (fast, local) — if block, stop</li>
<li><b>Layer 2:</b> Engine /decide (full governance policy)</li>
</ul>
<p>Most-restrictive-wins across both layers. Your proxy rules are preserved, not replaced.</p>
<p>When connected, local proxy rules are skipped by default (platform is the single governance authority). Set <code>skip_local_rules: false</code> in your config to keep both layers active.</p>

<h3>Logging Context</h3>
<p>Set <code>product: your-product-name</code> in your config to tag proxy calls with a product identifier. This appears in logs and platform audit records for multi-product deployments.</p>

<h3>What Carries Over</h3>
<ul>
<li>Agents → registered in platform agent registry</li>
<li>Tools → registered as intents with risk classification</li>
<li>Rules → converted to starter admissibility policy</li>
<li>Baselines → exported as behavioral context</li>
</ul>
<p>Your agents don't need any changes — same tokens, same proxy URL.</p>
</div>`,
  observability: `<div class="docs-content">
<h2>Observability</h2>
<p>The gateway exposes two observability channels: OpenTelemetry (OTel) tracing for distributed trace propagation, and Prometheus metrics for operational monitoring.</p>

<h3>OTel Tracing</h3>
<p>When enabled, the gateway participates in distributed traces by extracting and propagating W3C trace context (<code>traceparent</code> / <code>tracestate</code>) from MCP request <code>_meta</code> fields.</p>

<h4>What the gateway does:</h4>
<ul>
<li>Extracts <code>traceparent</code> from incoming modern client requests (<code>_meta</code>)</li>
<li>Creates child spans for each tool call with attributes: tool name, agent, server, decision, latency</li>
<li>Propagates trace context to upstream MCP servers</li>
<li>Records trace IDs in audit entries — links dashboard feed to distributed traces</li>
<li>Exports spans to an OTLP-compatible collector (Jaeger, Grafana Tempo, Datadog, etc.)</li>
</ul>

<h4>Configuration:</h4>
<pre><code>proxy:
  tracing:
    enabled: true
    endpoint: "localhost:4317"          # OTLP gRPC collector
    service_name: "recall-mcp-gateway"  # appears in traces</code></pre>

<h4>Helm values:</h4>
<pre><code>tracing:
  enabled: true
  endpoint: "otel-collector:4317"
  serviceName: "recall-mcp-gateway"</code></pre>

<p>Trace IDs appear in the Live tab as purple badges on each call. Click to expand and see the full trace ID in the detail panel.</p>

<h3>Prometheus Metrics</h3>
<p>The gateway exposes a <code>/metrics</code> endpoint in Prometheus exposition format. Enabled by default.</p>

<h4>Configuration:</h4>
<pre><code>proxy:
  metrics_enabled: true   # default: true
  # Set to false to disable /metrics endpoint</code></pre>

<h4>Available metrics:</h4>
<table>
<tr><th>Metric</th><th>Type</th><th>Description</th></tr>
<tr><td><code>recall_proxy_tool_calls_total</code></td><td>counter</td><td>Total tool calls by tool, agent, decision</td></tr>
<tr><td><code>recall_proxy_tool_latency_ms</code></td><td>histogram</td><td>Tool call latency distribution</td></tr>
<tr><td><code>recall_proxy_active_connections</code></td><td>gauge</td><td>Active upstream server connections</td></tr>
<tr><td><code>recall_proxy_rules_evaluated_total</code></td><td>counter</td><td>Rule evaluations by action</td></tr>
<tr><td><code>recall_proxy_approvals_pending</code></td><td>gauge</td><td>Currently pending approvals</td></tr>
</table>

<h4>Grafana / scraping:</h4>
<pre><code># Prometheus scrape config
- job_name: recall-mcp-gateway
  static_configs:
    - targets: ['recall-mcp-gateway:8090']
  metrics_path: /metrics</code></pre>
<p>The Helm chart includes Prometheus annotations by default (<code>prometheus.io/scrape: "true"</code>).</p>

<h3>Dashboard visibility</h3>
<p>Current observability status is shown in <b>Settings → Observability</b>:</p>
<ul>
<li><b>OTel Tracing:</b> enabled/disabled with endpoint</li>
<li><b>Prometheus Metrics:</b> enabled/disabled with <code>/metrics</code> path</li>
</ul>
</div>`,
  api: `<div class="docs-content">
<h2>API Reference</h2>
<p>All admin endpoints are on <code>http://localhost:8090/admin/v1/</code>. No authentication required (localhost only).</p>

<h3>Health</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET</td><td><code>/healthz</code></td><td>Liveness check</td></tr>
<tr><td>GET</td><td><code>/readyz</code></td><td>Readiness check (servers connected)</td></tr>
</table>

<h3>Status & Data</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET</td><td><code>/admin/v1/status</code></td><td>Server status, baseline status, alerts</td></tr>
<tr><td>GET</td><td><code>/admin/v1/baselines</code></td><td>Behavioral baselines per (agent, tool)</td></tr>
<tr><td>GET</td><td><code>/admin/v1/insights</code></td><td>Compliance preview + governance insights</td></tr>
<tr><td>GET</td><td><code>/admin/v1/sequences</code></td><td>Detected tool call sequences</td></tr>
</table>

<h3>Servers</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>POST</td><td><code>/admin/v1/servers/add</code></td><td>Add upstream MCP server (name, transport, url/command, auth)</td></tr>
<tr><td>GET</td><td><code>/admin/v1/servers/config/{name}</code></td><td>Get server config (secrets redacted)</td></tr>
<tr><td>PUT</td><td><code>/admin/v1/servers/update/{name}</code></td><td>Update server config (merge semantics, reconnects)</td></tr>
<tr><td>DELETE</td><td><code>/admin/v1/servers/remove/{name}</code></td><td>Remove server and disconnect</td></tr>
</table>

<h3>Agents</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET</td><td><code>/admin/v1/agents</code></td><td>List all agents</td></tr>
<tr><td>POST</td><td><code>/admin/v1/agents</code></td><td>Create agent (with optional token)</td></tr>
<tr><td>GET</td><td><code>/admin/v1/agents/{id}</code></td><td>Get single agent</td></tr>
<tr><td>PUT</td><td><code>/admin/v1/agents/{id}</code></td><td>Update agent</td></tr>
<tr><td>DELETE</td><td><code>/admin/v1/agents/{id}</code></td><td>Delete agent</td></tr>
<tr><td>PUT</td><td><code>/admin/v1/agents/{id}/servers</code></td><td>Update allowed servers</td></tr>
<tr><td>POST</td><td><code>/admin/v1/agents/{id}/rotate-token</code></td><td>Rotate Bearer token (new token shown once)</td></tr>
<tr><td>POST</td><td><code>/admin/v1/agents/{id}/reveal-token</code></td><td>Returns 410 Gone — tokens cannot be revealed (hashed at rest)</td></tr>
</table>

<h3>Rules</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET</td><td><code>/admin/v1/rules</code></td><td>List all rules</td></tr>
<tr><td>POST</td><td><code>/admin/v1/rules</code></td><td>Create rule</td></tr>
<tr><td>DELETE</td><td><code>/admin/v1/rules/{index}</code></td><td>Delete rule by index</td></tr>
</table>

<h3>Approvals</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET</td><td><code>/admin/v1/approvals</code></td><td>List pending + history</td></tr>
<tr><td>POST</td><td><code>/admin/v1/approvals/{id}/approve</code></td><td>Approve pending request</td></tr>
<tr><td>POST</td><td><code>/admin/v1/approvals/{id}/deny</code></td><td>Deny pending request</td></tr>
</table>

<h3>Tool Reviews</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET</td><td><code>/admin/v1/tools/reviews</code></td><td>List all tool review statuses</td></tr>
<tr><td>GET</td><td><code>/admin/v1/tools/{name}/review</code></td><td>Get review status for a tool</td></tr>
<tr><td>POST</td><td><code>/admin/v1/tools/{name}/review</code></td><td>Mark tool as reviewed (with optional note, acknowledged signals)</td></tr>
</table>

<h3>Recommendations</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET</td><td><code>/admin/v1/recommendations</code></td><td>Get recommendations</td></tr>
<tr><td>POST</td><td><code>/admin/v1/recommendations/{id}/apply</code></td><td>Apply recommendation</td></tr>
<tr><td>POST</td><td><code>/admin/v1/recommendations/{id}/dismiss</code></td><td>Dismiss recommendation</td></tr>
</table>

<h3>Runtime Config</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET</td><td><code>/admin/v1/config/runtime</code></td><td>Get current runtime settings</td></tr>
<tr><td>PUT</td><td><code>/admin/v1/config/runtime</code></td><td>Hot-reload settings without restart. Fields: <code>mcpHeaderMode</code> (passthrough/generate/managed), <code>approvalTtl</code> (e.g. "5m"), <code>allowedOrigins</code> (string array)</td></tr>
</table>

<h3>Platform</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>POST</td><td><code>/admin/v1/platform/connect</code></td><td>Connect to Recall Platform</td></tr>
<tr><td>GET</td><td><code>/admin/v1/platform/products</code></td><td>List platform products</td></tr>
<tr><td>POST</td><td><code>/admin/v1/platform/sync</code></td><td>Sync proxy data to platform</td></tr>
<tr><td>GET</td><td><code>/admin/v1/platform/summary</code></td><td>Platform dashboard data</td></tr>
</table>

<h3>Export</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET</td><td><code>/admin/v1/export/audit</code></td><td>Export audit log (JSON)</td></tr>
<tr><td>GET</td><td><code>/admin/v1/export-config</code></td><td>Export current config as YAML</td></tr>
</table>

<h3>Webhook Config</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET</td><td><code>/admin/v1/config/webhook</code></td><td>Get current webhook URL</td></tr>
<tr><td>PUT</td><td><code>/admin/v1/config/webhook</code></td><td>Set webhook URL</td></tr>
<tr><td>POST</td><td><code>/admin/v1/config/webhook-test</code></td><td>Test webhook delivery</td></tr>
</table>

<h3>Observability</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET</td><td><code>/metrics</code></td><td>Prometheus metrics (requires <code>metrics_enabled: true</code> in config)</td></tr>
<tr><td>GET</td><td><code>/dashboard/auth-status</code></td><td>Returns <code>{"authEnabled": true/false}</code></td></tr>
</table>

<h3>Security</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>POST</td><td><code>/admin/v1/revoke</code></td><td>Revoke a bearer token (SHA-256 hash)</td></tr>
<tr><td>POST</td><td><code>/token</code></td><td>RFC 8693 token exchange (delegation)</td></tr>
<tr><td>GET</td><td><code>/admin/v1/audit/verify</code></td><td>Verify audit hash chain integrity</td></tr>
<tr><td>GET</td><td><code>/.well-known/oauth-protected-resource</code></td><td>Protected Resource Metadata</td></tr>
</table>

<h3>Approval (Signed URLs)</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>GET/POST</td><td><code>/approvals/{id}/{action}?sig=...</code></td><td>Signed approval URL (browser click or programmatic)</td></tr>
<tr><td>POST</td><td><code>/slack/callback</code></td><td>Slack interactive payload handler</td></tr>
</table>

<h3>MCP Endpoint</h3>
<table>
<tr><th>Method</th><th>Path</th><th>Description</th></tr>
<tr><td>POST</td><td><code>/mcp</code></td><td>MCP Streamable HTTP endpoint (agents connect here)</td></tr>
</table>
</div>`,
  frameworks: `<div class="docs-content">
<h2>Framework Guide</h2>
<p>Connect any MCP-compatible agent to the proxy. The proxy exposes a standard MCP Streamable HTTP endpoint at <code>/mcp</code>.</p>

<h3>Claude Code / Claude Desktop</h3>
<pre><code># Add to claude_desktop_config.json
{
  "mcpServers": {
    "recall-mcp-gateway": {
      "url": "http://localhost:8090/mcp"
    }
  }
}</code></pre>

<h3>Cursor</h3>
<pre><code># Add to .cursor/mcp.json
{
  "mcpServers": {
    "recall-mcp-gateway": {
      "url": "http://localhost:8090/mcp"
    }
  }
}</code></pre>

<h3>LangChain (Python)</h3>
<pre><code>from langchain_mcp_adapters import MCPToolkit

toolkit = MCPToolkit(
    server_url="http://localhost:8090/mcp",
    headers={"Authorization": "Bearer rp_your_token"}
)
tools = toolkit.get_tools()</code></pre>

<h3>CrewAI</h3>
<pre><code>from crewai import Agent
from crewai_tools import MCPTool

mcp_tool = MCPTool(
    server_url="http://localhost:8090/mcp",
    headers={"Authorization": "Bearer rp_your_token"}
)

agent = Agent(
    role="Researcher",
    tools=[mcp_tool]
)</code></pre>

<h3>n8n</h3>
<pre><code>MCP Server Configuration:
  Server URL: http://recall-mcp-gateway:8090/mcp
  Transport: Streamable HTTP
  Authentication: Bearer Token
  Token: rp_your_token</code></pre>

<h3>ServiceNow</h3>
<pre><code>MCP Server Connection:
  URL: http://recall-mcp-gateway:8090/mcp
  Authentication Type: Bearer Token
  Token: rp_your_token</code></pre>

<h3>Custom (Any Language)</h3>
<p>Any MCP client that supports Streamable HTTP transport works. Send requests to <code>POST /mcp</code>.</p>

<p><b>Modern clients (MCP 2026-07-28, stateless):</b></p>
<pre><code>POST /mcp HTTP/1.1
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
}</code></pre>
<p>No session, no <code>initialize</code> handshake. Each request is self-contained.</p>

<p><b>Legacy clients (pre-2026, session-based):</b></p>
<pre><code>POST /mcp HTTP/1.1
Content-Type: application/json
Authorization: Bearer rp_your_token

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2024-11-05",
    "clientInfo": {"name": "my-agent", "version": "1.0"},
    "capabilities": {}
  }
}</code></pre>
<p>The response includes <code>Mcp-Session-Id</code> header — include it in all subsequent requests. The gateway supports both modes transparently.</p>
</div>`,
};
