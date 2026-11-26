import { useState, useCallback, useEffect, useMemo } from 'react'
import { useGatewayStore } from '@/stores/gateway-store'
import { useFeedStore } from '@/stores/feed-store'
import {
  fetchAgentConfigs,
  fetchRulesConfig,
  createAgent,
  deleteAgent,
  updateAgentServers,
  rotateAgentToken,
  type AgentConfig,
  type RuleEntry,
} from '@/lib/api'
import type { ToolProfile } from '@/lib/types'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

const archetypeLabels: Record<string, string> = {
  autonomous: 'Autonomous — M2M, admin approves',
  user_embedded: 'User-embedded — interactive, consent forms',
  delegated: 'Delegated — acting for user',
}

const classLabels: Record<string, string> = {
  READ_ONLY: 'READ', WRITE: 'WRITE', DESTRUCTIVE: 'DELETE',
  EXTERNAL_COMM: 'EXTERNAL', CODE_EXECUTION: 'EXEC', DATA_TRANSFER: 'TRANSFER',
}

function classColor(cls: string): string {
  switch (cls) {
    case 'DESTRUCTIVE': case 'CODE_EXECUTION': return 'var(--red)'
    case 'WRITE': case 'DATA_TRANSFER': return 'var(--amber)'
    case 'EXTERNAL_COMM': return 'var(--purple)'
    default: return 'var(--muted)'
  }
}

// Determine tool status for a specific agent
function toolStatus(
  toolName: string, agentId: string, agent: AgentConfig, rules: RuleEntry[],
): { color: string; icon: string; label: string } {
  if ((agent.blockedTools || []).includes(toolName))
    return { color: 'var(--red)', icon: '✕', label: 'blocked' }
  if ((agent.hiddenTools || []).includes(toolName))
    return { color: 'var(--muted)', icon: '◌', label: 'hidden' }

  // Check rules
  for (const r of rules) {
    if (r.tool === '*') continue
    let matches = r.tool === toolName
    if (!matches && r.tool?.includes('*')) {
      try { matches = new RegExp('^' + r.tool.replace(/\*/g, '.*') + '$').test(toolName) } catch { /* ignore */ }
    }
    if (!matches) continue
    const ra = r.agents || []
    if (ra.length > 0 && !ra.includes(agentId)) continue
    // Matched rule
    if (r.action === 'block') return { color: 'var(--red)', icon: '✕', label: 'blocked' }
    if (r.action === 'require_approval') return { color: 'var(--purple)', icon: '◎', label: 'approval' }
    if (r.action === 'require_consent') return { color: 'var(--purple)', icon: '◎', label: 'consent' }
    if (r.action === 'rate_limit') return { color: 'var(--amber)', icon: '◔', label: 'rate limited' }
    if (r.action === 'hide') return { color: 'var(--muted)', icon: '◌', label: 'hidden' }
  }

  return { color: 'var(--green)', icon: '✔', label: 'allowed' }
}

function sourceBadge(source?: string) {
  const isConfig = source === 'config' || source === 'config.yaml'
  return (
    <span style={{
      fontSize: '10px', padding: '2px 6px', borderRadius: '3px',
      background: isConfig
        ? 'color-mix(in srgb, var(--muted) 15%, transparent)'
        : 'color-mix(in srgb, var(--green) 15%, transparent)',
      color: isConfig ? 'var(--muted)' : 'var(--green)',
      fontFamily: MONO, fontWeight: 600,
    }}>
      {isConfig ? 'config.yaml' : 'dashboard'}
    </span>
  )
}

export function AgentsList() {
  const servers = useGatewayStore(s => s.servers)
  const tools = useGatewayStore(s => s.tools)
  const hasAS = !!useGatewayStore(s => s.authIssuer)
  const agentCounts = useFeedStore(s => s.agentCounts)
  const entries = useFeedStore(s => s.entries)
  const [agents, setAgents] = useState<AgentConfig[]>([])
  const [rules, setRules] = useState<RuleEntry[]>([])
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [showCreate, setShowCreate] = useState(false)
  const [formId, setFormId] = useState('')
  const [formArchetype, setFormArchetype] = useState('autonomous')
  const [formProduct, setFormProduct] = useState('')
  const [formAuthMethod, setFormAuthMethod] = useState<'bearer' | 'jwt'>('bearer')
  const [formGenToken, setFormGenToken] = useState(true)
  const [newToken, setNewToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadData = useCallback(async () => {
    try {
      const [agentsData, rulesData] = await Promise.all([fetchAgentConfigs(), fetchRulesConfig()])
      setAgents(agentsData.agents || [])
      setRules(rulesData.rules || [])
    } catch (e) {
      console.error('Failed to load agents:', e)
    }
  }, [])

  useEffect(() => { loadData() }, [loadData])

  const toggleExpand = useCallback((id: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const handleCreate = async () => {
    if (!formId.trim()) return
    try {
      setLoading(true); setError(null)
      const resp = await createAgent({
        id: formId.trim(),
        archetype: formArchetype,
        product: formProduct || undefined,
        generateToken: formGenToken,
      })
      if (resp.token) setNewToken(resp.token)
      setShowCreate(false); setFormId(''); setFormArchetype('autonomous'); setFormProduct('')
      await loadData()
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  const handleDelete = async (id: string) => {
    if (!confirm(`Delete agent "${id}"?`)) return
    try {
      await deleteAgent(id)
      await loadData()
    } catch (e) {
      setError(String(e))
    }
  }

  const handleServerToggle = async (agent: AgentConfig, serverName: string, enabled: boolean) => {
    const current = agent.allowedServers || []
    const updated = enabled
      ? [...current, serverName]
      : current.filter(s => s !== serverName)
    try {
      await updateAgentServers(agent.id, updated)
      await loadData()
    } catch (e) {
      setError(String(e))
    }
  }

  const handleRotateToken = async (id: string) => {
    if (!confirm(`Rotate token for "${id}"? The old token will be invalidated.`)) return
    try {
      const resp = await rotateAgentToken(id)
      setNewToken(resp.token)
      await loadData()
    } catch (e) {
      setError(String(e))
    }
  }

  const inputStyle: React.CSSProperties = {
    background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius)',
    color: 'var(--text)', padding: '6px 10px', fontSize: '13px', width: '100%',
    outline: 'none', fontFamily: MONO,
  }

  const labelStyle: React.CSSProperties = {
    fontSize: '11px', color: 'var(--muted)', marginBottom: '4px', display: 'block',
    textTransform: 'uppercase', letterSpacing: '0.05em', fontWeight: 600,
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0' }}>
      {/* Header */}
      <div style={{
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        padding: '12px 20px', borderBottom: '1px solid var(--border)',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2" /><circle cx="12" cy="7" r="4" />
          </svg>
          <span style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text)' }}>Agent Permissions</span>
        </div>
        <button onClick={() => { setShowCreate(true); setError(null); setNewToken(null) }} style={{
          padding: '6px 14px', borderRadius: 'var(--radius)', fontSize: '13px', fontWeight: 500,
          cursor: 'pointer', border: 'none', background: 'var(--accent)', color: '#fff',
        }}>
          + Create Agent
        </button>
      </div>

      {/* Token display */}
      {newToken && (
        <div style={{
          padding: '12px 20px', borderBottom: '1px solid var(--border)',
          background: 'color-mix(in srgb, var(--green) 8%, var(--bg))',
        }}>
          <div style={{ fontSize: '11px', color: 'var(--green)', fontWeight: 600, marginBottom: '4px' }}>
            Agent Token (copy now — won't be shown again)
          </div>
          <div style={{ display: 'flex', gap: '8px', alignItems: 'stretch' }}>
            <div style={{
              flex: 1, fontFamily: MONO, fontSize: '12px', color: 'var(--text)', padding: '6px 10px',
              background: 'var(--bg)', borderRadius: 'var(--radius)', border: '1px solid var(--border)',
              wordBreak: 'break-all', userSelect: 'all',
            }}>
              {newToken}
            </div>
            <button onClick={() => { navigator.clipboard.writeText(newToken); setCopied(true); setTimeout(() => setCopied(false), 2000) }} style={{
              padding: '6px 14px', fontSize: '12px', fontWeight: 500, cursor: 'pointer',
              border: '1px solid var(--border)', borderRadius: 'var(--radius)',
              background: copied ? 'var(--green)' : 'var(--surface)', color: copied ? 'white' : 'var(--text)',
              whiteSpace: 'nowrap', transition: 'all 0.15s',
            }}>
              {copied ? 'Copied!' : 'Copy'}
            </button>
          </div>
          <button onClick={() => setNewToken(null)} style={{
            marginTop: '6px', fontSize: '11px', color: 'var(--muted)', background: 'transparent',
            border: 'none', cursor: 'pointer',
          }}>
            Dismiss
          </button>
        </div>
      )}

      {/* Create form */}
      {showCreate && (
        <div style={{
          padding: '16px 20px', borderBottom: '1px solid var(--border)',
        }}>
          {/* Row 1: ID, Archetype, Product */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '12px', marginBottom: '12px' }}>
            <div>
              <label style={labelStyle}>Agent ID</label>
              <input style={inputStyle} value={formId} placeholder="e.g., pipeline-bot"
                onChange={e => setFormId(e.target.value)} />
            </div>
            <div>
              <label style={labelStyle}>Archetype</label>
              <select style={inputStyle} value={formArchetype}
                onChange={e => setFormArchetype(e.target.value)}>
                <option value="autonomous">Autonomous — M2M, no user present</option>
                <option value="user_embedded">User-embedded — interactive</option>
                <option value="delegated">Delegated — acting for user</option>
              </select>
            </div>
            <div>
              <label style={labelStyle}>Product (optional)</label>
              <input style={inputStyle} value={formProduct} placeholder="e.g., ServiceNow"
                onChange={e => setFormProduct(e.target.value)} />
            </div>
          </div>

          {/* Auth method */}
          <div style={{ marginBottom: '12px' }}>
            <label style={labelStyle}>Auth Method</label>
            <div style={{ display: 'flex', alignItems: 'center', gap: '16px', fontSize: '13px', color: 'var(--text)' }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: '5px', cursor: 'pointer' }}>
                <input type="radio" name="auth-method" checked={formAuthMethod === 'bearer'}
                  onChange={() => { setFormAuthMethod('bearer'); setFormGenToken(true) }} />
                Bearer token
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: '5px', cursor: hasAS ? 'pointer' : 'default', color: hasAS ? 'var(--text)' : 'var(--muted)' }}>
                <input type="radio" name="auth-method" checked={formAuthMethod === 'jwt'}
                  disabled={!hasAS} onChange={() => { setFormAuthMethod('jwt'); setFormGenToken(false) }} />
                JWT (external AS)
              </label>
            </div>
          </div>

          {formAuthMethod === 'bearer' && (
            <label style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '13px', color: 'var(--text)', marginBottom: '12px', cursor: 'pointer' }}>
              <input type="checkbox" checked={formGenToken} onChange={e => setFormGenToken(e.target.checked)} />
              Generate API key
            </label>
          )}
          {formAuthMethod === 'jwt' && (
            <div style={{ fontSize: '12px', color: 'var(--muted)', marginBottom: '12px', padding: '8px 12px', background: 'var(--bg)', borderRadius: 'var(--radius)', border: '1px solid var(--border)' }}>
              Agent will authenticate via JWT from the external Authorization Server. No bearer token needed — identity comes from the JWT <code style={{ fontFamily: "'JetBrains Mono', monospace" }}>sub</code> claim.
            </div>
          )}

          {error && <div style={{ fontSize: '12px', color: 'var(--red)', marginBottom: '8px' }}>{error}</div>}

          <div style={{ display: 'flex', gap: '8px' }}>
            <button onClick={handleCreate} disabled={loading} style={{
              padding: '6px 14px', borderRadius: 'var(--radius)', fontSize: '13px', fontWeight: 500,
              cursor: 'pointer', border: 'none', background: 'var(--accent)', color: '#fff',
            }}>
              Create
            </button>
            <button onClick={() => { setShowCreate(false); setError(null) }} style={{
              padding: '6px 14px', borderRadius: 'var(--radius)', fontSize: '13px', fontWeight: 500,
              cursor: 'pointer', border: '1px solid var(--border)', background: 'transparent', color: 'var(--text)',
            }}>
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Empty state */}
      {agents.length === 0 && (
        <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--muted)', fontSize: '13px' }}>
          No agents registered yet.
        </div>
      )}

      {/* Agent rows */}
      {agents.map(agent => (
        <AgentRow
          key={agent.id}
          agent={agent}
          isExpanded={expanded.has(agent.id)}
          onToggle={() => toggleExpand(agent.id)}
          onDelete={() => handleDelete(agent.id)}
          onRotateToken={() => handleRotateToken(agent.id)}
          onServerToggle={(server, enabled) => handleServerToggle(agent, server, enabled)}
          servers={servers.map(s => s.name)}
          tools={tools}
          rules={rules}
          callCount={agentCounts[agent.id] || 0}
          entries={entries}
          onUpdate={loadData}
        />
      ))}
    </div>
  )
}

function AgentRow({ agent, isExpanded, onToggle, onDelete, onRotateToken, onServerToggle, onUpdate, servers, tools, rules, callCount, entries }: {
  agent: AgentConfig
  isExpanded: boolean
  onToggle: () => void
  onDelete: () => void
  onRotateToken: () => void
  onServerToggle: (server: string, enabled: boolean) => void
  onUpdate: () => void
  servers: string[]
  tools: ToolProfile[]
  rules: RuleEntry[]
  callCount: number
  entries: import('@/lib/types').Entry[]
}) {
  const [hovered, setHovered] = useState(false)
  const [toolPopup, setToolPopup] = useState<{ name: string; pos: { x: number; y: number } } | null>(null)
  const archetype = agent.archetype || 'autonomous'

  // Compute last-seen per tool for stale indicator (7 days)
  const STALE_MS = 7 * 24 * 60 * 60 * 1000
  const toolLastSeen = useMemo(() => {
    const map: Record<string, number> = {}
    for (const e of entries) {
      if (e.agentId !== agent.id) continue
      const ts = new Date(e.timestamp).getTime()
      if (!map[e.tool] || ts > map[e.tool]) map[e.tool] = ts
    }
    return map
  }, [entries, agent.id])
  const canDelete = agent.source !== 'config' && agent.source !== 'config.yaml'

  // Compute accessible servers
  const accessibleServers = useMemo(() => {
    if (!agent.serverAccessRestricted && (!agent.allowedServers || agent.allowedServers.length === 0)) {
      return servers // unrestricted = all
    }
    return servers.filter(s => (agent.allowedServers || []).includes(s))
  }, [agent, servers])

  const isUnrestricted = !agent.serverAccessRestricted && (!agent.allowedServers || agent.allowedServers.length === 0)

  // Effective permissions: tools grouped by server
  const effectivePerms = useMemo(() => {
    const result: { server: string; tools: { tool: ToolProfile; status: { color: string; icon: string; label: string } }[] }[] = []
    for (const serverName of accessibleServers) {
      const serverTools = tools.filter(t => t.server === serverName)
      const perms = serverTools.map(t => ({
        tool: t,
        status: toolStatus(t.name, agent.id, agent, rules),
      }))
      // Sort: allowed first, then others
      const statusOrder: Record<string, number> = { allowed: 0, approval: 1, consent: 1, 'rate limited': 2, blocked: 3, hidden: 4 }
      perms.sort((a, b) => (statusOrder[a.status.label] ?? 5) - (statusOrder[b.status.label] ?? 5))
      if (perms.length > 0) result.push({ server: serverName, tools: perms })
    }
    return result
  }, [accessibleServers, tools, agent, rules])

  // Stats: count by status
  const stats = useMemo(() => {
    let allowed = 0, blocked = 0, approval = 0, rateLimited = 0
    for (const sp of effectivePerms) {
      for (const t of sp.tools) {
        if (t.status.label === 'allowed') allowed++
        else if (t.status.label === 'blocked') blocked++
        else if (t.status.label === 'approval' || t.status.label === 'consent') approval++
        else if (t.status.label === 'rate limited') rateLimited++
      }
    }
    return { allowed, blocked, approval, rateLimited }
  }, [effectivePerms])

  // Rules affecting this agent
  const affectingRules = useMemo(() => {
    return rules.filter(r => {
      const ra = r.agents || []
      return ra.length === 0 || ra.includes(agent.id)
    }).slice(0, 8)
  }, [rules, agent.id])

  const actionColor = (action: string) => {
    switch (action) {
      case 'block': return 'var(--red)'
      case 'require_approval': case 'require_consent': return 'var(--purple)'
      case 'rate_limit': return 'var(--amber)'
      case 'hide': return 'var(--muted)'
      default: return 'var(--green)'
    }
  }

  return (
    <div
      style={{
        borderBottom: '1px solid var(--border)',
        background: hovered ? 'var(--surface-hover)' : 'transparent',
        transition: 'background 0.1s',
      }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {/* Header */}
      <div onClick={onToggle} style={{
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        padding: '10px 20px', cursor: 'pointer', gap: '8px',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', minWidth: 0 }}>
          <span style={{
            fontSize: '13px', fontWeight: 400, color: 'var(--text)', fontFamily: MONO,
            overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
          }}>
            {agent.id}
          </span>
          {sourceBadge(agent.source)}
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexShrink: 0 }}>
          {/* Stats chips */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '4px', fontSize: '11px', color: 'var(--muted)' }}>
            {stats.allowed > 0 && (
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px' }}>
                <span style={{ width: '6px', height: '6px', borderRadius: '50%', background: 'var(--green)', display: 'inline-block' }} />
                <span style={{ color: 'var(--green)', fontFamily: MONO }}>{stats.allowed}</span>
                <span>tools</span>
              </span>
            )}
            {stats.blocked > 0 && (
              <>
                <span style={{ color: 'var(--border)' }}>·</span>
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px' }}>
                  <span style={{ width: '6px', height: '6px', borderRadius: '50%', background: 'var(--red)', display: 'inline-block' }} />
                  <span style={{ color: 'var(--red)', fontFamily: MONO }}>{stats.blocked}</span>
                  <span>blocked</span>
                </span>
              </>
            )}
            {stats.approval > 0 && (
              <>
                <span style={{ color: 'var(--border)' }}>·</span>
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px' }}>
                  <span style={{ width: '6px', height: '6px', borderRadius: '50%', background: 'var(--purple)', display: 'inline-block' }} />
                  <span style={{ color: 'var(--purple)', fontFamily: MONO }}>{stats.approval}</span>
                  <span>approval</span>
                </span>
              </>
            )}
            {stats.rateLimited > 0 && (
              <>
                <span style={{ color: 'var(--border)' }}>·</span>
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px' }}>
                  <span style={{ width: '6px', height: '6px', borderRadius: '50%', background: 'var(--amber)', display: 'inline-block' }} />
                  <span style={{ color: 'var(--amber)', fontFamily: MONO }}>{stats.rateLimited}</span>
                  <span>rate-limited</span>
                </span>
              </>
            )}
            <span style={{ color: 'var(--border)' }}>·</span>
            <span style={{ fontFamily: MONO }}>{callCount} calls</span>
          </div>
          {hovered && canDelete && (
            <button onClick={e => { e.stopPropagation(); onDelete() }} style={{
              padding: '3px 10px', borderRadius: 'var(--radius)', fontSize: '11px',
              background: 'color-mix(in srgb, var(--red) 10%, transparent)',
              color: 'var(--red)', border: '1px solid color-mix(in srgb, var(--red) 30%, transparent)',
              cursor: 'pointer', fontWeight: 500,
            }}>
              delete
            </button>
          )}
          <span style={{
            fontSize: '10px', color: 'var(--muted)',
            transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
            transition: 'transform 0.15s', display: 'inline-block',
          }}>
            ▼
          </span>
        </div>
      </div>

      {/* Expanded */}
      {isExpanded && (
        <div style={{ padding: '0 20px 14px 20px' }}>
          {/* Archetype & Auth — two columns */}
          <div style={{
            display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px', marginBottom: '14px',
          }}>
            <div>
              <div style={{ fontSize: '10px', fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '4px' }}>
                Archetype
              </div>
              <div style={{ fontSize: '12px', color: 'var(--text)' }}>
                {archetypeLabels[archetype] || archetype}
              </div>
            </div>
            <div>
              <div style={{ fontSize: '10px', fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '4px' }}>
                Authentication
              </div>
              <div style={{ fontSize: '12px', color: 'var(--text)', display: 'flex', alignItems: 'center', gap: '8px' }}>
                {agent.hasToken ? (
                  <>
                    <span>Bearer token</span>
                    <span style={{ fontFamily: MONO, color: 'var(--muted)', letterSpacing: '2px' }}>••••••</span>
                    <span style={{ fontSize: '11px', color: 'var(--muted)' }}>(hashed)</span>
                    <button onClick={e => { e.stopPropagation(); onRotateToken() }} style={{
                      fontSize: '11px', color: 'var(--muted)', background: 'transparent',
                      border: '1px solid var(--border)', borderRadius: 'var(--radius)',
                      padding: '2px 8px', cursor: 'pointer',
                    }}>
                      ↻ Rotate
                    </button>
                  </>
                ) : (
                  <span style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                    <span style={{
                      fontSize: '10px', fontWeight: 600, padding: '2px 6px', borderRadius: '3px',
                      background: 'color-mix(in srgb, var(--accent) 12%, transparent)', color: 'var(--accent)',
                    }}>JWT</span>
                    External AS — identity from JWT <code style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: '11px' }}>sub</code> claim
                  </span>
                )}
              </div>
            </div>
          </div>

          {/* Product */}
          <div style={{ marginBottom: '14px' }}>
            <div style={{ fontSize: '10px', fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '4px' }}>
              Product
            </div>
            <div style={{ fontSize: '12px', color: agent.product ? 'var(--text)' : 'var(--muted)' }}>
              {agent.product || 'Not assigned'}
            </div>
          </div>

          {/* Server Access */}
          <div style={{ marginBottom: '14px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px' }}>
              <span style={{ fontSize: '10px', fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                Server Access
              </span>
              {isUnrestricted && (
                <span style={{ fontSize: '11px', fontWeight: 600, color: 'var(--green)' }}>(ALL)</span>
              )}
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
              {servers.map(serverName => {
                const allowed = isUnrestricted || (agent.allowedServers || []).includes(serverName)
                const serverToolCount = tools.filter(t => t.server === serverName).length
                return (
                  <label key={serverName} style={{
                    display: 'flex', alignItems: 'center', gap: '10px', fontSize: '13px',
                    color: 'var(--text)', cursor: 'pointer', padding: '2px 0',
                  }}>
                    {/* Toggle switch style */}
                    <div style={{
                      width: '34px', height: '18px', borderRadius: '9px', position: 'relative',
                      background: allowed ? 'var(--green)' : 'var(--border)', transition: 'background 0.2s',
                      flexShrink: 0, cursor: 'pointer',
                    }}
                      onClick={e => { e.preventDefault(); onServerToggle(serverName, !allowed) }}
                    >
                      <div style={{
                        width: '14px', height: '14px', borderRadius: '50%', background: '#fff',
                        position: 'absolute', top: '2px',
                        left: allowed ? '18px' : '2px', transition: 'left 0.2s',
                      }} />
                    </div>
                    <span style={{ fontFamily: MONO, fontWeight: 400 }}>{serverName}</span>
                    <span style={{ color: 'var(--muted)', fontSize: '12px' }}>{serverToolCount} tools</span>
                  </label>
                )
              })}
            </div>
          </div>

          {/* Effective Permissions */}
          {effectivePerms.length > 0 && (
            <div style={{ marginBottom: '14px' }}>
              <div style={{ fontSize: '10px', fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '6px' }}>
                Effective Permissions ({effectivePerms.reduce((n, g) => n + g.tools.length, 0)} tools)
              </div>
              {effectivePerms.map(group => (
                <div key={group.server} style={{ marginBottom: '8px' }}>
                  <div style={{ fontSize: '12px', color: 'var(--accent)', fontWeight: 500, marginBottom: '4px' }}>
                    {group.server}
                  </div>
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: '5px' }}>
                    {group.tools.map(({ tool, status }) => {
                      const clsLabel = classLabels[tool.classification] || tool.classification || 'READ'
                      const clsClr = classColor(tool.classification)
                      const chipBg = status.label === 'allowed' ? 'var(--bg)'
                        : `color-mix(in srgb, ${status.color} 8%, var(--bg))`
                      const lastSeen = toolLastSeen[tool.name]
                      const isStale = status.label === 'allowed' && (!lastSeen || (Date.now() - lastSeen) > STALE_MS)
                      return (
                        <span key={tool.name}
                          onClick={e => {
                            e.stopPropagation()
                            const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
                            setToolPopup({ name: tool.name, pos: { x: rect.left, y: rect.bottom + 4 } })
                          }}
                          style={{
                            display: 'inline-flex', alignItems: 'center', gap: '4px',
                            padding: '3px 7px', borderRadius: '4px', fontSize: '12px',
                            background: chipBg, border: '1px solid var(--border)',
                            fontFamily: MONO, color: 'var(--text)', cursor: 'pointer',
                            position: 'relative',
                          }}>
                          {isStale && (
                            <span title="No calls in 7 days" style={{
                              position: 'absolute', top: '-2px', right: '-2px',
                              width: '5px', height: '5px', borderRadius: '50%',
                              background: 'var(--amber)',
                            }} />
                          )}
                          <span style={{ color: status.color, fontSize: '13px', fontWeight: 600, lineHeight: 1 }}>{status.icon}</span>
                          {tool.name}
                          <span style={{
                            fontSize: '9px', fontWeight: 700, padding: '1px 4px', borderRadius: '2px',
                            background: `color-mix(in srgb, ${clsClr} 15%, transparent)`, color: clsClr,
                          }}>
                            {clsLabel}
                          </span>
                          {(agent.acknowledgedTools || []).includes(tool.name) && (
                            <span style={{
                              fontSize: '9px', padding: '1px 4px', borderRadius: '2px',
                              background: 'rgba(245,158,11,0.1)', color: 'var(--amber)', fontWeight: 600,
                            }}>
                              ack
                            </span>
                          )}
                        </span>
                      )
                    })}
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* Rules Affecting This Agent */}
          {affectingRules.length > 0 && (
            <div>
              <div style={{ fontSize: '10px', fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '6px' }}>
                Rules Affecting This Agent ({affectingRules.length})
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '3px' }}>
                {affectingRules.map((r, i) => (
                  <div key={i} style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '12px' }}>
                    <span style={{ fontFamily: MONO, color: 'var(--text)' }}>{r.tool}</span>
                    <span style={{
                      padding: '1px 6px', borderRadius: '3px', fontSize: '10px', fontWeight: 600,
                      background: `color-mix(in srgb, ${actionColor(r.action)} 15%, transparent)`,
                      color: actionColor(r.action),
                    }}>
                      {r.action.replace('require_', '')}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Tool access popup */}
          {toolPopup && (
            <ToolAccessPopup
              toolName={toolPopup.name}
              agentId={agent.id}
              agent={agent}
              rules={rules}
              position={toolPopup.pos}
              onClose={() => setToolPopup(null)}
              onUpdate={onUpdate}
            />
          )}
        </div>
      )}
    </div>
  )
}

function ToolAccessPopup({ toolName, agentId, agent, rules, position, onClose, onUpdate }: {
  toolName: string; agentId: string; agent: AgentConfig; rules: RuleEntry[]
  position: { x: number; y: number }; onClose: () => void; onUpdate: () => void
}) {
  const status = toolStatus(toolName, agentId, agent, rules)
  const options: Array<{ icon: string; label: string; desc: string; color: string; value: string }> = [
    { icon: '●', label: 'Allowed', desc: 'Visible and callable', color: 'var(--green)', value: 'allowed' },
    { icon: '✕', label: 'Blocked', desc: 'Hard deny — fenced off', color: 'var(--red)', value: 'blocked' },
    { icon: '◌', label: 'Hidden', desc: 'Not listed, but callable', color: 'var(--muted)', value: 'hidden' },
  ]

  const handleSelect = async (value: string) => {
    try {
      const blocked = new Set(agent.blockedTools || [])
      const hidden = new Set(agent.hiddenTools || [])
      blocked.delete(toolName); hidden.delete(toolName)
      if (value === 'blocked') blocked.add(toolName)
      if (value === 'hidden') hidden.add(toolName)
      const { updateAgentTools } = await import('@/lib/api')
      await updateAgentTools(agentId, {
        blockedTools: Array.from(blocked),
        hiddenTools: Array.from(hidden),
      })
    } catch (e) { console.error(e) }
    onClose()
    onUpdate()
  }

  return (
    <>
      <div onClick={onClose} style={{ position: 'fixed', inset: 0, zIndex: 999 }} />
      <div style={{
        position: 'fixed',
        left: Math.min(position.x, window.innerWidth - 300),
        top: Math.min(position.y, window.innerHeight - 200),
        zIndex: 1000, width: '280px',
        background: 'var(--surface)', border: '1px solid var(--border)',
        borderRadius: 'var(--radius)', boxShadow: '0 8px 30px rgba(0,0,0,0.12)',
        overflow: 'hidden',
      }}>
        <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--border)', fontSize: '13px', color: 'var(--text)' }}>
          Set access for <strong style={{ fontFamily: "'JetBrains Mono', monospace" }}>{toolName}</strong>:
        </div>
        {options.map(opt => {
          const isActive = status.label === opt.value
          return (
            <div key={opt.value}
              onClick={() => handleSelect(opt.value)}
              style={{
                display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                padding: '8px 14px', cursor: 'pointer',
                background: isActive ? `color-mix(in srgb, ${opt.color} 8%, transparent)` : 'transparent',
                borderBottom: '1px solid var(--border)',
              }}
              onMouseEnter={e => { if (!isActive) e.currentTarget.style.background = 'var(--surface-hover)' }}
              onMouseLeave={e => { if (!isActive) e.currentTarget.style.background = 'transparent' }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <span style={{ color: opt.color, fontSize: '14px', fontWeight: 600 }}>{opt.icon}</span>
                <span style={{ fontSize: '13px', fontWeight: 600, color: opt.color }}>{opt.label}</span>
              </div>
              <span style={{ fontSize: '12px', color: 'var(--muted)' }}>{opt.desc}</span>
            </div>
          )
        })}
      </div>
    </>
  )
}
