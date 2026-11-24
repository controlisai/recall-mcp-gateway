import { useState, useCallback, useEffect, useMemo } from 'react'
import { useGatewayStore } from '@/stores/gateway-store'
import {
  fetchRulesConfig,
  fetchAgentConfigs,
  createRule,
  deleteRule,
  type RuleEntry,
  type AgentConfig,
} from '@/lib/api'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

function actionColor(action: string): string {
  switch (action) {
    case 'block': return 'var(--red)'
    case 'require_approval': return 'var(--purple)'
    case 'require_consent': return 'var(--purple)'
    case 'rate_limit': return 'var(--amber)'
    case 'hide': return 'var(--muted)'
    default: return 'var(--green)'
  }
}

function actionLabel(action: string): string {
  switch (action) {
    case 'require_approval': return 'approval'
    case 'require_consent': return 'consent'
    case 'rate_limit': return 'rate limit'
    default: return action
  }
}

interface RuleFormState {
  tool: string
  server: string
  agent: string
  action: string
  reason: string
  rateLimit: string
  notify: string
  retryMethod: string
  statusWebhook: string
  consentTitle: string
  consentDescription: string
  consentFields: string
}

const emptyRuleForm: RuleFormState = {
  tool: '', server: '', agent: '', action: 'allow', reason: '',
  rateLimit: '', notify: '', retryMethod: 'poll', statusWebhook: '',
  consentTitle: '', consentDescription: '', consentFields: '',
}

export function RulesEditor() {
  const defaultAction = useGatewayStore(s => s.defaultAction)
  const toolProfiles = useGatewayStore(s => s.tools)
  const [rules, setRules] = useState<RuleEntry[]>([])
  const [agents, setAgents] = useState<AgentConfig[]>([])
  const [showForm, setShowForm] = useState(false)
  const [editingIndex, setEditingIndex] = useState<number | null>(null)
  const [form, setForm] = useState<RuleFormState>(emptyRuleForm)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadData = useCallback(async () => {
    try {
      const [rulesData, agentsData] = await Promise.all([fetchRulesConfig(), fetchAgentConfigs()])
      setRules(rulesData.rules || [])
      setAgents(agentsData.agents || [])
    } catch (e) {
      console.error('Failed to load rules data:', e)
    }
  }, [])

  useEffect(() => { loadData() }, [loadData])

  // Check for prefilled rule from recommendations
  useEffect(() => {
    const prefill = sessionStorage.getItem('prefillRule')
    if (prefill) {
      sessionStorage.removeItem('prefillRule')
      try {
        const data = JSON.parse(prefill)
        setForm(f => ({
          ...f,
          tool: data.tool || '', server: data.server || '',
          action: data.action || 'block', reason: data.reason || '',
        }))
        setShowForm(true)
      } catch { /* ignore */ }
    }
  }, [])

  // Filter out default * rule from table
  const displayRules = useMemo(() =>
    rules.filter(r => r.tool !== '*').map(r => ({ ...r, originalIndex: rules.indexOf(r) })),
    [rules],
  )

  // Unique tools from discovered profiles
  const discoveredTools = useMemo(() => {
    const names = new Set(toolProfiles.map(t => t.name))
    return Array.from(names).sort()
  }, [toolProfiles])

  // Map tool -> server
  const toolServerMap = useMemo(() => {
    const map: Record<string, string> = {}
    for (const t of toolProfiles) map[t.name] = t.server
    return map
  }, [toolProfiles])

  // Get archetype of selected agent
  const selectedAgentArchetype = useMemo(() => {
    if (!form.agent) return null
    const a = agents.find(a => a.id === form.agent)
    return a?.archetype || 'autonomous'
  }, [form.agent, agents])

  // Available actions depend on whether agent is selected and archetype
  const availableActions = useMemo(() => {
    if (!form.agent) {
      return ['allow', 'block', 'rate_limit', 'hide']
    }
    const base = ['allow', 'block', 'rate_limit', 'hide']
    if (selectedAgentArchetype === 'user_embedded') {
      base.push('require_consent')
    } else {
      base.push('require_approval')
    }
    return base
  }, [form.agent, selectedAgentArchetype])

  const onToolChange = (tool: string) => {
    const server = toolServerMap[tool] || ''
    setForm(f => ({ ...f, tool, server }))
  }

  const resetForm = () => {
    setForm(emptyRuleForm)
    setShowForm(false)
    setEditingIndex(null)
    setError(null)
  }

  const handleEdit = (rule: RuleEntry & { originalIndex: number }) => {
    setForm({
      tool: rule.tool, server: rule.server || '', agent: rule.agents?.[0] || '',
      action: rule.action, reason: rule.reason || '', rateLimit: rule.rateLimit ? `${rule.rateLimit}/${rule.ratePeriod || '1m'}` : '',
      notify: rule.notify || '', retryMethod: rule.retryMethod || 'poll',
      statusWebhook: rule.statusWebhook || '',
      consentTitle: rule.consentForm?.title || '', consentDescription: rule.consentForm?.description || '',
      consentFields: rule.consentForm?.fields?.join(', ') || '',
    })
    setEditingIndex(rule.originalIndex)
    setShowForm(true)
    setError(null)
  }

  const handleSubmit = async () => {
    if (!form.tool) return
    try {
      setLoading(true); setError(null)
      const payload: Record<string, unknown> = {
        tool: form.tool,
        action: form.action,
      }
      if (form.server) payload.server = form.server
      if (form.agent) payload.agents = [form.agent]
      if (form.reason) payload.reason = form.reason
      if (form.action === 'rate_limit' && form.rateLimit) payload.rate_limit = form.rateLimit
      if ((form.action === 'require_approval') && form.notify) payload.notify = form.notify
      if (form.action === 'require_approval') {
        payload.retry_method = form.retryMethod
        if (form.statusWebhook) payload.status_webhook = form.statusWebhook
      }
      if (form.action === 'require_consent') {
        const cf: Record<string, unknown> = {}
        if (form.consentTitle) cf.title = form.consentTitle
        if (form.consentDescription) cf.description = form.consentDescription
        if (form.consentFields) cf.fields = form.consentFields.split(',').map(s => s.trim()).filter(Boolean)
        if (Object.keys(cf).length > 0) payload.consent_form = cf
      }
      // If editing, delete old rule first
      if (editingIndex !== null) {
        await deleteRule(editingIndex)
      }
      await createRule(payload as Parameters<typeof createRule>[0])
      resetForm()
      await loadData()
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  const handleDelete = async (index: number) => {
    if (!confirm('Delete this rule?')) return
    try {
      await deleteRule(index)
      await loadData()
    } catch (e) {
      setError(String(e))
    }
  }

  const handleDefaultToggle = async (action: 'allow' | 'block') => {
    try {
      // Find and delete existing * rule, then create new one
      const starIdx = rules.findIndex(r => r.tool === '*')
      if (starIdx >= 0) await deleteRule(starIdx)
      await createRule({ tool: '*', action } as Parameters<typeof createRule>[0])
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
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'auto' }}>
      {/* Header */}
      <div style={{
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        padding: '12px 20px', borderBottom: '1px solid var(--border)',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
          <span style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text)' }}>Active Rules</span>
          {/* Default action toggle */}
          <div style={{ display: 'flex', gap: '0', borderRadius: 'var(--radius)', overflow: 'hidden', border: '1px solid var(--border)' }}>
            <button onClick={() => handleDefaultToggle('allow')} style={{
              padding: '3px 12px', fontSize: '11px', fontWeight: 600, cursor: 'pointer', border: 'none',
              background: defaultAction === 'allow' ? 'color-mix(in srgb, var(--green) 20%, transparent)' : 'transparent',
              color: defaultAction === 'allow' ? 'var(--green)' : 'var(--muted)',
            }}>
              Allow
            </button>
            <button onClick={() => handleDefaultToggle('block')} style={{
              padding: '3px 12px', fontSize: '11px', fontWeight: 600, cursor: 'pointer', border: 'none',
              borderLeft: '1px solid var(--border)',
              background: defaultAction === 'block' ? 'color-mix(in srgb, var(--red) 20%, transparent)' : 'transparent',
              color: defaultAction === 'block' ? 'var(--red)' : 'var(--muted)',
            }}>
              Block
            </button>
          </div>
          <span style={{ fontSize: '11px', color: 'var(--muted)' }}>
            Default: {defaultAction || 'allow'}
          </span>
        </div>
        <button onClick={() => { resetForm(); setShowForm(true) }} style={{
          padding: '6px 14px', borderRadius: 'var(--radius)', fontSize: '13px', fontWeight: 500,
          cursor: 'pointer', border: 'none', background: 'var(--accent)', color: '#fff',
        }}>
          + Add Rule
        </button>
      </div>

      {/* Add rule form */}
      {showForm && (
        <div style={{
          padding: '16px 20px', borderBottom: '1px solid var(--border)', background: 'var(--bg)',
        }}>
          {/* Row 1: Tool, Server, Agent */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '12px', marginBottom: '12px' }}>
            <div>
              <label style={labelStyle}>Tool</label>
              <select style={inputStyle} value={form.tool} onChange={e => onToolChange(e.target.value)}>
                <option value="">Select tool...</option>
                {discoveredTools.map(t => (
                  <option key={t} value={t}>{t}</option>
                ))}
              </select>
            </div>
            <div>
              <label style={labelStyle}>Server</label>
              <input style={{ ...inputStyle, opacity: 0.7 }} value={form.server} readOnly
                placeholder="Auto-filled from tool" />
            </div>
            <div>
              <label style={labelStyle}>Agent (optional)</label>
              <select style={inputStyle} value={form.agent}
                onChange={e => setForm(f => ({ ...f, agent: e.target.value }))}>
                <option value="">All agents</option>
                {agents.map(a => (
                  <option key={a.id} value={a.id}>{a.id}</option>
                ))}
              </select>
            </div>
          </div>

          {/* Row 2: Action + conditional fields */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 2fr', gap: '12px', marginBottom: '12px' }}>
            <div>
              <label style={labelStyle}>Action</label>
              <select style={inputStyle} value={form.action}
                onChange={e => setForm(f => ({ ...f, action: e.target.value }))}>
                {availableActions.map(a => (
                  <option key={a} value={a}>{actionLabel(a)}</option>
                ))}
              </select>
            </div>
            <div>
              <label style={labelStyle}>Reason (optional)</label>
              <input style={inputStyle} value={form.reason} placeholder="Why this rule exists"
                onChange={e => setForm(f => ({ ...f, reason: e.target.value }))} />
            </div>
          </div>

          {/* Rate limit fields */}
          {form.action === 'rate_limit' && (
            <div style={{ marginBottom: '12px' }}>
              <label style={labelStyle}>Rate Limit (e.g., 10/1m, 100/1h)</label>
              <input style={{ ...inputStyle, maxWidth: '200px' }} value={form.rateLimit}
                placeholder="10/1m"
                onChange={e => setForm(f => ({ ...f, rateLimit: e.target.value }))} />
            </div>
          )}

          {/* Approval fields */}
          {form.action === 'require_approval' && (
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '12px', marginBottom: '12px' }}>
              <div>
                <label style={labelStyle}>Notify Webhook</label>
                <input style={inputStyle} value={form.notify} placeholder="https://..."
                  onChange={e => setForm(f => ({ ...f, notify: e.target.value }))} />
              </div>
              <div>
                <label style={labelStyle}>Retry Method</label>
                <select style={inputStyle} value={form.retryMethod}
                  onChange={e => setForm(f => ({ ...f, retryMethod: e.target.value }))}>
                  <option value="poll">Poll (client polls)</option>
                  <option value="callback">Callback (status webhook)</option>
                </select>
              </div>
              {form.retryMethod === 'callback' && (
                <div>
                  <label style={labelStyle}>Status Webhook</label>
                  <input style={inputStyle} value={form.statusWebhook}
                    onChange={e => setForm(f => ({ ...f, statusWebhook: e.target.value }))} />
                </div>
              )}
            </div>
          )}

          {/* Consent fields */}
          {form.action === 'require_consent' && (
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px', marginBottom: '12px' }}>
              <div>
                <label style={labelStyle}>Consent Title</label>
                <input style={inputStyle} value={form.consentTitle} placeholder="Confirm action"
                  onChange={e => setForm(f => ({ ...f, consentTitle: e.target.value }))} />
              </div>
              <div>
                <label style={labelStyle}>Consent Description</label>
                <input style={inputStyle} value={form.consentDescription} placeholder="This action will..."
                  onChange={e => setForm(f => ({ ...f, consentDescription: e.target.value }))} />
              </div>
            </div>
          )}

          {error && <div style={{ fontSize: '12px', color: 'var(--red)', marginBottom: '8px' }}>{error}</div>}

          <div style={{ display: 'flex', gap: '8px' }}>
            <button onClick={handleSubmit} disabled={loading} style={{
              padding: '6px 14px', borderRadius: 'var(--radius)', fontSize: '13px', fontWeight: 500,
              cursor: 'pointer', border: 'none', background: 'var(--accent)', color: '#fff',
            }}>
              {editingIndex !== null ? 'Update Rule' : 'Create Rule'}
            </button>
            <button onClick={resetForm} style={{
              padding: '6px 14px', borderRadius: 'var(--radius)', fontSize: '13px', fontWeight: 500,
              cursor: 'pointer', border: 'none', background: 'transparent', color: 'var(--muted)',
            }}>
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Rules table */}
      {displayRules.length > 0 ? (
        <div style={{ overflow: 'auto' }}>
          {/* Table header */}
          <div style={{
            display: 'grid',
            gridTemplateColumns: '22% 16% 16% 16% 1fr 60px',
            padding: '8px 20px',
            background: 'color-mix(in srgb, var(--border) 30%, transparent)',
            borderBottom: '1px solid var(--border)',
            fontSize: '11px', fontWeight: 600, color: 'var(--muted)',
            textTransform: 'uppercase', letterSpacing: '0.05em',
          }}>
            <span>Tool</span>
            <span>Action</span>
            <span>Server</span>
            <span>Agent</span>
            <span>Reason</span>
            <span></span>
          </div>

          {/* Table rows */}
          {displayRules.map((rule, i) => (
            <RuleRow key={i} rule={rule} index={rule.originalIndex} onDelete={handleDelete} onEdit={handleEdit} toolNames={discoveredTools} />
          ))}
        </div>
      ) : (
        <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--muted)', fontSize: '13px' }}>
          No rules configured — click + Add Rule to create your first.
        </div>
      )}
    </div>
  )
}

function RuleRow({ rule, index, onDelete, onEdit, toolNames }: {
  rule: RuleEntry & { originalIndex: number }
  index: number
  onDelete: (index: number) => void
  onEdit: (rule: RuleEntry & { originalIndex: number }) => void
  toolNames: string[]
}) {
  const [hovered, setHovered] = useState(false)
  const color = actionColor(rule.action)
  const agentDisplay = rule.agents?.length ? rule.agents.join(', ') : 'all'

  return (
    <div
      className="audit-row"
      style={{
        display: 'grid',
        gridTemplateColumns: '22% 16% 16% 16% 1fr 60px',
        padding: '8px 20px',
        borderBottom: '1px solid var(--border)',
        alignItems: 'center',
        fontSize: '13px',
        background: hovered ? 'var(--surface-hover)' : 'transparent',
        transition: 'background 0.1s',
      }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {/* Tool */}
      <span style={{ overflow: 'hidden' }}>
        <span style={{ fontFamily: MONO, color: 'var(--text)', display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {rule.tool}
        </span>
        {rule.tool.includes('*') && (() => {
          const pattern = new RegExp('^' + rule.tool.replace(/\*/g, '.*') + '$')
          const count = toolNames.filter(t => pattern.test(t)).length
          return count > 0 ? <span style={{ fontSize: '10px', color: 'var(--muted)' }}>{count} tool{count !== 1 ? 's' : ''}</span> : null
        })()}
      </span>

      {/* Action badge */}
      <span>
        <span style={{
          display: 'inline-block', padding: '2px 8px', borderRadius: '3px', fontSize: '11px',
          fontWeight: 600, fontFamily: MONO,
          background: `color-mix(in srgb, ${color} 15%, transparent)`, color,
        }}>
          {actionLabel(rule.action)}
        </span>
        {rule.action === 'rate_limit' && rule.rateLimit && (
          <span style={{ fontSize: '10px', color: 'var(--muted)', marginLeft: '4px', fontFamily: MONO }}>
            {rule.rateLimit}/{rule.ratePeriod || '1m'}
          </span>
        )}
      </span>

      {/* Server */}
      <span style={{
        color: rule.server ? 'var(--accent)' : 'var(--muted)', fontSize: '12px',
        fontFamily: MONO, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
      }}>
        {rule.server || 'all'}
      </span>

      {/* Agent */}
      <span style={{
        color: rule.agents?.length ? 'var(--text)' : 'var(--muted)', fontSize: '12px',
        fontFamily: MONO, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
      }}>
        {agentDisplay}
      </span>

      {/* Reason */}
      <span style={{
        color: 'var(--muted)', fontSize: '12px',
        overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
      }}>
        {rule.reason || ''}
      </span>

      {/* Actions */}
      <span style={{ display: 'flex', gap: '4px', justifyContent: 'flex-end' }}>
        {hovered && (
          <>
            <button onClick={() => onEdit(rule)} style={{
              padding: '2px 8px', borderRadius: 'var(--radius)', fontSize: '11px',
              background: 'transparent', color: 'var(--muted)', border: '1px solid var(--border)',
              cursor: 'pointer', fontWeight: 500,
            }}>
              edit
            </button>
            <button onClick={() => onDelete(index)} style={{
              padding: '2px 8px', borderRadius: 'var(--radius)', fontSize: '11px',
              background: 'color-mix(in srgb, var(--red) 10%, transparent)',
              color: 'var(--red)', border: '1px solid color-mix(in srgb, var(--red) 30%, transparent)',
              cursor: 'pointer', fontWeight: 500,
            }}>
              ✕
            </button>
          </>
        )}
      </span>
    </div>
  )
}
