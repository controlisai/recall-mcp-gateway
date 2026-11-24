import { useState, useEffect, useCallback } from 'react'
import { RefreshCw, Download, Lightbulb, ShieldCheck } from 'lucide-react'
import { useGatewayStore } from '@/stores/gateway-store'
import {
  fetchRecommendations,
  fetchInsights,
  dismissRecommendation,
  fetchRulesConfig,
  type AgentConfig,
  type RuleEntry,
  type Recommendation,
  type CompliancePreview,
} from '@/lib/api'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

const severityOrder: Record<string, number> = { critical: 0, high: 1, medium: 2, warning: 3 }
const severityColor: Record<string, string> = {
  critical: 'var(--red)', high: 'var(--amber)', medium: 'var(--accent)', warning: 'var(--muted)',
}

const recCategories: Record<string, { label: string; color: string }> = {
  unprotected_critical_tool: { label: 'Security', color: 'var(--red)' },
  pii_exposure: { label: 'Compliance', color: 'var(--purple)' },
  destructive_sql: { label: 'Security', color: 'var(--red)' },
  suspicious_sequence: { label: 'Security', color: 'var(--red)' },
  rate_limit: { label: 'Performance', color: 'var(--amber)' },
}

const bandColor: Record<string, string> = {
  HEALTHY: 'var(--green)', DEGRADED: 'var(--amber)', AT_RISK: 'var(--red)', CRITICAL: 'var(--red)',
}

export function UnderstandTab() {
  const tools = useGatewayStore(s => s.tools)
  const [recs, setRecs] = useState<Recommendation[]>([])
  const [dismissedCount, setDismissedCount] = useState(0)
  const [compliance, setCompliance] = useState<CompliancePreview | null>(null)
  const [agents, setAgents] = useState<AgentConfig[]>([])
  const [rules, setRules] = useState<RuleEntry[]>([])
  const [recFilter, setRecFilter] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const loadData = useCallback(async () => {
    try {
      const { fetchAgentConfigs } = await import('@/lib/api')
      const [recData, insightData, agentData, rulesData] = await Promise.all([
        fetchRecommendations(), fetchInsights(), fetchAgentConfigs(), fetchRulesConfig(),
      ])
      setAgents(agentData.agents || [])
      setRules(rulesData.rules || [])
      setRecs(recData.recommendations || [])
      setDismissedCount(recData.dismissedCount || 0)
      setCompliance(insightData.compliancePreview || null)
    } catch (e) {
      console.error('Failed to load understand data:', e)
    }
  }, [])

  useEffect(() => {
    loadData()
    const interval = setInterval(loadData, 15000)
    return () => clearInterval(interval)
  }, [loadData])

  const handleDismiss = async (id: string, toolName?: string) => {
    try {
      await dismissRecommendation(id)
      // Also mark tool as reviewed (matching vanilla behavior)
      if (toolName) {
        try {
          await fetch(`/admin/v1/tools/${encodeURIComponent(toolName)}/review`, {
            method: 'POST', credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ note: 'Risk acknowledged — no rule needed' }),
          })
        } catch { /* ignore */ }
      }
      await loadData()
    } catch (e) { console.error(e) }
  }

  const handleApply = (rec: Recommendation) => {
    // Navigate to Rules tab with pre-filled data (matching vanilla behavior)
    // Don't mark as applied — recommendation disappears when engine detects coverage
    const rule = rec.suggestedRule
    if (rule) {
      const toolProfile = tools.find(t => t.name === rule.tool)
      const server = rule.server || toolProfile?.server || ''
      // Store prefill data for the rule editor to pick up
      sessionStorage.setItem('prefillRule', JSON.stringify({
        tool: rule.tool, server, action: rule.action || 'block',
        reason: rule.reason || rec.title,
      }))
    }
    window.location.hash = 'control'
  }

  const activeRecs = recs
    .filter(r => r.status === 'active')
    .filter(r => !recFilter || (recCategories[r.type]?.label || 'Other') === recFilter)
    .sort((a, b) => (severityOrder[a.severity] ?? 9) - (severityOrder[b.severity] ?? 9))
    .slice(0, 12)

  const appliedCount = recs.filter(r => r.status !== 'active' && r.status !== 'dismissed').length

  const score = compliance?.score ?? 0
  const band = compliance?.band || ''

  const categories = ['Security', 'Compliance', 'Performance', 'Other']

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* Stats strip */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: '24px', padding: '8px 24px',
        borderBottom: '1px solid var(--border)', flexShrink: 0, fontSize: '12px',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
          <span style={{ color: 'var(--muted)', textTransform: 'uppercase', fontSize: '10px', fontWeight: 600, letterSpacing: '0.05em' }}>Score</span>
          <span style={{ fontFamily: MONO, fontWeight: 600, fontSize: '14px', color: score > 800 ? 'var(--green)' : score > 500 ? 'var(--amber)' : 'var(--red)' }}>{score}</span>
          <span style={{ color: 'var(--muted)', fontSize: '11px' }}>/ 1000</span>
          {band && <span style={{ fontSize: '10px', fontWeight: 600, color: score > 800 ? 'var(--green)' : score > 500 ? 'var(--amber)' : 'var(--red)', letterSpacing: '0.03em' }}>{band.replace('_', ' ')}</span>}
        </div>
        <div style={{ width: '1px', height: '16px', background: 'var(--border)' }} />
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
          <span style={{ color: 'var(--muted)', textTransform: 'uppercase', fontSize: '10px', fontWeight: 600, letterSpacing: '0.05em' }}>Recommendations</span>
          <span style={{ fontFamily: MONO, fontWeight: 600, fontSize: '14px', color: activeRecs.length > 0 ? 'var(--accent)' : 'var(--muted)' }}>{activeRecs.length}</span>
          {dismissedCount > 0 && <span style={{ color: 'var(--muted)', fontSize: '11px' }}>{dismissedCount} dismissed</span>}
        </div>
        <div style={{ marginLeft: 'auto' }}>
          <button onClick={() => { setLoading(true); loadData().finally(() => setLoading(false)) }} disabled={loading} style={{
            fontSize: '11px', padding: '4px 10px', borderRadius: 'var(--radius)',
            background: 'transparent', color: 'var(--muted)', border: '1px solid var(--border)',
            cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '4px',
          }}>
            <RefreshCw size={12} style={{ animation: loading ? 'spin 1s linear infinite' : 'none' }} /> Refresh
          </button>
        </div>
      </div>

      {/* Two-column content */}
      <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
        {/* Left column: Recommendations */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', borderRight: '1px solid var(--border)', overflow: 'hidden' }}>
          <div style={{
            display: 'flex', alignItems: 'center', justifyContent: 'space-between',
            padding: '12px 20px', flexShrink: 0,
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <Lightbulb size={16} strokeWidth={1.5} style={{ color: 'var(--amber)' }} />
              <span style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text)' }}>Recommendations</span>
              {activeRecs.length > 0 && (
                <span style={{
                  fontSize: '11px', fontWeight: 600, padding: '2px 7px', borderRadius: '10px',
                  background: 'color-mix(in srgb, var(--accent) 15%, transparent)', color: 'var(--accent)',
                }}>
                  {activeRecs.length}
                </span>
              )}
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '11px', color: 'var(--muted)' }}>
              {appliedCount > 0 && <span>{appliedCount} applied</span>}
              {dismissedCount > 0 && <span>{dismissedCount} dismissed</span>}
            </div>
          </div>

          {/* Filter buttons */}
          <div style={{ display: 'flex', gap: '6px', padding: '8px 20px', flexShrink: 0 }}>
            {categories.map(cat => (
              <button key={cat} onClick={() => setRecFilter(recFilter === cat ? null : cat)} style={{
                fontSize: '11px', padding: '3px 10px', borderRadius: '12px', cursor: 'pointer',
                border: '1px solid var(--border)',
                background: recFilter === cat ? 'color-mix(in srgb, var(--accent) 12%, transparent)' : 'transparent',
                color: recFilter === cat ? 'var(--accent)' : 'var(--muted)', fontWeight: 500,
              }}>
                {cat}
              </button>
            ))}
          </div>

          {/* Recommendation items */}
          <div style={{ flex: 1, minHeight: 0, overflow: 'auto', padding: '0 20px' }}>
            {activeRecs.length === 0 ? (
              <div style={{ padding: '20px' }}>
                <div style={{
                  textAlign: 'center', padding: '32px 20px',
                  border: '1px dashed var(--border)', borderRadius: 'var(--radius)',
                }}>
                  <Lightbulb size={28} strokeWidth={1} style={{ color: 'var(--muted)', opacity: 0.3, display: 'block', margin: '0 auto 8px' }} />
                  <div style={{ fontSize: '13px', color: 'var(--muted)' }}>
                    Still learning — recommendations unlock after more observations
                  </div>
                </div>
              </div>
            ) : (
              activeRecs.map(rec => (
                <RecommendationCard key={rec.id} rec={rec} agents={agents} rules={rules} tools={tools} onDismiss={handleDismiss} onApply={handleApply} />
              ))
            )}
          </div>
        </div>

        {/* Right column: Compliance Preview */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <div style={{
            display: 'flex', alignItems: 'center', justifyContent: 'space-between',
            padding: '12px 20px', flexShrink: 0,
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <ShieldCheck size={16} strokeWidth={1.5} style={{ color: 'var(--accent)' }} />
              <span style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text)' }}>Compliance Preview</span>
            </div>
            <a href="/admin/v1/export/audit" target="_blank" style={{
              padding: '3px 8px', borderRadius: 'var(--radius)', fontSize: '11px',
              background: 'transparent', color: 'var(--muted)', border: '1px solid var(--border)',
              cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '4px',
              textDecoration: 'none',
            }}>
              <Download size={12} /> Export Audit
            </a>
          </div>

          <div style={{ flex: 1, minHeight: 0, overflow: 'auto', padding: '20px' }}>
            <ComplianceSection compliance={compliance} />
          </div>
        </div>
      </div>
    </div>
  )
}

function RecommendationCard({ rec, agents, rules, tools, onDismiss, onApply }: {
  rec: Recommendation
  agents: AgentConfig[]
  rules: RuleEntry[]
  tools: Array<{ name: string; server: string }>
  onDismiss: (id: string, toolName?: string) => void
  onApply: (rec: Recommendation) => void
}) {
  const [showBlockDropdown, setShowBlockDropdown] = useState(false)
  const [showAckDropdown, setShowAckDropdown] = useState(false)
  const cat = recCategories[rec.type] || { label: 'Other', color: 'var(--muted)' }
  const sColor = severityColor[rec.severity] || 'var(--muted)'

  const handleBlockForAgent = (agentId: string) => {
    setShowBlockDropdown(false)
    const tool = rec.suggestedRule?.tool || ''
    const toolProfile = tools.find((t: { name: string; server: string }) => t.name === tool)
    sessionStorage.setItem('prefillRule', JSON.stringify({
      tool, server: toolProfile?.server || '', action: 'block', agent: agentId,
    }))
    window.location.hash = 'control'
  }

  const handleAcknowledge = async (agentId: string) => {
    setShowAckDropdown(false)
    try {
      const { updateAgentTools, fetchAgentConfigs } = await import('@/lib/api')
      const data = await fetchAgentConfigs()
      const agent = data.agents.find((a: { id: string }) => a.id === agentId)
      const acked = new Set(agent?.acknowledgedTools || [])
      if (rec.suggestedRule?.tool) acked.add(rec.suggestedRule.tool)
      await updateAgentTools(agentId, { acknowledgedTools: Array.from(acked) })
    } catch (e) { console.error(e) }
  }

  return (
    <div style={{
      padding: '12px 0', borderBottom: '1px solid var(--border)',
      borderLeft: `3px solid ${sColor}`, paddingLeft: '12px', marginBottom: '0',
    }}>
      {/* Title row */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '4px' }}>
        <span style={{ fontSize: '13px', fontWeight: 600, color: sColor }}>{rec.title}</span>
        <span style={{
          fontSize: '12px', padding: '2px 7px', borderRadius: '3px',
          background: `color-mix(in srgb, ${cat.color} 12%, transparent)`, color: cat.color,
          fontWeight: 600,
        }}>
          {cat.label}
        </span>
      </div>

      {/* Description */}
      <div style={{ fontSize: '12px', color: 'var(--muted)', marginBottom: '6px' }}>{rec.description}</div>

      {/* Impact + Evidence inline */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px', flexWrap: 'wrap' }}>
        <span style={{ fontSize: '12px', color: 'var(--muted)' }}>Impact: <strong style={{ color: sColor }}>{rec.impactScore}</strong>/100</span>
        <div style={{ width: '80px', height: '4px', borderRadius: '2px', background: 'var(--border)', overflow: 'hidden' }}>
          <div style={{ width: `${rec.impactScore}%`, height: '100%', borderRadius: '2px', background: sColor }} />
        </div>
        {rec.evidence && (
          <span style={{ fontSize: '12px', color: 'var(--muted)', fontStyle: 'italic' }}>
            {rec.evidence}
          </span>
        )}
      </div>

      {/* Suggested rule */}
      {rec.suggestedRule && (
        <div style={{
          fontSize: '12px', fontFamily: MONO, color: 'var(--text)', padding: '4px 8px',
          background: 'var(--bg)', borderRadius: '4px', marginBottom: '8px',
          display: 'inline-flex', alignItems: 'center', gap: '6px',
        }}>
          {rec.suggestedRule.tool} →{' '}
          <span style={{
            padding: '1px 5px', borderRadius: '3px', fontWeight: 600,
            background: `color-mix(in srgb, ${rec.suggestedRule.action === 'block' ? 'var(--red)' : 'var(--purple)'} 12%, transparent)`,
            color: rec.suggestedRule.action === 'block' ? 'var(--red)' : 'var(--purple)',
          }}>
            {rec.suggestedRule.action}
          </span>
        </div>
      )}

      {/* Actions */}
      <div style={{ display: 'flex', gap: '6px' }}>
        {rec.actionMode === 'monitor' ? (
          <>
            <span style={{ fontSize: '11px', color: 'var(--muted)', padding: '3px 8px', background: 'var(--bg)', borderRadius: 'var(--radius)' }}>
              Monitor only
            </span>
            <button onClick={() => onDismiss(rec.id, rec.suggestedRule?.tool)} style={{
              fontSize: '12px', padding: '4px 12px', borderRadius: 'var(--radius)',
              background: 'transparent', color: 'var(--muted)', border: '1px solid var(--border)',
              cursor: 'pointer',
            }}>
              Dismiss
            </button>
          </>
        ) : (
          <>
            {/* Block for Agent dropdown */}
            <div style={{ position: 'relative' }}>
              <button onClick={() => { setShowBlockDropdown(!showBlockDropdown); setShowAckDropdown(false) }} style={{
                fontSize: '12px', padding: '4px 12px', borderRadius: 'var(--radius)',
                background: 'transparent', color: 'var(--muted)', border: '1px solid var(--border)',
                cursor: 'pointer', fontWeight: 500,
              }}>
                Block for Agent ▾
              </button>
              {showBlockDropdown && (
                <AgentDropdown agents={agents} rules={rules} toolName={rec.suggestedRule?.tool || ''} mode="block" onSelect={handleBlockForAgent} onClose={() => setShowBlockDropdown(false)} />
              )}
            </div>
            <button onClick={() => onApply(rec)} style={{
              fontSize: '12px', padding: '4px 12px', borderRadius: 'var(--radius)',
              background: 'var(--accent)', color: '#fff', border: 'none', cursor: 'pointer', fontWeight: 500,
            }}>
              Create Rule
            </button>
            {/* Acknowledge Risk dropdown */}
            <div style={{ position: 'relative' }}>
              <button onClick={() => { setShowAckDropdown(!showAckDropdown); setShowBlockDropdown(false) }} style={{
                fontSize: '12px', padding: '4px 12px', borderRadius: 'var(--radius)',
                background: 'transparent', color: 'var(--amber)', border: '1px solid color-mix(in srgb, var(--amber) 30%, transparent)',
                cursor: 'pointer', fontWeight: 500,
              }}>
                Acknowledge Risk ▾
              </button>
              {showAckDropdown && (
                <AgentDropdown agents={agents} rules={rules} toolName={rec.suggestedRule?.tool || ''} mode="acknowledge" onSelect={handleAcknowledge} onClose={() => setShowAckDropdown(false)} />
              )}
            </div>
            <button onClick={() => onDismiss(rec.id, rec.suggestedRule?.tool)} style={{
              fontSize: '12px', padding: '4px 12px', borderRadius: 'var(--radius)',
              background: 'transparent', color: 'var(--muted)', border: '1px solid var(--border)',
              cursor: 'pointer',
            }}>
              Dismiss
            </button>
          </>
        )}
      </div>
    </div>
  )
}

function ComplianceSection({ compliance }: { compliance: CompliancePreview | null }) {
  if (!compliance) {
    return <div style={{ color: 'var(--muted)', fontSize: '13px', textAlign: 'center', padding: '40px 0' }}>Loading compliance data...</div>
  }

  if (compliance.note && compliance.score == null) {
    return (
      <div style={{
        border: '1px dashed var(--border)', borderRadius: 'var(--radius)',
        textAlign: 'center', padding: '32px 20px',
      }}>
        <ShieldCheck size={28} strokeWidth={1} style={{ color: 'var(--muted)', opacity: 0.3, display: 'block', margin: '0 auto 8px' }} />
        <div style={{ fontSize: '13px', color: 'var(--muted)' }}>Compliance score available after more data</div>
        <div style={{ fontSize: '12px', color: 'var(--muted)', opacity: 0.6, marginTop: '4px' }}>{compliance.note}</div>
      </div>
    )
  }

  const score = compliance.score ?? 0
  const band = compliance.band || 'CRITICAL'
  const bColor = bandColor[band] || 'var(--red)'
  const trend = compliance.scoreTrend || []
  const comp = compliance.components || { toolGovernance: 0, signalSafety: 0, operationalReadiness: 0 }
  const details = compliance.details
  const bandLabel = band === 'AT_RISK' ? 'AT RISK' : band

  return (
    <div>
      {/* Score row: number + /1000 + band badge */}
      <div style={{ display: 'flex', alignItems: 'baseline', gap: '4px', marginBottom: '4px' }}>
        <span style={{ fontSize: '28px', fontWeight: 700, color: bColor, letterSpacing: '-0.03em' }}>{score}</span>
        <span style={{ fontSize: '14px', fontWeight: 400, color: 'var(--muted)' }}>/ 1000</span>
        <span style={{
          fontSize: '11px', fontWeight: 600, color: bColor, letterSpacing: '0.04em', marginLeft: 'auto',
          background: `color-mix(in srgb, ${bColor} 12%, transparent)`, padding: '2px 8px', borderRadius: '3px',
        }}>
          {bandLabel}
        </span>
      </div>

      {/* Main progress bar */}
      <div style={{ width: '100%', height: '4px', borderRadius: '2px', background: 'var(--border)', marginBottom: '8px' }}>
        <div style={{ width: `${Math.min(score / 10, 100)}%`, height: '100%', borderRadius: '2px', background: bColor, transition: 'width 0.5s cubic-bezier(0.16,1,0.3,1)' }} />
      </div>

      {/* Sparkline + delta */}
      {trend.length >= 1 ? (() => {
        const dayNames = ['Sun','Mon','Tue','Wed','Thu','Fri','Sat']
        // Build full 7-day array, filling missing days with oldest known score
        const trendMap: Record<string, typeof trend[0]> = {}
        for (const t of trend) trendMap[t.date] = t
        const oldestScore = trend[0].score
        const oldestBand = trend[0].band
        const full7: Array<{ date: string; score: number; band?: string; filled?: boolean }> = []
        for (let i = 6; i >= 0; i--) {
          const d = new Date(); d.setDate(d.getDate() - i)
          const key = d.toISOString().slice(0, 10)
          if (trendMap[key]) full7.push(trendMap[key])
          else full7.push({ date: key, score: oldestScore, band: oldestBand, filled: true })
        }
        const minS = Math.min(...full7.map(t => t.score))
        const maxS = Math.max(...full7.map(t => t.score))
        const range = maxS - minS || 1
        const padX = 4, padY = 2, tW = 200, tH = 32
        const points = full7.map((t, i) => ({
          x: padX + (i / 6) * (tW - padX * 2),
          y: padY + (1 - (t.score - minS) / range) * (tH - padY * 2),
          ...t,
        }))
        const realDays = trend.length
        const delta = realDays >= 2 ? trend[realDays - 1].score - trend[0].score : 0
        const deltaColor = delta > 0 ? 'var(--green)' : delta < 0 ? 'var(--red)' : 'var(--muted)'
        const deltaText = realDays >= 2 && delta !== 0
          ? `${delta > 0 ? '↑' : '↓'} ${delta > 0 ? '+' : ''}${delta} from ${realDays}d ago`
          : 'Stable'
        return (
          <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '16px' }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <svg viewBox={`0 0 ${tW} ${tH}`} preserveAspectRatio="none" style={{ width: '100%', height: '32px', display: 'block' }}>
                <polyline fill="none" stroke={bColor} strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round"
                  points={points.map(p => `${p.x},${p.y}`).join(' ')} />
                {points.map((p, i) => (
                  <circle key={i} cx={p.x} cy={p.y}
                    r={i === 6 ? 3 : 1.5} fill={bColor}
                    opacity={p.filled ? 0.25 : i === 6 ? 1 : 0.6} />
                ))}
              </svg>
              {/* Day labels */}
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '9px', color: 'var(--muted)', padding: `0 ${padX}px` }}>
                {points.map((p, i) => {
                  const d = new Date(p.date + 'T00:00:00')
                  return <span key={i}>{dayNames[d.getDay()]}</span>
                })}
              </div>
            </div>
            <div style={{ fontSize: '12px', fontWeight: 500, whiteSpace: 'nowrap', color: deltaColor }}>
              {deltaText}
            </div>
          </div>
        )
      })() : (
        <div style={{ fontSize: '11px', color: 'var(--muted)', fontStyle: 'italic', marginBottom: '16px' }}>Trend data collecting...</div>
      )}

      {/* Score Breakdown header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '8px' }}>
        <div style={{ fontSize: '12px', fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.3px' }}>Score Breakdown</div>
        <span onClick={() => { window.location.hash = 'docs' }} style={{ fontSize: '11px', color: 'var(--accent)', cursor: 'pointer' }}>How scoring works →</span>
      </div>

      {/* Pillar cards */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
        <PillarCard title="Tool Governance" score={comp.toolGovernance} max={400} color="var(--accent)"
          icon={<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" /><polyline points="9 12 11 14 15 10" /></svg>}
          description="Per-agent coverage of HIGH/CRITICAL tools — all agents with server access must have a rule, block, or acknowledgment."
          detail={details ? `All ${details.governedHighCritical ?? 0} high-risk tools governed` : undefined}
          gap={(() => {
            const ungoverned = details?.toolDetails?.filter(t => !t.governed).length ?? 0
            if (ungoverned > 0) return { text: `${ungoverned} ungoverned`, pts: 400 - comp.toolGovernance }
            return undefined
          })()}
          toolDetails={details?.toolDetails}
          platformRows={[{ label: 'Risk-aware classification', desc: 'PII + blast radius scoring' }]}
          fixAction={comp.toolGovernance < 400 ? () => { window.location.hash = 'control' } : undefined}
        />
        <PillarCard title="Signal Safety" score={comp.signalSafety} max={300} color="var(--green)"
          icon={<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" /><line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" /></svg>}
          description="Inverse of risky content detected in tool arguments (PII, credentials, destructive SQL)."
          detail={comp.signalSafety >= 300 ? 'No unacknowledged risky signals' : undefined}
          gap={comp.signalSafety < 300 ? { text: 'Unacknowledged signals', pts: 300 - comp.signalSafety } : undefined}
          subItems={[
            { label: 'Tool call logging', desc: 'All arguments inspected', met: true },
            { label: 'Content detection', desc: 'PII, credentials, destructive SQL patterns', met: true },
          ]}
          platformRows={[{ label: 'PII tokenization', desc: 'Vault-based data sensitivity policies' }]}
          fixAction={comp.signalSafety < 300 ? () => { window.location.hash = 'discover' } : undefined}
        />
        <PillarCard title="Ops Readiness" score={comp.operationalReadiness} max={300} color="var(--amber)"
          icon={<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><rect x="2" y="2" width="20" height="8" rx="2" /><rect x="2" y="14" width="20" height="8" rx="2" /><line x1="6" y1="6" x2="6.01" y2="6" /><line x1="6" y1="18" x2="6.01" y2="18" /></svg>}
          description="4 infrastructure checks at 75 pts each: baselines, audit storage, agent scoping, observability."
          gap={comp.operationalReadiness < 300 ? { text: 'Missing infrastructure checks', pts: 300 - comp.operationalReadiness } : undefined}
          checklist={details?.opsChecklist}
          platformRows={[
            { label: 'Behavioral monitoring', desc: 'Drift detection, EWMA scoring' },
            { label: 'Compliance exports', desc: 'EU AI Act audit evidence' },
          ]}
          fixAction={comp.operationalReadiness < 300 ? () => { window.location.hash = 'settings' } : undefined}
        />
      </div>
    </div>
  )
}

function AgentDropdown({ agents, rules, toolName, mode, onSelect, onClose }: {
  agents: AgentConfig[]
  rules: RuleEntry[]
  toolName: string
  mode: 'block' | 'acknowledge'
  onSelect: (id: string) => void
  onClose: () => void
}) {
  const pseudoSet = new Set(['system', 'unauthorized', 'anonymous'])
  const visible = agents.filter(a => !pseudoSet.has(a.id))

  // Find the server for this tool
  const toolProfile = useGatewayStore(s => s.tools).find(t => t.name === toolName)
  const toolServer = toolProfile?.server || ''

  // Classify agents by protection status
  const unprotected: AgentConfig[] = []
  const acknowledged: AgentConfig[] = []
  const governed: Array<{ id: string; via: string }> = []
  const noAccess: AgentConfig[] = []

  for (const agent of visible) {
    // Check server access
    const hasAccess = (() => {
      if (!agent.serverAccessRestricted && (!agent.allowedServers || agent.allowedServers.length === 0)) return true
      if (agent.serverAccessRestricted && (!agent.allowedServers || agent.allowedServers.length === 0)) return false
      return toolServer ? (agent.allowedServers || []).includes(toolServer) : true
    })()

    if (!hasAccess) { noAccess.push(agent); continue }

    const isBlocked = (agent.blockedTools || []).includes(toolName)
    const isAcked = (agent.acknowledgedTools || []).includes(toolName)

    // Check if covered by a rule
    let hasRule = false
    for (const r of rules) {
      if (r.tool === '*') continue
      const toolMatch = r.tool === toolName
      if (!toolMatch) continue
      const ra = r.agents || []
      if (ra.length > 0 && !ra.includes(agent.id)) continue
      hasRule = true
      break
    }

    if (isBlocked) governed.push({ id: agent.id, via: 'blocked' })
    else if (hasRule) governed.push({ id: agent.id, via: 'rule' })
    else if (isAcked) acknowledged.push(agent)
    else unprotected.push(agent)
  }

  const handleBulk = () => {
    unprotected.forEach(a => onSelect(a.id))
  }

  const headerText = mode === 'block'
    ? `Block ${toolName} for:`
    : `Acknowledge risk for ${toolName}:`

  return (
    <>
      <div onClick={onClose} style={{ position: 'fixed', inset: 0, zIndex: 998 }} />
      <div style={{
        position: 'absolute', top: '100%', left: 0, zIndex: 999, marginTop: '4px',
        background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 'var(--radius)',
        boxShadow: '0 4px 16px rgba(0,0,0,0.12)', minWidth: '280px', maxHeight: '350px', overflow: 'auto',
      }}>
        {/* Header */}
        <div style={{
          padding: '8px 12px', fontSize: '12px', color: 'var(--muted)',
          borderBottom: '1px solid var(--border)', fontWeight: 500,
        }}>
          {headerText.split(toolName).map((part, i, arr) => (
            <span key={i}>
              {part}
              {i < arr.length - 1 && <strong style={{ fontFamily: MONO, color: 'var(--text)' }}>{toolName}</strong>}
            </span>
          ))}
        </div>

        {/* All protected */}
        {unprotected.length === 0 && acknowledged.length === 0 && (
          <div style={{ padding: '10px 12px', fontSize: '12px', color: 'var(--green)' }}>
            All agents are already protected
          </div>
        )}

        {/* Unprotected agents */}
        {unprotected.map(a => (
          <div key={a.id} onClick={() => onSelect(a.id)} style={{
            display: 'flex', alignItems: 'center', justifyContent: 'space-between',
            padding: '6px 12px', fontSize: '12px', cursor: 'pointer',
          }}
            onMouseEnter={e => (e.currentTarget.style.background = 'var(--surface-hover)')}
            onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
          >
            <span style={{ fontFamily: MONO, color: 'var(--text)' }}>{a.id}</span>
            <span style={{ fontSize: '11px', color: 'var(--muted)' }}>unprotected</span>
          </div>
        ))}

        {/* Bulk action */}
        {unprotected.length > 1 && (
          <div style={{ borderTop: '1px solid var(--border)' }}>
            <div onClick={handleBulk} style={{
              display: 'flex', alignItems: 'center', justifyContent: 'space-between',
              padding: '6px 12px', fontSize: '12px', cursor: 'pointer',
              color: mode === 'block' ? 'var(--red)' : 'var(--amber)', fontWeight: 600,
            }}
              onMouseEnter={e => (e.currentTarget.style.background = 'var(--surface-hover)')}
              onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
            >
              <span>{mode === 'block' ? 'Block for all unprotected' : 'Acknowledge for all unprotected'}</span>
              <span style={{ color: 'var(--muted)', fontWeight: 400 }}>{unprotected.length} agents</span>
            </div>
          </div>
        )}

        {/* Risk accepted section */}
        {acknowledged.length > 0 && (
          <>
            <div style={{ padding: '4px 12px', fontSize: '10px', fontWeight: 600, color: 'var(--amber)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
              Risk accepted
            </div>
            {acknowledged.map(a => (
              <div key={a.id} style={{
                display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                padding: '6px 12px', fontSize: '12px', opacity: 0.7,
              }}>
                <span style={{ fontFamily: MONO, color: 'var(--text)' }}>{a.id}</span>
                <span style={{ fontSize: '11px', color: 'var(--green)' }}>acknowledged</span>
              </div>
            ))}
          </>
        )}

        {/* Already protected section */}
        {governed.length > 0 && (
          <>
            <div style={{ padding: '4px 12px', fontSize: '10px', fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
              Already protected
            </div>
            {governed.map(g => (
              <div key={g.id} style={{
                display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                padding: '6px 12px', fontSize: '12px', opacity: 0.7,
              }}>
                <span style={{ fontFamily: MONO, color: 'var(--text)' }}>{g.id}</span>
                <span style={{ fontSize: '11px', color: g.via === 'rule' ? 'var(--accent)' : 'var(--red)' }}>{g.via}</span>
              </div>
            ))}
          </>
        )}

        {/* Without server access */}
        {noAccess.length > 0 && (
          <div style={{
            padding: '6px 12px', fontSize: '10px', fontWeight: 600, color: 'var(--muted)',
            textTransform: 'uppercase', letterSpacing: '0.04em', borderTop: '1px solid var(--border)',
          }}>
            {noAccess.length} without server access
          </div>
        )}
      </div>
    </>
  )
}

function PillarCard({ title, score, max, color, icon, description, detail, gap, toolDetails, subItems, checklist, platformRows, fixAction }: {
  title: string; score: number; max: number; color: string
  icon: React.ReactNode; description: string; detail?: string
  gap?: { text: string; pts: number }
  toolDetails?: Array<{ name?: string; governed: boolean; dominantRoute?: string; coveredAgents?: number; relevantAgents?: number }>
  subItems?: Array<{ label: string; desc: string; met: boolean }>
  checklist?: Record<string, boolean>
  platformRows?: Array<{ label: string; desc: string }>
  fixAction?: () => void
}) {
  const pct = max > 0 ? (score / max) * 100 : 0
  const isFull = score >= max
  const valColor = isFull ? 'var(--green)' : color

  return (
    <div style={{
      padding: '10px 12px', borderRadius: 'var(--radius)',
      border: '1px solid var(--border)',
    }}>
      {/* Header: icon+title left, score right */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '6px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', color, fontSize: '13px', fontWeight: 600 }}>
          {icon}{title}
        </div>
        <span style={{ fontSize: '13px', fontWeight: 700, fontVariantNumeric: 'tabular-nums', color: valColor }}>
          {score}<span style={{ color: 'var(--muted)', fontWeight: 400, fontSize: '11px' }}>/{max}</span>
        </span>
      </div>

      {/* Progress bar */}
      <div style={{ width: '100%', height: '6px', borderRadius: '3px', background: 'var(--border)', marginBottom: '6px', position: 'relative', overflow: 'hidden' }}>
        <div style={{ width: `${Math.min(pct, 100)}%`, height: '100%', borderRadius: '3px', background: valColor, transition: 'width 0.5s cubic-bezier(0.16,1,0.3,1)' }} />
      </div>

      {/* Description */}
      <div style={{ fontSize: '11px', color: 'var(--muted)', lineHeight: 1.4, marginBottom: '6px' }}>{description}</div>

      {/* Gap tip — actionable hint when score is below max */}
      {gap && gap.pts > 0 && (
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '12px', padding: '4px 0', borderTop: '1px dashed var(--border)', marginTop: '4px' }}>
          <span style={{ color: 'var(--amber)' }}>⚠</span>
          <span style={{ color: 'var(--text)' }}>{gap.text}</span>
          <span style={{ color: 'var(--green)', fontWeight: 600, marginLeft: 'auto', fontSize: '11px' }}>+{gap.pts} pts if resolved</span>
        </div>
      )}

      {/* Summary line */}
      {detail && (
        <div style={{ display: 'flex', alignItems: 'center', gap: '4px', fontSize: '12px', color: 'var(--text)', padding: '4px 0', borderTop: '1px dashed var(--border)', marginTop: '4px' }}>
          <span style={{ color: 'var(--green)' }}>✓</span> {detail}
        </div>
      )}

      {/* Governed tool details */}
      {toolDetails && toolDetails.length > 0 && (() => {
        const governed = toolDetails.filter(t => t.governed)
        const ungoverned = toolDetails.filter(t => !t.governed)
        const routes: Record<string, number> = {}
        governed.forEach(t => { const r = t.dominantRoute || 'rule'; routes[r] = (routes[r] || 0) + 1 })
        return (
          <div style={{ marginTop: '4px' }}>
            {governed.length > 0 && (
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', fontSize: '12px', padding: '3px 0' }}>
                <div><span style={{ color: 'var(--green)', fontWeight: 600 }}>✓ {governed.length} governed</span>
                  <div style={{ fontSize: '11px', color: 'var(--muted)', marginLeft: '18px' }}>
                    {governed.map(t => t.name).filter(Boolean).join(', ')}
                  </div>
                </div>
                <span style={{ fontSize: '11px', color: 'var(--muted)' }}>
                  {Object.entries(routes).map(([r, c]) => `${c} ${r}`).join(', ')}
                </span>
              </div>
            )}
            {ungoverned.length > 0 && ungoverned.slice(0, 5).map((t, i) => (
              <div key={i} style={{ display: 'flex', justifyContent: 'space-between', fontSize: '12px', padding: '2px 0' }}>
                <span><span style={{ color: 'var(--amber)' }}>○</span> <span style={{ color: 'var(--muted)' }}>{t.name || '—'}</span></span>
                {t.relevantAgents != null && <span style={{ fontSize: '11px', color: 'var(--muted)' }}>{t.coveredAgents ?? 0}/{t.relevantAgents} agents</span>}
              </div>
            ))}
          </div>
        )
      })()}

      {/* Signal Safety sub-items */}
      {subItems && subItems.map((item, i) => (
        <div key={i} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', fontSize: '12px', padding: '3px 0' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
            <span style={{ color: item.met ? 'var(--green)' : 'var(--muted)' }}>{item.met ? '✓' : '○'}</span>
            <span style={{ color: item.met ? 'var(--text)' : 'var(--muted)' }}>{item.label}</span>
          </div>
          <span style={{ fontSize: '11px', color: 'var(--muted)' }}>{item.desc}</span>
        </div>
      ))}

      {/* Ops checklist */}
      {checklist && (() => {
        const opsRequired = [
          { key: 'baselines_active', label: 'Baselines active' },
          { key: 'audit_persistent', label: 'Audit persistence' },
          { key: 'agent_scoped', label: 'Agent access scoped' },
          { key: 'observability', label: 'Observability' },
        ]
        const met = opsRequired.filter(o => checklist[o.key]).length
        return (
          <div style={{ marginTop: '4px', borderTop: '1px dashed var(--border)', paddingTop: '6px' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '4px' }}>
              <span style={{ fontSize: '11px', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.3px', color: 'var(--muted)' }}>Checklist</span>
              <span style={{ fontSize: '11px', fontWeight: 600, color: met === opsRequired.length ? 'var(--green)' : 'var(--amber)' }}>
                {met}/{opsRequired.length} required
              </span>
            </div>
            {opsRequired.map(o => {
              const isMet = !!checklist[o.key]
              return (
                <div key={o.key} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', fontSize: '12px', padding: '3px 0' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                    <span style={{ color: isMet ? 'var(--green)' : 'var(--amber)' }}>{isMet ? '✓' : '○'}</span>
                    <span style={{ color: isMet ? 'var(--text)' : 'var(--muted)' }}>{o.label}</span>
                  </div>
                  <span style={{ fontSize: '11px', color: isMet ? 'var(--muted)' : 'var(--green)', fontWeight: isMet ? 400 : 600 }}>+75 pts</span>
                </div>
              )
            })}
          </div>
        )
      })()}

      {/* Platform upsell rows */}
      {platformRows && platformRows.map((row, i) => (
        <div key={i} style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '12px', padding: '3px 0' }}>
          <span style={{ color: 'var(--muted)' }}>○</span>
          <span style={{ color: 'var(--muted)' }}>{row.label}</span>
          <span style={{ fontSize: '11px', color: 'var(--muted)', marginLeft: 'auto' }}>{row.desc}</span>
          <span onClick={() => { window.location.hash = 'settings' }} style={{ fontSize: '11px', color: 'var(--accent)', cursor: 'pointer', flexShrink: 0 }}>Connect →</span>
        </div>
      ))}

      {fixAction && (
        <button onClick={fixAction} style={{
          marginTop: '8px', fontSize: '11px', padding: '3px 12px', borderRadius: 'var(--radius)',
          background: 'var(--accent)', color: '#fff', border: 'none', cursor: 'pointer', fontWeight: 500,
        }}>Fix</button>
      )}
    </div>
  )
}
