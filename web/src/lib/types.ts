export interface SignalEntry {
  name: string
  category: string
  severity: string
  message: string
}

export interface Entry {
  timestamp: string
  requestId: string
  agentId: string
  userId?: string
  actingFor?: string
  delegationDepth?: number
  orgId?: string
  tool: string
  server: string
  decision: string
  decisionReason?: string
  matchedRule?: string
  matchedRuleIndex: number
  matchedRuleAction?: string
  latencyMs: number
  arguments?: Record<string, unknown>
  result_preview?: string
  isError?: boolean
  errorMessage?: string
  errorCategory?: string
  mode: string
  costPerCall?: number
  engineDecisionId?: string
  signals?: SignalEntry[]
  identitySource?: string
  protocolEra?: string
  archetype?: string
  traceId?: string
  hash?: string
  consentPhase?: string
}

export interface ConnectionEvent {
  type: string
  timestamp: string
  details?: string
}

export interface Server {
  name: string
  status: string
  tool_count: number
  circuit_state: string
  reconnect_count: number
  server_era?: string
  last_reconnect_at?: string
  meanLatencyMs?: number
  errorRate?: number
  healthStatus?: string
  sampleCount?: number
  auth_type?: string
  events?: ConnectionEvent[]
}

export interface ToolProfile {
  name: string
  server: string
  classification: string
  riskLevel?: string
  riskReasons?: string[]
  inputRisk?: string
}

export interface ToolAnnotation {
  readOnlyHint?: boolean
  destructiveHint?: boolean
  idempotentHint?: boolean
  openWorldHint?: boolean
  title?: string
  description?: string
}

export interface InitMessage {
  type: 'init'
  version: string
  servers: Server[]
  tools: ToolProfile[]
  toolAnnotations: Record<string, ToolAnnotation>
  toolProfiles?: Record<string, string[]>
  recent: Entry[]
  user: string
  defaultAction?: string
  mcpHeaderMode?: string
  authMode?: string
  tracingEnabled: boolean
  tracingEndpoint?: string
  metricsEnabled: boolean
  approvalSecretConfigured: boolean
  slackSecretConfigured: boolean
  allowedOrigins?: string[]
  approvalTtl?: string
  approvalStorage?: string
  clusterMode: boolean
  dashboardAuth?: boolean
  authIssuer?: string
  authAudience?: string
  authJwksUrl?: string
  maxDelegationDepth?: number
}

export interface EntryMessage {
  type: 'entry'
  entry: Entry
}

export type SSEMessage = InitMessage | EntryMessage

export type TabId = 'discover' | 'understand' | 'control' | 'docs' | 'settings'
