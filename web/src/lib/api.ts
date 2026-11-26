async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(path, {
    credentials: 'same-origin',
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...init?.headers,
    },
  })
  if (!resp.ok) {
    throw new Error(`API ${resp.status}: ${resp.statusText}`)
  }
  return resp.json()
}

export function fetchStatus() {
  return apiFetch<Record<string, unknown>>('/admin/v1/status')
}

export function fetchRules() {
  return apiFetch<{ rules: Record<string, unknown>[]; count: number }>('/admin/v1/rules')
}

export function fetchAgents() {
  return apiFetch<{ agents: Record<string, unknown>[]; count: number }>('/admin/v1/agents')
}

export function fetchApprovals() {
  return apiFetch<{ approvals: Record<string, unknown>[] | null; count: number; history: Record<string, unknown>[] | null }>(
    '/admin/v1/approvals'
  )
}

export function fetchAuthStatus() {
  return apiFetch<{ authEnabled: boolean }>('/dashboard/auth-status')
}

export interface AgentConfig {
  id: string
  archetype?: string
  product?: string
  allowedServers?: string[]
  serverAccessRestricted?: boolean
  blockedTools?: string[]
  hiddenTools?: string[]
  acknowledgedTools?: string[]
  hasToken?: boolean
  source?: string
  orgId?: string
}

export interface ServerConfig {
  name: string
  url?: string
  transport?: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  headers?: Record<string, string>
  costPerCall?: number
  auth?: {
    type: string
    token?: string
    username?: string
    password?: string
    headers?: Record<string, string>
    tokenUrl?: string
    clientId?: string
    clientSecret?: string
    scopes?: string[]
    clientCert?: string
    clientKey?: string
    caCert?: string
    issuer?: string
  }
  serverType?: string
  mcpHeaderMode?: string
  toolAnnotations?: Array<{
    tool: string
    title?: string
    readOnlyHint?: boolean
    destructiveHint?: boolean
    idempotentHint?: boolean
    openWorldHint?: boolean
  }>
}

export interface ConnectionEvent {
  type: string
  time: string
  detail?: string
}

export function fetchAgentConfigs() {
  return apiFetch<{ agents: AgentConfig[]; count: number }>('/admin/v1/agents')
}

export interface BaselineEntry {
  agentId: string
  tool: string
  baseline: {
    sampleCount: number
    baselineReady: boolean
    baselineSource?: string
  }
}

export function fetchBaselines() {
  return apiFetch<{ baselines: BaselineEntry[] }>('/admin/v1/baselines')
}

export interface RuleEntry {
  tool: string
  server?: string
  action: string
  actions?: Record<string, string>
  agents?: string[]
  rateLimit?: number
  ratePeriod?: string
  reason?: string
  notify?: string
  retryMethod?: string
  retryHintMs?: number
  statusWebhook?: string
  consentForm?: { title?: string; description?: string; fields?: string[] }
  delegationNotify?: string
  requiredScopes?: string[]
}

export function fetchRulesConfig() {
  return apiFetch<{ rules: RuleEntry[]; count: number }>('/admin/v1/rules')
}

// --- Audit Stats (dashboard) ---

export interface TopAgentStat {
  id: string
  count: number
  errors: number
}

export interface TopToolStat {
  name: string
  count: number
  server: string
}

export interface HourlyBucket {
  hour: string
  total: number
  errors: number
}

export interface AuditStatsResponse {
  totalCount: number
  blockedCount: number
  signalCounts: Record<string, number>
  topAgents: TopAgentStat[] | null
  topTools: TopToolStat[] | null
  avgLatencyMs: number
  uniqueTools: number
  uniqueAgents: number
  errorCount: number
  hourlyActivity: HourlyBucket[] | null
}

export function fetchAuditStats(from: string) {
  const qs = new URLSearchParams()
  qs.set('from', from)
  return apiFetch<AuditStatsResponse>(`/admin/v1/audit/stats?${qs.toString()}`)
}

export interface AuditSearchParams {
  tool?: string
  agent?: string
  decision?: string
  server?: string
  from?: string
  to?: string
  limit?: number
}

export function fetchAuditSearch(params: AuditSearchParams) {
  const qs = new URLSearchParams()
  if (params.tool) qs.set('tool', params.tool)
  if (params.agent) qs.set('agent', params.agent)
  if (params.decision) qs.set('decision', params.decision)
  if (params.server) qs.set('server', params.server)
  if (params.from) qs.set('from', params.from)
  if (params.to) qs.set('to', params.to)
  if (params.limit) qs.set('limit', String(params.limit))
  return apiFetch<{ entries: import('./types').Entry[]; count: number }>(
    `/admin/v1/audit/search?${qs.toString()}`
  )
}

// --- Understand tab ---

export interface Recommendation {
  id: string
  type: string
  severity: string
  title: string
  description: string
  evidence?: string
  suggestedRule?: { tool: string; action: string; rate_limit?: string; reason?: string; server?: string }
  impactScore: number
  source?: string
  status: string
  actionMode?: string
  createdAt?: string
}

export function fetchRecommendations() {
  return apiFetch<{ recommendations: Recommendation[]; count: number; dismissedCount: number }>('/admin/v1/recommendations')
}

export function dismissRecommendation(id: string) {
  return apiFetch<{ status: string }>(`/admin/v1/recommendations/${encodeURIComponent(id)}/dismiss`, { method: 'POST' })
}

export function applyRecommendation(id: string) {
  return apiFetch<{ status: string }>(`/admin/v1/recommendations/${encodeURIComponent(id)}/apply`, { method: 'POST' })
}

export interface SequenceAlert {
  pattern: string
  agent: string
  tools: string[]
  count: number
  lastSeen?: string
  message?: string
  asserted?: boolean
}

export function fetchSequences() {
  return apiFetch<{ sequences: SequenceAlert[]; count: number }>('/admin/v1/sequences')
}

export function assertWorkflow(agentId: string, steps: string[]) {
  return apiFetch<{ status: string }>('/admin/v1/workflows/expected', {
    method: 'POST',
    body: JSON.stringify({ agentId, steps }),
  })
}

export interface CompliancePreview {
  score?: number
  band?: string
  note?: string
  source?: string
  scoreTrend?: Array<{ date: string; score: number; band: string; filled?: boolean }>
  components?: { toolGovernance: number; signalSafety: number; operationalReadiness: number }
  details?: {
    governedByRule?: number
    governedByBlocked?: number
    governedByAcknowledged?: number
    governedHighCritical?: number
    totalHighCritical?: number
    totalAgents?: number
    toolDetails?: Array<{ name?: string; governed: boolean; dominantRoute?: string }>
    opsChecklist?: Record<string, boolean>
  }
}

export function fetchInsights() {
  return apiFetch<{ insights: unknown[] | null; compliancePreview: CompliancePreview; count: number }>('/admin/v1/insights')
}

// --- Settings ---

export function saveRuntimeConfig(data: Record<string, string>) {
  return apiFetch<{ status: string }>('/admin/v1/config/runtime', {
    method: 'PUT', body: JSON.stringify(data),
  })
}

export function saveWebhookConfig(data: { approvalWebhookUrl?: string; alertWebhookUrl?: string }) {
  return apiFetch<{ status: string }>('/admin/v1/config/webhook', {
    method: 'PUT', body: JSON.stringify(data),
  })
}

export function testWebhook(url: string) {
  return apiFetch<{ status: string }>('/admin/v1/config/webhook-test', {
    method: 'POST', body: JSON.stringify({ url, message: 'Recall MCP Gateway webhook test' }),
  })
}

export interface AuditInfo {
  storage: string; path?: string; entryCount: number; sizeBytes?: number
  oldestEntry?: string; retention?: string; maxEntries?: number;
}

export function fetchAuditInfo() {
  return apiFetch<AuditInfo>('/admin/v1/audit/info')
}

export function saveAuditConfig(maxEntries: number) {
  return apiFetch<{ status: string }>('/admin/v1/audit/config', {
    method: 'PUT', body: JSON.stringify({ maxEntries }),
  })
}

export function purgeAudit() {
  return apiFetch<{ status: string }>('/admin/v1/audit/purge', { method: 'DELETE' })
}

export function connectPlatform(data: Record<string, string>) {
  return apiFetch<{
    status?: string; error?: string; engineConnected?: boolean
    engineWarning?: string; products?: Array<{ id: string; name: string }>
    consoleUrl?: string
  }>('/admin/v1/platform/connect', { method: 'POST', body: JSON.stringify(data) })
}

export function syncToPlatform(productId: string) {
  return apiFetch<{
    status: string; synced?: Record<string, unknown>
    totals?: { errors?: number }; consoleUrl?: string
  }>('/admin/v1/platform/sync', { method: 'POST', body: JSON.stringify({ productId }) })
}

// --- Server CRUD ---

export function fetchServerConfig(name: string) {
  return apiFetch<ServerConfig>(`/admin/v1/servers/config/${encodeURIComponent(name)}`)
}

export function addServer(config: ServerConfig) {
  return apiFetch<{ status: string; name: string; toolCount: number }>('/admin/v1/servers/add', {
    method: 'POST',
    body: JSON.stringify(config),
  })
}

export function updateServer(name: string, config: Partial<ServerConfig>) {
  return apiFetch<{ status: string; name: string; toolCount: number }>(
    `/admin/v1/servers/update/${encodeURIComponent(name)}`,
    { method: 'PUT', body: JSON.stringify(config) },
  )
}

export function removeServer(name: string) {
  return apiFetch<{ status: string; name: string }>(
    `/admin/v1/servers/remove/${encodeURIComponent(name)}`,
    { method: 'DELETE' },
  )
}

// --- Agent CRUD ---

export function createAgent(data: { id: string; archetype?: string; product?: string; generateToken?: boolean }) {
  return apiFetch<{ status: string; agentId: string; token?: string }>('/admin/v1/agents', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export function deleteAgent(id: string) {
  return apiFetch<{ status: string; agentId: string }>(
    `/admin/v1/agents/${encodeURIComponent(id)}`,
    { method: 'DELETE' },
  )
}

export function updateAgentServers(id: string, allowedServers: string[]) {
  return apiFetch<{ status: string; agentId: string; allowedServers: string[] }>(
    `/admin/v1/agents/${encodeURIComponent(id)}/servers`,
    { method: 'PUT', body: JSON.stringify({ allowedServers }) },
  )
}

export function updateAgentTools(id: string, tools: { blockedTools?: string[]; hiddenTools?: string[]; acknowledgedTools?: string[] }) {
  return apiFetch<{ status: string; agentId: string }>(
    `/admin/v1/agents/${encodeURIComponent(id)}/tools`,
    { method: 'PUT', body: JSON.stringify(tools) },
  )
}

export function updateAgentProduct(id: string, product: string) {
  return apiFetch<{ status: string; agentId: string }>(
    `/admin/v1/agents/${encodeURIComponent(id)}`,
    { method: 'PUT', body: JSON.stringify({ product }) },
  )
}

export function rotateAgentToken(id: string) {
  return apiFetch<{ status: string; agentId: string; token: string }>(
    `/admin/v1/agents/${encodeURIComponent(id)}/rotate-token`,
    { method: 'POST' },
  )
}

// --- Rules CRUD ---

export function createRule(rule: Omit<RuleEntry, 'rateLimit' | 'ratePeriod'> & { rate_limit?: string }) {
  return apiFetch<{ status: string; rule: RuleEntry; index: number }>('/admin/v1/rules', {
    method: 'POST',
    body: JSON.stringify(rule),
  })
}

export function deleteRule(index: number) {
  return apiFetch<{ status: string }>(`/admin/v1/rules/${index}`, { method: 'DELETE' })
}

export function setDefaultAction(action: 'allow' | 'block') {
  return apiFetch<{ status: string }>('/admin/v1/rules', {
    method: 'POST',
    body: JSON.stringify({ tool: '*', action }),
  })
}
