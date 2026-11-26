import { useState, useCallback, useEffect } from 'react'
import { useGatewayStore } from '@/stores/gateway-store'
import { reconnectSSE } from '@/hooks/use-sse'
import { ClipboardList, Clock, EyeOff, TriangleAlert, Info } from 'lucide-react'
import type { Server, ToolProfile, ToolAnnotation, ConnectionEvent } from '@/lib/types'
import {
  addServer,
  removeServer,
  fetchServerConfig,
  updateServer,
  fetchBaselines,
  fetchAgentConfigs,
  fetchRulesConfig,
  createRule,
  type ServerConfig,
  type AgentConfig,
  type RuleEntry,
  type BaselineEntry,
} from '@/lib/api'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

function statusColor(status: string): string {
  if (status === 'connected') return 'var(--green)'
  if (status === 'reconnecting' || status === 'idle-closed') return 'var(--amber)'
  return 'var(--red)'
}

function statusLabel(status: string): string {
  return status
}

function riskColor(risk: string): string {
  switch (risk?.toUpperCase()) {
    case 'CRITICAL': return 'var(--red)'
    case 'HIGH': return 'var(--amber)'
    case 'MEDIUM': return 'var(--accent)'
    default: return 'var(--muted)'
  }
}

// Map classification to display label (matching vanilla)
const classLabels: Record<string, string> = {
  READ_ONLY: 'READ', WRITE: 'WRITE', DESTRUCTIVE: 'DELETE',
  EXTERNAL_COMM: 'EXTERNAL', CODE_EXECUTION: 'EXEC', DATA_TRANSFER: 'TRANSFER',
}

// Map classification to CSS class color
function classificationColor(cls: string): string {
  switch (cls) {
    case 'DESTRUCTIVE': case 'CODE_EXECUTION': return 'var(--red)'
    case 'WRITE': case 'DATA_TRANSFER': return 'var(--amber)'
    case 'EXTERNAL_COMM': return 'var(--purple)'
    default: return 'var(--muted)'
  }
}

// Check if a tool is governed for all agents with server access
function toolIsGoverned(
  toolName: string, toolServer: string,
  agents: AgentConfig[], rules: RuleEntry[],
): boolean {
  const pseudoSet = new Set(['system', 'unauthorized', 'anonymous'])
  const relevant = agents.filter(a => {
    if (pseudoSet.has(a.id)) return false
    if ((!a.allowedServers || a.allowedServers.length === 0) && !a.serverAccessRestricted) return true
    if (a.serverAccessRestricted && (!a.allowedServers || a.allowedServers.length === 0)) return false
    return (a.allowedServers || []).includes(toolServer)
  })
  if (relevant.length === 0) return true
  return relevant.every(a => {
    if ((a.blockedTools || []).includes(toolName) || (a.acknowledgedTools || []).includes(toolName)) return true
    return rules.some(r => {
      if (r.tool === '*') return false
      let matches = r.tool === toolName
      if (!matches && r.tool?.includes('*')) {
        try { matches = new RegExp('^' + r.tool.replace(/\*/g, '.*') + '$').test(toolName) } catch { /* ignore */ }
      }
      if (!matches) return false
      const ra = r.agents || []
      return ra.length === 0 || ra.includes(a.id)
    })
  })
}

interface AddServerFormState {
  name: string
  transport: 'http' | 'stdio'
  url: string
  command: string
  args: string
  envVars: string
  authType: string
  authToken: string
  authUser: string
  authPass: string
  authHeaders: string
  oauthTokenUrl: string
  oauthClientId: string
  oauthClientSecret: string
  oauthScopes: string
  serverType: string
  mtlsCert: string
  mtlsKey: string
  mtlsCa: string
}

const emptyForm: AddServerFormState = {
  name: '', transport: 'http', url: '', command: '', args: '', envVars: '',
  serverType: '', mtlsCert: '', mtlsKey: '', mtlsCa: '',
  authType: 'none', authToken: '', authUser: '', authPass: '', authHeaders: '',
  oauthTokenUrl: '', oauthClientId: '', oauthClientSecret: '', oauthScopes: '',
}

export function ServersList() {
  const servers = useGatewayStore(s => s.servers)
  const tools = useGatewayStore(s => s.tools)
  const toolAnnotations = useGatewayStore(s => s.toolAnnotations)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [showForm, setShowForm] = useState(false)
  const [editingServer, setEditingServer] = useState<string | null>(null)
  const [form, setForm] = useState<AddServerFormState>(emptyForm)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [baselines, setBaselines] = useState<BaselineEntry[]>([])
  const [agents, setAgents] = useState<AgentConfig[]>([])
  const [rules, setRules] = useState<RuleEntry[]>([])

  useEffect(() => {
    fetchBaselines().then(d => setBaselines(d.baselines || [])).catch(() => {})
    fetchAgentConfigs().then(d => setAgents(d.agents || [])).catch(() => {})
    fetchRulesConfig().then(d => setRules(d.rules || [])).catch(() => {})
  }, [])

  const toggleExpand = useCallback((name: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }, [])

  const toolsByServer = (serverName: string): ToolProfile[] =>
    tools.filter(t => t.server === serverName)

  const toolsByRisk = (serverTools: ToolProfile[]) => {
    const groups: Record<string, ToolProfile[]> = {}
    for (const t of serverTools) {
      const risk = t.riskLevel || 'LOW'
      if (!groups[risk]) groups[risk] = []
      groups[risk].push(t)
    }
    return groups
  }

  const serverBaselines = (serverName: string) => {
    return baselines.filter(b => {
      return tools.some(t => t.name === b.tool && t.server === serverName)
    })
  }

  const resetForm = () => { setForm(emptyForm); setShowForm(false); setEditingServer(null); setError(null) }

  const handleEdit = async (name: string) => {
    try {
      setLoading(true)
      const cfg = await fetchServerConfig(name)
      setForm({
        name: cfg.name,
        transport: (cfg.transport as 'http' | 'stdio') || 'http',
        url: cfg.url || '',
        command: cfg.command || '',
        args: cfg.args?.join(' ') || '',
        envVars: cfg.env ? Object.entries(cfg.env).map(([k, v]) => `${k}=${v}`).join('\n') : '',
        authType: cfg.auth?.type || 'none',
        authToken: cfg.auth?.token || '',
        authUser: cfg.auth?.username || '',
        authPass: cfg.auth?.password || '',
        authHeaders: cfg.auth?.headers ? Object.entries(cfg.auth.headers).map(([k, v]) => `${k}: ${v}`).join('\n') : '',
        oauthTokenUrl: cfg.auth?.tokenUrl || '',
        oauthClientId: cfg.auth?.clientId || '',
        oauthClientSecret: cfg.auth?.clientSecret || '',
        oauthScopes: cfg.auth?.scopes?.join(', ') || '',
        serverType: cfg.serverType || '',
        mtlsCert: cfg.auth?.clientCert || '',
        mtlsKey: cfg.auth?.clientKey || '',
        mtlsCa: cfg.auth?.caCert || '',
      })
      setEditingServer(name)
      setShowForm(true)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  const handleRemove = async (name: string) => {
    if (!confirm(`Remove server "${name}"? This will disconnect and remove all its tools.`)) return
    try {
      await removeServer(name)
      // Reconnect SSE to refresh server list
      setTimeout(() => reconnectSSE(), 500)
    } catch (e) {
      setError(String(e))
    }
  }

  const buildConfig = (): ServerConfig => {
    const cfg: ServerConfig & { server_type?: string } = { name: form.name }
    if (form.serverType) cfg.server_type = form.serverType
    if (form.transport === 'http') {
      cfg.transport = 'http'
      cfg.url = form.url
    } else {
      cfg.transport = 'stdio'
      cfg.command = form.command
      if (form.args.trim()) cfg.args = form.args.trim().split(/\s+/)
      if (form.envVars.trim()) {
        cfg.env = {}
        for (const line of form.envVars.split('\n')) {
          const eq = line.indexOf('=')
          if (eq > 0) cfg.env[line.slice(0, eq)] = line.slice(eq + 1)
        }
      }
    }
    if (form.authType !== 'none') {
      cfg.auth = { type: form.authType }
      if (form.authType === 'bearer') cfg.auth.token = form.authToken
      if (form.authType === 'basic') { cfg.auth.username = form.authUser; cfg.auth.password = form.authPass }
      if (form.authType === 'headers' && form.authHeaders.trim()) {
        cfg.auth.headers = {}
        for (const line of form.authHeaders.split('\n')) {
          const colon = line.indexOf(':')
          if (colon > 0) cfg.auth.headers[line.slice(0, colon).trim()] = line.slice(colon + 1).trim()
        }
      }
      if (form.authType === 'oauth_client_credentials') {
        cfg.auth.tokenUrl = form.oauthTokenUrl
        cfg.auth.clientId = form.oauthClientId
        cfg.auth.clientSecret = form.oauthClientSecret
        if (form.oauthScopes.trim()) cfg.auth.scopes = form.oauthScopes.split(',').map(s => s.trim())
      }
      if (form.authType === 'mtls') {
        cfg.auth.clientCert = form.mtlsCert
        cfg.auth.clientKey = form.mtlsKey
        if (form.mtlsCa.trim()) cfg.auth.caCert = form.mtlsCa
      }
    }
    return cfg
  }

  const handleSubmit = async () => {
    try {
      setLoading(true)
      setError(null)
      const cfg = buildConfig()
      if (editingServer) {
        await updateServer(editingServer, cfg)
      } else {
        await addServer(cfg)
      }
      resetForm()
      // Reconnect SSE to refresh server list with new state
      setTimeout(() => reconnectSSE(), 500)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
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

  const btnStyle = (variant: 'primary' | 'ghost' | 'danger'): React.CSSProperties => ({
    padding: '6px 14px', borderRadius: 'var(--radius)', fontSize: '13px', fontWeight: 500,
    cursor: 'pointer', border: 'none', transition: 'opacity 0.15s',
    background: variant === 'primary' ? 'var(--accent)'
      : variant === 'danger' ? 'color-mix(in srgb, var(--red) 15%, transparent)'
      : 'transparent',
    color: variant === 'primary' ? '#fff'
      : variant === 'danger' ? 'var(--red)'
      : 'var(--muted)',
  })

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0' }}>
      {/* Header */}
      <div style={{
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        padding: '12px 20px', borderBottom: '1px solid var(--border)',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span style={{ fontSize: '14px' }}>⬡</span>
          <span style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text)' }}>Servers & Tools</span>
        </div>
        <button onClick={() => { resetForm(); setShowForm(true) }} style={btnStyle('primary')}>
          + Add
        </button>
      </div>

      {/* Add/Edit form */}
      {showForm && (
        <div style={{
          padding: '16px 20px', borderBottom: '1px solid var(--border)',
        }}>
          {/* Row 1: Name, Transport, Server Type */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '12px', marginBottom: '12px' }}>
            <div>
              <label style={labelStyle}>Server Name</label>
              <input style={{ ...inputStyle, ...(editingServer ? { opacity: 0.5 } : {}) }}
                value={form.name} readOnly={!!editingServer}
                onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
                placeholder="e.g., github-mcp"
              />
            </div>
            <div>
              <label style={labelStyle}>Transport</label>
              <select style={inputStyle} value={form.transport} disabled={!!editingServer}
                onChange={e => setForm(f => ({ ...f, transport: e.target.value as 'http' | 'stdio' }))}>
                <option value="http">http (URL)</option>
                <option value="stdio">stdio (command)</option>
              </select>
            </div>
            <div>
              <label style={labelStyle}>Server Type</label>
              <select style={inputStyle} value={form.serverType}
                onChange={e => setForm(f => ({ ...f, serverType: e.target.value }))}>
                <option value="">Auto-detect</option>
                <option value="filesystem">Filesystem</option>
                <option value="github">GitHub</option>
                <option value="database">Database</option>
                <option value="slack">Slack</option>
                <option value="web-search">Web Search</option>
                <option value="generic">Generic</option>
              </select>
            </div>
          </div>

          {/* Transport-specific fields */}
          {form.transport === 'http' ? (
            <div style={{ marginBottom: '12px' }}>
              <label style={labelStyle}>URL</label>
              <input style={inputStyle} value={form.url} placeholder="https://mcp.example.com/sse"
                onChange={e => setForm(f => ({ ...f, url: e.target.value }))}
              />
            </div>
          ) : (
            <>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px', marginBottom: '12px' }}>
                <div>
                  <label style={labelStyle}>Command</label>
                  <input style={inputStyle} value={form.command} placeholder="e.g., npx"
                    onChange={e => setForm(f => ({ ...f, command: e.target.value }))}
                  />
                </div>
                <div>
                  <label style={labelStyle}>Args</label>
                  <input style={inputStyle} value={form.args} placeholder="e.g., -y @modelcontextprotocol/server-github"
                    onChange={e => setForm(f => ({ ...f, args: e.target.value }))}
                  />
                </div>
              </div>
              <div style={{ marginBottom: '12px' }}>
                <label style={labelStyle}>Env Vars <span style={{ fontWeight: 400, textTransform: 'none' }}>(optional, KEY=VALUE per line)</span></label>
                <input style={inputStyle} value={form.envVars}
                  placeholder="GITHUB_TOKEN=ghp_xxx"
                  onChange={e => setForm(f => ({ ...f, envVars: e.target.value }))}
                />
              </div>
            </>
          )}

          {/* Auth Type */}
          <div style={{ marginBottom: '12px', maxWidth: '340px' }}>
            <label style={labelStyle}>Auth Type</label>
            <select style={inputStyle} value={form.authType}
              onChange={e => setForm(f => ({ ...f, authType: e.target.value }))}>
              <option value="none">None</option>
              <option value="bearer">Bearer Token</option>
              <option value="basic">Basic Auth</option>
              <option value="headers">Custom Headers</option>
              <option value="oauth_client_credentials">OAuth Client Credentials</option>
              <option value="mtls">mTLS (Client Certificate)</option>
            </select>
          </div>

          {/* Auth-specific fields */}
          {form.authType === 'bearer' && (
            <div style={{ marginBottom: '12px' }}>
              <label style={labelStyle}>Token</label>
              <input style={inputStyle} type="password" value={form.authToken} placeholder="${API_TOKEN}"
                onChange={e => setForm(f => ({ ...f, authToken: e.target.value }))}
              />
            </div>
          )}
          {form.authType === 'basic' && (
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px', marginBottom: '12px' }}>
              <div>
                <label style={labelStyle}>Username</label>
                <input style={inputStyle} value={form.authUser}
                  onChange={e => setForm(f => ({ ...f, authUser: e.target.value }))} />
              </div>
              <div>
                <label style={labelStyle}>Password</label>
                <input style={inputStyle} type="password" value={form.authPass}
                  onChange={e => setForm(f => ({ ...f, authPass: e.target.value }))} />
              </div>
            </div>
          )}
          {form.authType === 'oauth_client_credentials' && (
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px', marginBottom: '12px' }}>
              <div>
                <label style={labelStyle}>Token URL</label>
                <input style={inputStyle} value={form.oauthTokenUrl}
                  onChange={e => setForm(f => ({ ...f, oauthTokenUrl: e.target.value }))} />
              </div>
              <div>
                <label style={labelStyle}>Client ID</label>
                <input style={inputStyle} value={form.oauthClientId}
                  onChange={e => setForm(f => ({ ...f, oauthClientId: e.target.value }))} />
              </div>
              <div>
                <label style={labelStyle}>Client Secret</label>
                <input style={inputStyle} type="password" value={form.oauthClientSecret}
                  onChange={e => setForm(f => ({ ...f, oauthClientSecret: e.target.value }))} />
              </div>
              <div>
                <label style={labelStyle}>Scopes</label>
                <input style={inputStyle} value={form.oauthScopes} placeholder="read, write"
                  onChange={e => setForm(f => ({ ...f, oauthScopes: e.target.value }))} />
              </div>
            </div>
          )}
          {form.authType === 'headers' && (
            <div style={{ marginBottom: '12px' }}>
              <label style={labelStyle}>Headers (one per line, Key: Value)</label>
              <textarea style={{ ...inputStyle, minHeight: '60px', resize: 'vertical' }} value={form.authHeaders}
                onChange={e => setForm(f => ({ ...f, authHeaders: e.target.value }))}
                placeholder="X-API-Key: my-key" />
            </div>
          )}
          {form.authType === 'mtls' && (
            <>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px', marginBottom: '12px' }}>
                <div>
                  <label style={labelStyle}>Client Cert Path</label>
                  <input style={inputStyle} value={form.mtlsCert} placeholder="/etc/certs/client.pem"
                    onChange={e => setForm(f => ({ ...f, mtlsCert: e.target.value }))} />
                </div>
                <div>
                  <label style={labelStyle}>Client Key Path</label>
                  <input style={inputStyle} value={form.mtlsKey} placeholder="/etc/certs/client-key.pem"
                    onChange={e => setForm(f => ({ ...f, mtlsKey: e.target.value }))} />
                </div>
              </div>
              <div style={{ marginBottom: '12px' }}>
                <label style={labelStyle}>CA Cert Path <span style={{ fontWeight: 400, textTransform: 'none' }}>(optional, for self-signed servers)</span></label>
                <input style={inputStyle} value={form.mtlsCa} placeholder="/etc/certs/ca.pem"
                  onChange={e => setForm(f => ({ ...f, mtlsCa: e.target.value }))} />
              </div>
            </>
          )}

          {error && (
            <div style={{ fontSize: '12px', color: 'var(--red)', marginBottom: '8px' }}>{error}</div>
          )}

          <div style={{ display: 'flex', gap: '8px' }}>
            <button onClick={handleSubmit} disabled={loading} style={btnStyle('primary')}>
              {editingServer ? 'Save & Reconnect' : 'Connect & Save'}
            </button>
            <button onClick={resetForm} style={{
              padding: '6px 14px', borderRadius: 'var(--radius)', fontSize: '13px', fontWeight: 500,
              cursor: 'pointer', border: '1px solid var(--border)', background: 'transparent',
              color: 'var(--text)',
            }}>Cancel</button>
          </div>
        </div>
      )}

      {/* Empty state */}
      {servers.length === 0 && !showForm && (
        <div style={{
          padding: '40px 20px', textAlign: 'center', color: 'var(--muted)', fontSize: '13px',
        }}>
          No MCP servers connected.
          <button onClick={() => setShowForm(true)} style={{
            ...btnStyle('primary'), marginLeft: '8px', fontSize: '12px', padding: '4px 10px',
          }}>
            + Add server
          </button>
        </div>
      )}

      {/* Server cards */}
      {servers.map(server => (
        <ServerCard
          key={server.name}
          server={server}
          tools={toolsByServer(server.name)}
          toolsByRisk={toolsByRisk(toolsByServer(server.name))}
          annotations={toolAnnotations}
          baselines={serverBaselines(server.name)}
          agents={agents}
          rules={rules}
          isExpanded={expanded.has(server.name)}
          onToggle={() => toggleExpand(server.name)}
          onEdit={() => handleEdit(server.name)}
          onRemove={() => handleRemove(server.name)}
          onBlockAll={async () => {
            if (!confirm(`Block all tools on "${server.name}"?`)) return
            const serverTools = toolsByServer(server.name)
            for (const t of serverTools) {
              try { await createRule({ tool: t.name, action: 'block', server: server.name } as Parameters<typeof createRule>[0]) } catch { /* ignore */ }
            }
          }}
        />
      ))}
    </div>
  )
}

function eventDot(evt: ConnectionEvent): { symbol: string; color: string } {
  switch (evt.type) {
    case 'connected': case 'reconnected': return { symbol: '●', color: 'var(--green)' }
    case 'disconnected': case 'auth_failed': return { symbol: '○', color: 'var(--red)' }
    case 'reconnecting': return { symbol: '◌', color: 'var(--amber)' }
    case 'circuit_opened': return { symbol: '◉', color: 'var(--red)' }
    case 'circuit_closed': return { symbol: '●', color: 'var(--green)' }
    case 'idle_closed': return { symbol: '·', color: 'var(--muted)' }
    default: return { symbol: '·', color: 'var(--muted)' }
  }
}

function timeAgo(ts: string): string {
  const diff = Date.now() - new Date(ts).getTime()
  const secs = Math.floor(diff / 1000)
  if (secs < 60) return `${secs}s ago`
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

function ServerCard({ server, tools, toolsByRisk, annotations, baselines, agents, rules, isExpanded, onToggle, onEdit, onRemove, onBlockAll }: {
  server: Server
  tools: ToolProfile[]
  toolsByRisk: Record<string, ToolProfile[]>
  annotations: Record<string, ToolAnnotation>
  baselines: BaselineEntry[]
  agents: AgentConfig[]
  rules: RuleEntry[]
  isExpanded: boolean
  onToggle: () => void
  onEdit: () => void
  onRemove: () => void
  onBlockAll: () => void
}) {
  const [hovered, setHovered] = useState(false)
  const [popupTool, setPopupTool] = useState<string | null>(null)
  const [popupPos, setPopupPos] = useState<{ x: number; y: number }>({ x: 0, y: 0 })
  const riskOrder = ['CRITICAL', 'HIGH', 'MEDIUM', 'LOW']

  // Baseline badge
  const learned = baselines.filter(b => b.baseline?.baselineSource === 'learned').length
  const totalBl = baselines.length
  const blLabel = totalBl > 0
    ? (learned === totalBl ? `learned (${totalBl})` : learned > 0 ? `${learned}/${totalBl} learned` : `default (${totalBl})`)
    : null

  const sColor = statusColor(server.status)
  const sLabel = statusLabel(server.status)

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
      {/* Header row */}
      <div
        onClick={onToggle}
        style={{
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
          padding: '10px 20px', cursor: 'pointer', gap: '8px',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', minWidth: 0, flexWrap: 'wrap' }}>
          {/* Status badge */}
          <span style={{
            fontSize: '12px', padding: '2px 7px', borderRadius: '3px',
            background: `color-mix(in srgb, ${sColor} 12%, transparent)`, color: sColor,
            fontWeight: 500,
          }}>
            {sLabel}
          </span>
          {/* Name */}
          <span style={{
            fontSize: '13px', fontWeight: 400, color: 'var(--text)', fontFamily: MONO,
            overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
          }}>
            {server.name}
          </span>
          {/* Era badge */}
          {server.server_era && (
            <span style={{
              fontSize: '12px', padding: '2px 7px', borderRadius: '3px',
              background: server.server_era === 'modern' ? 'rgba(34,197,94,0.1)' : 'rgba(245,158,11,0.1)',
              color: server.server_era === 'modern' ? 'var(--green)' : 'var(--amber)',
            }}>
              {server.server_era}
            </span>
          )}
          {/* Auth type badge */}
          {server.auth_type && (
            <span style={{
              fontSize: '12px', padding: '2px 7px', borderRadius: '3px',
              background: 'rgba(139,92,246,0.08)', color: 'var(--purple)',
            }}>
              {server.auth_type === 'oauth_client_credentials' ? 'oauth' : server.auth_type}
            </span>
          )}
          {/* Baseline badge */}
          {blLabel && (
            <span style={{
              fontSize: '12px', padding: '2px 7px', borderRadius: '3px',
              background: 'rgba(139,92,246,0.08)', color: 'var(--purple)',
            }}>
              {blLabel}
            </span>
          )}
          {/* Health metrics */}
          {server.meanLatencyMs != null && server.meanLatencyMs > 0 && (
            <span style={{ fontSize: '12px', color: 'var(--muted)' }}>
              {Math.round(server.meanLatencyMs)}ms avg
              {server.errorRate != null && server.errorRate > 0.01 && ` · ${(server.errorRate * 100).toFixed(0)}% err`}
            </span>
          )}
          {/* Circuit state */}
          {server.circuit_state && server.circuit_state !== 'closed' && (
            <span style={{
              fontSize: '12px',
              color: server.circuit_state === 'open' ? 'var(--red)' : 'var(--amber)',
            }}>
              circuit {server.circuit_state.replace('_', '-')}
            </span>
          )}
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', flexShrink: 0 }}>
          {/* Action buttons shown on hover */}
          {hovered && (
            <>
              <button onClick={e => { e.stopPropagation(); onBlockAll() }} style={{
                padding: '3px 10px', borderRadius: 'var(--radius)', fontSize: '11px',
                background: 'color-mix(in srgb, var(--red) 10%, transparent)',
                color: 'var(--red)', border: '1px solid color-mix(in srgb, var(--red) 30%, transparent)',
                cursor: 'pointer', fontWeight: 500,
              }}>
                block all
              </button>
              <button onClick={e => { e.stopPropagation(); onEdit() }} style={{
                padding: '3px 10px', borderRadius: 'var(--radius)', fontSize: '11px',
                background: 'transparent', color: 'var(--muted)', border: '1px solid var(--border)',
                cursor: 'pointer', fontWeight: 500,
              }}>
                edit
              </button>
              <button onClick={e => { e.stopPropagation(); onRemove() }} style={{
                padding: '3px 10px', borderRadius: 'var(--radius)', fontSize: '11px',
                background: 'color-mix(in srgb, var(--red) 10%, transparent)',
                color: 'var(--red)', border: '1px solid color-mix(in srgb, var(--red) 30%, transparent)',
                cursor: 'pointer', fontWeight: 500,
              }}>
                remove
              </button>
            </>
          )}
          {/* Tool count + expand */}
          <span style={{ fontSize: '12px', color: 'var(--muted)', fontFamily: MONO }}>
            {server.tool_count} {server.tool_count === 1 ? 'tool' : 'tools'}
          </span>
          <span style={{
            fontSize: '10px', color: 'var(--muted)',
            transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
            transition: 'transform 0.15s', display: 'inline-block',
          }}>
            ▼
          </span>
        </div>
      </div>

      {/* Expanded content */}
      {isExpanded && (
        <div style={{ padding: '0 20px 14px 20px' }}>
          {/* Tools grouped by risk */}
          {riskOrder.map(risk => {
            const riskTools = toolsByRisk[risk]
            if (!riskTools?.length) return null
            return (
              <div key={risk} style={{ marginBottom: '10px' }}>
                <div style={{
                  fontSize: '11px', fontWeight: 700, color: riskColor(risk),
                  textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '4px',
                }}>
                  {risk} ({riskTools.length})
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px' }}>
                  {riskTools.map(tool => {
                    const ann = annotations[tool.name]
                    const isHighRisk = risk === 'CRITICAL' || risk === 'HIGH'
                    const governed = isHighRisk ? toolIsGoverned(tool.name, tool.server, agents, rules) : null
                    const clsLabel = classLabels[tool.classification] || tool.classification || 'READ'
                    const clsColor = classificationColor(tool.classification)
                    return (
                      <span key={tool.name}
                        onClick={e => {
                          e.stopPropagation()
                          const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
                          setPopupPos({ x: rect.left, y: rect.bottom + 4 })
                          setPopupTool(popupTool === tool.name ? null : tool.name)
                        }}
                        style={{
                          display: 'inline-flex', alignItems: 'center', gap: '4px',
                          padding: '4px 8px', borderRadius: '4px', fontSize: '12px',
                          background: popupTool === tool.name ? 'color-mix(in srgb, var(--accent) 10%, var(--bg))' : 'var(--bg)',
                          border: `1px solid ${popupTool === tool.name ? 'var(--accent)' : 'var(--border)'}`,
                          fontFamily: MONO, color: 'var(--text)', cursor: 'pointer',
                          transition: 'border-color 0.15s',
                        }}>
                        {tool.name}
                        {/* Annotation badges */}
                        {ann?.readOnlyHint && <span style={{ fontSize: '9px', padding: '1px 4px', borderRadius: '2px', background: 'rgba(59,130,246,0.1)', color: 'var(--accent)' }}>RO</span>}
                        {ann?.destructiveHint && <span style={{ fontSize: '9px', padding: '1px 4px', borderRadius: '2px', background: 'rgba(239,68,68,0.1)', color: 'var(--red)' }}>⚠D</span>}
                        {ann?.idempotentHint && <span style={{ fontSize: '9px', padding: '1px 4px', borderRadius: '2px', background: 'rgba(139,92,246,0.1)', color: '#8b5cf6' }}>IK</span>}
                        {ann?.openWorldHint && <span style={{ fontSize: '9px', padding: '1px 4px', borderRadius: '2px', background: 'rgba(245,158,11,0.1)', color: 'var(--amber)' }}>OW</span>}
                        {/* Governance badge for HIGH/CRITICAL */}
                        {governed !== null && (
                          governed
                            ? <span style={{ fontSize: '10px', padding: '1px 5px', borderRadius: '3px', background: 'rgba(34,197,94,0.1)', color: 'var(--green)', fontWeight: 500 }}>✓ governed</span>
                            : <span style={{ fontSize: '10px', padding: '1px 5px', borderRadius: '3px', background: 'rgba(245,158,11,0.1)', color: 'var(--amber)', fontWeight: 500 }}>⚠ ungoverned</span>
                        )}
                        {/* Classification chip */}
                        <span style={{
                          fontSize: '9px', fontWeight: 700, padding: '1px 5px',
                          borderRadius: '2px',
                          background: `color-mix(in srgb, ${clsColor} 15%, transparent)`,
                          color: clsColor,
                        }}>
                          {clsLabel}
                        </span>
                      </span>
                    )
                  })}
                </div>
              </div>
            )
          })}

          {tools.length === 0 && (
            <div style={{ fontSize: '12px', color: 'var(--muted)', padding: '4px 0' }}>No tools discovered</div>
          )}

          {/* Tool popup */}
          {popupTool && (
            <ToolPopup
              toolName={popupTool}
              tool={tools.find(t => t.name === popupTool)}
              agents={agents}
              rules={rules}
              position={popupPos}
              onClose={() => setPopupTool(null)}
              onNavigateRules={(action) => {
                const t = tools.find(t => t.name === popupTool)
                sessionStorage.setItem('prefillRule', JSON.stringify({
                  tool: popupTool, server: t?.server || server.name, action,
                }))
                setPopupTool(null)
                // Dispatch a custom event so ControlTab switches to Rules sub-tab
                window.dispatchEvent(new CustomEvent('switchToRules'))
              }}
            />
          )}

          {/* Connection events timeline */}
          {server.events && server.events.length > 0 && (
            <div style={{ marginTop: '10px', borderTop: '1px solid var(--border)', paddingTop: '8px' }}>
              <div style={{
                display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                marginBottom: '6px',
              }}>
                <span style={{
                  fontSize: '10px', fontWeight: 600, color: 'var(--muted)',
                  textTransform: 'uppercase', letterSpacing: '0.05em',
                }}>
                  Connection History
                </span>
                <button
                  onClick={e => { e.stopPropagation(); window.location.hash = `discover` }}
                  style={{
                    fontSize: '11px', color: 'var(--muted)', background: 'transparent',
                    border: '1px solid var(--border)', borderRadius: 'var(--radius)',
                    padding: '2px 8px', cursor: 'pointer',
                  }}
                >
                  View full history →
                </button>
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '3px' }}>
                {server.events.slice(-10).reverse().map((evt, i) => {
                  const dot = eventDot(evt)
                  return (
                    <div key={i} style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '11px' }}>
                      <span style={{ color: dot.color, fontFamily: MONO, fontSize: '12px', width: '14px', textAlign: 'center' }}>
                        {dot.symbol}
                      </span>
                      <span style={{ color: 'var(--muted)', fontFamily: MONO, fontSize: '11px', minWidth: '50px' }}>
                        {timeAgo(evt.timestamp)}
                      </span>
                      <span style={{ color: dot.color, fontSize: '12px', fontWeight: 400 }}>
                        {evt.type.replace(/_/g, ' ')}
                      </span>
                      {evt.details && (
                        <span style={{ color: 'var(--muted)', fontSize: '11px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          {evt.details}
                        </span>
                      )}
                    </div>
                  )
                })}
              </div>
              {server.reconnect_count > 0 && (
                <div style={{ fontSize: '10px', color: 'var(--muted)', marginTop: '4px' }}>
                  {server.reconnect_count} reconnect{server.reconnect_count !== 1 ? 's' : ''} total
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function ToolPopup({ toolName, tool, agents, rules, position, onClose, onNavigateRules }: {
  toolName: string
  tool?: ToolProfile
  agents: AgentConfig[]
  rules: RuleEntry[]
  position: { x: number; y: number }
  onClose: () => void
  onNavigateRules: (action: string) => void
}) {
  const pseudoSet = new Set(['system', 'unauthorized', 'anonymous'])
  const risk = tool?.riskLevel || 'LOW'
  const cls = tool?.classification || 'READ_ONLY'
  const clsLabel = classLabels[cls] || cls
  const governed = tool ? toolIsGoverned(toolName, tool.server, agents, rules) : false

  const relevantAgents = agents.filter(a => {
    if (pseudoSet.has(a.id)) return false
    if ((!a.allowedServers || a.allowedServers.length === 0) && !a.serverAccessRestricted) return true
    if (a.serverAccessRestricted && (!a.allowedServers || a.allowedServers.length === 0)) return false
    return tool ? (a.allowedServers || []).includes(tool.server) : false
  })

  const coveredAgents = relevantAgents.filter(a => {
    if ((a.blockedTools || []).includes(toolName) || (a.acknowledgedTools || []).includes(toolName)) return true
    return rules.some(r => {
      if (r.tool === '*') return false
      let m = r.tool === toolName
      if (!m && r.tool?.includes('*')) {
        try { m = new RegExp('^' + r.tool.replace(/\*/g, '.*') + '$').test(toolName) } catch { /* ignore */ }
      }
      if (!m) return false
      const ra = r.agents || []
      return ra.length === 0 || ra.includes(a.id)
    })
  })

  const coverageDetail = (() => {
    if (coveredAgents.length === relevantAgents.length && relevantAgents.length > 0) {
      const matchingRule = rules.find(r => {
        if (r.tool === '*') return false
        let m = r.tool === toolName
        if (!m && r.tool?.includes('*')) {
          try { m = new RegExp('^' + r.tool.replace(/\*/g, '.*') + '$').test(toolName) } catch { /* ignore */ }
        }
        return m && (!r.agents || r.agents.length === 0)
      })
      if (matchingRule) return `${relevantAgents.length} agents: all rule: ${matchingRule.action}`
    }
    return `${coveredAgents.length} of ${relevantAgents.length} agents covered`
  })()

  const riskLevels = ['LOW', 'MEDIUM', 'HIGH', 'CRITICAL']

  return (
    <>
      <div onClick={onClose} style={{ position: 'fixed', inset: 0, zIndex: 999 }} />
      <div style={{
        position: 'fixed',
        left: Math.min(position.x, window.innerWidth - 360),
        top: Math.min(position.y, window.innerHeight - 400),
        zIndex: 1000, width: '340px',
        background: 'var(--surface)', border: '1px solid var(--border)',
        borderRadius: 'var(--radius)', boxShadow: '0 8px 30px rgba(0,0,0,0.12)',
      }}>
        {/* Header */}
        <div style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '6px', flexWrap: 'wrap' }}>
            <span style={{ fontSize: '14px', fontWeight: 600, fontFamily: MONO, color: 'var(--text)' }}>{toolName}</span>
            {governed
              ? <span style={{ fontSize: '10px', padding: '1px 6px', borderRadius: '3px', background: 'rgba(34,197,94,0.1)', color: 'var(--green)', fontWeight: 600 }}>✓ governed</span>
              : <span style={{ fontSize: '10px', padding: '1px 6px', borderRadius: '3px', background: 'rgba(245,158,11,0.1)', color: 'var(--amber)', fontWeight: 600 }}>⚠ ungoverned</span>
            }
            <span style={{ fontSize: '10px', padding: '1px 5px', borderRadius: '2px', background: 'var(--bg)', color: 'var(--muted)', fontWeight: 600 }}>{clsLabel}</span>
            <span style={{ fontSize: '10px', padding: '1px 5px', borderRadius: '2px', background: 'var(--bg)', color: riskColor(risk), fontWeight: 600 }}>{risk}</span>
          </div>
        </div>

        {/* Agent coverage */}
        <div style={{ padding: '10px 16px', borderBottom: '1px solid var(--border)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '4px' }}>
            <span style={{ fontSize: '10px', fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Agent Coverage</span>
            <span style={{ fontSize: '11px', fontWeight: 600, color: coveredAgents.length === relevantAgents.length ? 'var(--green)' : 'var(--amber)' }}>
              {coveredAgents.length}/{relevantAgents.length} covered
            </span>
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text)', display: 'flex', alignItems: 'center', gap: '4px' }}>
            <span style={{ color: governed ? 'var(--green)' : 'var(--amber)' }}>{governed ? '✓' : '⚠'}</span>
            {coverageDetail}
          </div>
        </div>

        {/* Actions */}
        <div style={{ padding: '4px 16px', borderBottom: '1px solid var(--border)' }}>
          {([
            { label: 'Add rule...', icon: <ClipboardList size={18} strokeWidth={1.5} />, action: 'block' },
            { label: 'Rate limit...', icon: <Clock size={18} strokeWidth={1.5} />, action: 'rate_limit' },
            { label: 'Hide from agents', icon: <EyeOff size={18} strokeWidth={1.5} />, action: 'hide' },
          ]).map(({ label, icon, action }) => (
            <div key={label} onClick={e => { e.stopPropagation(); onNavigateRules(action) }}
              style={{ display: 'flex', alignItems: 'center', gap: '10px', padding: '8px 0', cursor: 'pointer', fontSize: '13px', color: 'var(--text)' }}
              onMouseEnter={e => (e.currentTarget.style.color = 'var(--accent)')}
              onMouseLeave={e => (e.currentTarget.style.color = 'var(--text)')}>
              <span style={{ width: '20px', display: 'flex', justifyContent: 'center', opacity: 0.5 }}>{icon}</span>
              {label}
            </div>
          ))}
        </div>

        {/* Risk level */}
        <div style={{ padding: '10px 16px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', gap: '10px' }}>
          <TriangleAlert size={16} strokeWidth={1.5} style={{ color: 'var(--muted)', flexShrink: 0 }} />
          {riskLevels.map(level => (
            <span key={level} onClick={async e => {
              e.stopPropagation()
              try {
                const resp = await fetch('/admin/v1/tools/risk-override', {
                  method: 'POST', credentials: 'same-origin',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({ tool: toolName, riskLevel: level }),
                })
                if (resp.ok) {
                  // Update Zustand store immediately so UI re-renders with new risk level
                  const currentTools = useGatewayStore.getState().tools
                  useGatewayStore.setState({
                    tools: currentTools.map(t => t.name === toolName ? { ...t, riskLevel: level } : t),
                  })
                }
                onClose()
              } catch { /* ignore */ }
            }} style={{
              fontSize: '11px', fontWeight: level === risk ? 700 : 400,
              color: level === risk ? riskColor(level) : 'var(--muted)',
              textDecoration: level === risk ? 'underline' : 'none', cursor: 'pointer',
            }}>{level}</span>
          ))}
        </div>

        {/* View details */}
        <div onClick={e => { e.stopPropagation(); onClose() }}
          style={{ padding: '8px 16px', display: 'flex', alignItems: 'center', gap: '6px', fontSize: '12px', color: 'var(--accent)', cursor: 'pointer' }}>
          <Info size={14} strokeWidth={1.5} /> View details...
        </div>
      </div>
    </>
  )
}
