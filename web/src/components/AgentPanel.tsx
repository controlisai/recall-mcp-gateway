import { useMemo, useState, useEffect } from 'react'
import { X } from 'lucide-react'
import { useFeedStore } from '@/stores/feed-store'
import { useGatewayStore } from '@/stores/gateway-store'
import { fetchAgentConfigs, fetchBaselines, fetchRulesConfig, type AgentConfig, type BaselineEntry, type RuleEntry } from '@/lib/api'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

const archDescriptions: Record<string, string> = {
  autonomous: 'Autonomous — M2M, admin approves',
  user_embedded: 'User-embedded — interactive, consent forms',
  delegated: 'Delegated — acting for user',
}

const classColors: Record<string, { bg: string; color: string; label: string }> = {
  read_only: { bg: 'rgba(34,197,94,0.15)', color: 'var(--green)', label: 'READ' },
  write: { bg: 'rgba(245,158,11,0.15)', color: 'var(--amber)', label: 'WRITE' },
  destructive: { bg: 'rgba(239,68,68,0.15)', color: 'var(--red)', label: 'DELETE' },
  external_comm: { bg: 'rgba(245,158,11,0.15)', color: 'var(--amber)', label: 'EXTERNAL' },
}

interface AgentPanelProps {
  agentId: string
  onClose: () => void
}

export function AgentPanel({ agentId, onClose }: AgentPanelProps) {
  const entries = useFeedStore(s => s.entries)
  const tools = useGatewayStore(s => s.tools)

  const [agentCfg, setAgentCfg] = useState<AgentConfig | null>(null)
  const [baselines, setBaselines] = useState<BaselineEntry[]>([])
  const [rules, setRules] = useState<RuleEntry[]>([])

  useEffect(() => {
    fetchAgentConfigs().then(d => {
      const cfg = d.agents.find(a => a.id === agentId)
      if (cfg) setAgentCfg(cfg)
    }).catch(() => {})
    fetchBaselines().then(d => {
      setBaselines(d.baselines.filter(b => b.agentId === agentId))
    }).catch(() => {})
    fetchRulesConfig().then(d => {
      // Rules that apply to this agent: either no agents filter, or includes this agent
      setRules((d.rules || []).filter(r =>
        r.tool !== '*' && (!r.agents || r.agents.length === 0 || r.agents.includes(agentId))
      ))
    }).catch(() => {})
  }, [agentId])

  const profile = useMemo(() => {
    const agentEntries = entries.filter(e => e.agentId === agentId)
    const count = agentEntries.length
    const lastSeen = agentEntries.length > 0 ? agentEntries[0].timestamp : null

    const toolCounts: Record<string, number> = {}
    let errors = 0
    for (const e of agentEntries) {
      if (e.tool && e.tool !== 'tools/list') {
        toolCounts[e.tool] = (toolCounts[e.tool] || 0) + 1
      }
      if (e.isError || e.decision === 'block') errors++
    }
    const topTools = Object.entries(toolCounts).sort((a, b) => b[1] - a[1]).slice(0, 8)
    return { count, lastSeen, topTools, errors }
  }, [entries, agentId])

  // Group tools by server for Effective Permissions
  const toolsByServer = useMemo(() => {
    const map = new Map<string, typeof tools>()
    for (const t of tools) {
      const list = map.get(t.server) || []
      list.push(t)
      map.set(t.server, list)
    }
    return map
  }, [tools])

  const archetype = agentCfg?.archetype || 'autonomous'
  const isRestricted = agentCfg?.serverAccessRestricted
  const allowed = agentCfg?.allowedServers || []
  const accessLabel = (!isRestricted && allowed.length === 0)
    ? 'All servers (no restrictions)'
    : allowed.join(', ') || 'No access (restricted)'

  return (
    <>
      <div onClick={onClose} style={{
        position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', zIndex: 100,
      }} />
      <div style={{
        position: 'fixed', top: 0, right: 0, bottom: 0, width: '420px',
        background: 'var(--surface)', borderLeft: '1px solid var(--border)',
        zIndex: 101, display: 'flex', flexDirection: 'column',
        boxShadow: '-4px 0 24px rgba(0,0,0,0.3)', overflow: 'hidden',
      }}>
        {/* Header */}
        <div style={{
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
          padding: '16px 20px', borderBottom: '1px solid var(--border)', flexShrink: 0,
        }}>
          <h2 style={{ fontSize: '16px', fontWeight: 600, fontFamily: MONO }}>{agentId}</h2>
          <button onClick={onClose} style={{
            background: 'none', border: 'none', cursor: 'pointer', color: 'var(--muted)', padding: '4px',
          }}>
            <X size={18} />
          </button>
        </div>

        {/* Content */}
        <div style={{ flex: 1, overflow: 'auto' }}>
          <Section title="Identity">
            <Row label="Agent ID" value={agentId} />
            <Row label="Archetype" value={archDescriptions[archetype] || archetype} />
            <Row label="Auth" value={agentCfg?.hasToken ? 'Bearer token' : 'JWT (external AS)'} />
            {agentCfg?.source && <Row label="Source" value={agentCfg.source} />}
            {agentCfg?.orgId && <Row label="Org" value={agentCfg.orgId} />}
          </Section>

          <Section title="Access Scope">
            <Row label="Access" value={accessLabel} />
          </Section>

          <Section title="Activity">
            <Row label="Total Calls" value={String(profile.count)} />
            <Row label="Last Seen" value={
              profile.lastSeen ? new Date(profile.lastSeen).toLocaleString() : 'Never'
            } />
            {profile.topTools.length > 0 && (
              <Row label="Top Tools" value={
                profile.topTools.map(([t, c]) => `${t} (${c})`).join(', ')
              } />
            )}
          </Section>

          {baselines.length > 0 && (
            <Section title="Baselines">
              {baselines.map(b => {
                const source = b.baseline.baselineSource || (b.baseline.baselineReady ? 'learned' : 'learning')
                return (
                  <Row key={b.tool} label={b.tool}
                    value={`${source} (${b.baseline.sampleCount} observations)`}
                  />
                )
              })}
            </Section>
          )}

          <Section title={`Effective Permissions (${tools.length} tools)`}>
            {Array.from(toolsByServer.entries()).map(([srv, srvTools]) => (
              <div key={srv} style={{ marginBottom: '10px' }}>
                <div style={{
                  fontSize: '12px', fontWeight: 600, color: 'var(--accent)',
                  marginBottom: '4px',
                }}>
                  {srv}
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px' }}>
                  {srvTools.map(t => {
                    const lc = t.classification.toLowerCase()
                    const cls = classColors[lc] || { bg: 'rgba(255,255,255,0.06)', color: 'var(--muted)', label: t.classification }
                    const isHigh = ['critical', 'high', 'destructive', 'external_comm', 'write'].includes(lc)
                    return (
                      <span key={t.name} style={{
                        display: 'inline-flex', alignItems: 'center', gap: '4px',
                        fontSize: '11px', fontFamily: MONO, padding: '3px 8px',
                        borderRadius: '4px', border: '1px solid var(--border)',
                        background: 'var(--surface)',
                      }}>
                        {isHigh && <span style={{ color: 'var(--amber)', fontSize: '10px' }}>&#9650;</span>}
                        {t.name}
                        <span style={{
                          fontSize: '9px', fontWeight: 700, padding: '1px 4px',
                          borderRadius: '2px', background: cls.bg, color: cls.color,
                        }}>
                          {cls.label}
                        </span>
                      </span>
                    )
                  })}
                </div>
              </div>
            ))}
          </Section>

          {/* Rules */}
          {rules.length > 0 && (
            <Section title={`Rules (${rules.length})`}>
              {rules.map((r, i) => {
                const actionColors: Record<string, string> = {
                  block: 'var(--red)', rate_limit: 'var(--amber)',
                  require_approval: 'var(--purple)', require_consent: 'var(--purple)',
                  allow: 'var(--green)', hide: 'var(--muted)',
                }
                return (
                  <div key={i} style={{
                    display: 'flex', alignItems: 'center', gap: '8px',
                    padding: '5px 0', fontSize: '13px',
                  }}>
                    <span style={{ fontFamily: MONO, fontWeight: 500, color: 'var(--text)' }}>
                      {r.tool}
                    </span>
                    {r.server && (
                      <span style={{ fontSize: '12px', color: 'var(--accent)' }}>{r.server}</span>
                    )}
                    <span style={{
                      fontSize: '12px', fontWeight: 600,
                      color: actionColors[r.action] || 'var(--text)',
                    }}>
                      {r.action}
                    </span>
                    {r.action === 'rate_limit' && r.rateLimit && (
                      <span style={{ fontSize: '11px', color: 'var(--muted)' }}>
                        {r.rateLimit}/{r.ratePeriod || 'minute'}
                      </span>
                    )}
                  </div>
                )
              })}
            </Section>
          )}

          {/* Platform disclaimer */}
          <div style={{
            padding: '16px 20px', borderTop: '1px solid var(--border)',
            display: 'flex', gap: '8px', alignItems: 'flex-start',
          }}>
            <span style={{ color: 'var(--muted)', fontSize: '14px', flexShrink: 0, marginTop: '1px' }}>&#9201;</span>
            <span style={{ fontSize: '12px', color: 'var(--muted)', lineHeight: 1.4 }}>
              With Recall Platform: trust levels, compliance bands, cross-product governance
            </span>
          </div>
        </div>
      </div>
    </>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ borderBottom: '1px solid var(--border)' }}>
      <div style={{
        fontSize: '12px', fontWeight: 600, textTransform: 'uppercase',
        letterSpacing: '0.05em', color: 'var(--muted)', padding: '12px 20px 6px',
      }}>
        {title}
      </div>
      <div style={{ padding: '0 20px 12px' }}>{children}</div>
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div style={{
      display: 'flex', justifyContent: 'space-between', alignItems: 'baseline',
      padding: '4px 0', fontSize: '13px', gap: '12px',
      borderBottom: '1px solid rgba(128,128,128,0.1)',
    }}>
      <span style={{ color: 'var(--muted)', flexShrink: 0 }}>{label}</span>
      <span style={{ color: 'var(--text)', textAlign: 'right', wordBreak: 'break-all' }}>{value}</span>
    </div>
  )
}
