import { useState, useEffect, useCallback } from 'react'
import * as Tabs from '@radix-ui/react-tabs'
import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { Moon, Sun, CheckSquare, AlertTriangle, LogOut, Radar, Brain, Shield, BookOpen } from 'lucide-react'
import { useGatewayStore } from '@/stores/gateway-store'
import { useTheme } from '@/hooks/use-theme'
import { LOGO_SRC } from '@/lib/logo'
import { fetchApprovals, fetchAgentConfigs, type AgentConfig } from '@/lib/api'
import type { TabId } from '@/lib/types'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Consolas', monospace"

interface Approval {
  id: string
  tool: string
  server: string
  agentId: string
  status: string
  createdAt: string
  expiresAt?: string
  reason?: string
  matchedRule?: string
}

interface Alert {
  type: string
  message: string
  severity: string
  tool?: string
}

function timeLeft(expiresAt?: string): string {
  if (!expiresAt) return ''
  const ms = new Date(expiresAt).getTime() - Date.now()
  if (ms <= 0) return 'expired'
  const secs = Math.floor(ms / 1000)
  if (secs < 60) return `${secs}s`
  return `${Math.floor(secs / 60)}m ${secs % 60}s`
}

export function TopBar({ tab }: { tab: TabId }) {
  const { theme, toggleTheme } = useTheme()
  const version = useGatewayStore((s) => s.version)
  const connected = useGatewayStore((s) => s.connected)
  const user = useGatewayStore((s) => s.user)
  const [showApprovals, setShowApprovals] = useState(false)
  const [showAlerts, setShowAlerts] = useState(false)
  const [approvals, setApprovals] = useState<Approval[]>([])
  const [alerts, setAlerts] = useState<Alert[]>([])
  const [, setTick] = useState(0)
  const [approvalSearch, setApprovalSearch] = useState('')
  const [approvalFilter, setApprovalFilter] = useState<string>('all')
  const [alertSearch, setAlertSearch] = useState('')
  const [alertFilter, setAlertFilter] = useState<string>('all')
  const [dismissedAlerts, setDismissedAlerts] = useState<Set<string>>(new Set())
  const [agentMap, setAgentMap] = useState<Record<string, AgentConfig>>({})

  const activeAlerts = alerts.filter((_, i) => !dismissedAlerts.has(`${i}-${alerts[i]?.message}`))
  const alertCount = activeAlerts.length

  const loadApprovals = useCallback(async () => {
    try {
      const [data, agentData] = await Promise.all([fetchApprovals(), fetchAgentConfigs()])
      const pending = ((data.approvals || []) as unknown as Approval[]).filter(a => a.status === 'pending')
      setApprovals(pending)
      const map: Record<string, AgentConfig> = {}
      for (const a of (agentData.agents || [])) map[a.id] = a
      setAgentMap(map)
    } catch { /* ignore */ }
  }, [])

  const loadAlerts = useCallback(async () => {
    try {
      const resp = await fetch('/admin/v1/status', { credentials: 'same-origin' })
      if (!resp.ok) return
      const data = await resp.json()
      setAlerts(data.alerts || [])
    } catch { /* ignore */ }
  }, [])

  // Always poll approvals (badge needs count) — faster when panel is open
  useEffect(() => {
    loadApprovals()
    const interval = setInterval(loadApprovals, showApprovals ? 3000 : 5000)
    return () => clearInterval(interval)
  }, [showApprovals, loadApprovals])

  // Always poll alerts (badge needs count) — faster when panel is open
  useEffect(() => {
    loadAlerts()
    const interval = setInterval(loadAlerts, showAlerts ? 5000 : 15000)
    return () => clearInterval(interval)
  }, [showAlerts, loadAlerts])

  // Countdown tick for approval expiry
  useEffect(() => {
    if (!showApprovals || approvals.length === 0) return
    const t = setInterval(() => setTick(n => n + 1), 1000)
    return () => clearInterval(t)
  }, [showApprovals, approvals.length])

  const handleApprovalAction = async (id: string, action: 'approve' | 'deny') => {
    try {
      await fetch(`/admin/v1/approvals/${id}/${action}`, {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
      })
      await loadApprovals()
    } catch (e) { console.error(e) }
  }

  const versionShort = version
    ? 'v' + version.replace(/-dirty$/, '').replace(/-\d+-g[0-9a-f]+$/, '')
    : ''

  const tabStyle = (id: TabId): React.CSSProperties => ({
    padding: '0 16px',
    height: '100%',
    display: 'flex',
    alignItems: 'center',
    fontSize: '15px',
    gap: '6px',
    fontWeight: 500,
    color: tab === id ? 'var(--accent)' : 'var(--muted)',
    cursor: 'pointer',
    borderBottom: `2px solid ${tab === id ? 'var(--accent)' : 'transparent'}`,
    background: 'transparent',
    border: 'none',
    borderBottomWidth: '2px',
    borderBottomStyle: 'solid',
    borderBottomColor: tab === id ? 'var(--accent)' : 'transparent',
    whiteSpace: 'nowrap',
    transition: 'color 0.15s',
    outline: 'none',
  })

  const panelStyle: React.CSSProperties = {
    position: 'absolute', top: '100%', right: 0, marginTop: '4px',
    background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: '10px',
    boxShadow: '0 8px 32px rgba(0,0,0,0.15)', width: '420px', maxHeight: '480px',
    overflow: 'auto', zIndex: 1000,
  }

  return (
    <div style={{
      display: 'flex',
      alignItems: 'center',
      height: '52px',
      padding: '0 24px',
      background: 'var(--bg)',
      borderBottom: '1px solid var(--border)',
      position: 'relative',
      zIndex: 10,
    }}>
      {/* Left: Brand */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
        <img src={LOGO_SRC} alt="ControlisAI" width="24" height="24" style={{ flexShrink: 0 }} />
        <span style={{
          display: 'inline-flex', alignItems: 'center', borderRadius: '5px', overflow: 'hidden',
          fontSize: '13px', fontWeight: 700, letterSpacing: '0.01em', lineHeight: 1,
        }}>
          <span style={{ background: 'var(--accent)', color: 'white', padding: '5px 10px' }}>Recall</span>
          <span style={{ background: 'hsl(222 16% 20%)', color: 'hsl(220 14% 75%)', padding: '5px 10px' }}>MCP Gateway</span>
        </span>
        <span style={{ fontSize: '13px', color: 'var(--muted)' }}>MCP tool control plane</span>
        {versionShort && (
          <span style={{ fontSize: '13px', color: 'var(--muted)', fontFamily: MONO }}>{versionShort}</span>
        )}
      </div>

      {/* Center: Tabs */}
      <Tabs.List style={{ display: 'flex', alignItems: 'center', height: '100%', marginLeft: '24px', gap: 0 }}>
        <Tabs.Trigger value="discover" style={{...tabStyle('discover'), display: 'inline-flex', alignItems: 'center', gap: '4px'}}><Radar size={14} />Discover</Tabs.Trigger>
        <Tabs.Trigger value="understand" style={{...tabStyle('understand'), display: 'inline-flex', alignItems: 'center', gap: '4px'}}><Brain size={14} />Understand</Tabs.Trigger>
        <Tabs.Trigger value="control" style={{...tabStyle('control'), display: 'inline-flex', alignItems: 'center', gap: '4px'}}><Shield size={14} />Control</Tabs.Trigger>
        <Tabs.Trigger value="docs" style={{...tabStyle('docs'), display: 'inline-flex', alignItems: 'center', gap: '4px'}}><BookOpen size={14} />Docs</Tabs.Trigger>
        <Tabs.Trigger value="settings" style={tabStyle('settings')}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" />
          </svg>
        </Tabs.Trigger>
      </Tabs.List>

      {/* Right side */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginLeft: 'auto' }}>
        {/* Theme toggle */}
        <button
          onClick={toggleTheme}
          title="Toggle theme"
          style={{
            background: 'none', border: '1px solid var(--border)', borderRadius: 'var(--radius)',
            color: 'var(--muted)', cursor: 'pointer', width: '32px', height: '32px',
            display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '14px',
          }}
        >
          {theme === 'dark' ? <Moon size={16} /> : <Sun size={16} />}
        </button>

        <div style={{ width: '1px', height: '20px', background: 'var(--border)', margin: '0 2px' }} />

        {/* Approvals badge + dropdown */}
        <div style={{ position: 'relative', zIndex: 1001 }}>
          <div onClick={() => { setShowApprovals(!showApprovals); setShowAlerts(false) }} style={{
            display: 'flex', alignItems: 'center', gap: '6px', padding: '6px 12px',
            borderRadius: 'var(--radius)', fontSize: '15px', fontWeight: 500,
            color: 'var(--muted)', cursor: 'pointer',
            border: showApprovals ? '1px solid var(--border)' : '1px solid transparent',
            background: showApprovals ? 'var(--surface)' : 'transparent',
          }}>
            <CheckSquare size={17} style={{ color: 'var(--purple)' }} />
            <span>Approvals</span>
            <span style={{
              fontSize: '11px', fontWeight: 700, fontFamily: MONO,
              minWidth: '20px', height: '20px', display: 'inline-flex', alignItems: 'center',
              justifyContent: 'center', borderRadius: '10px', padding: '0 5px',
              background: approvals.length > 0 ? 'var(--purple)' : 'hsla(268,65%,60%,0.15)',
              color: approvals.length > 0 ? '#fff' : 'var(--purple)',
            }}>{approvals.length}</span>
          </div>

          {showApprovals && (
            <>
              <div onClick={() => setShowApprovals(false)} style={{ position: 'fixed', inset: 0, zIndex: 998 }} />
              <div style={panelStyle}>
                {/* Header */}
                <div style={{
                  padding: '14px 16px 10px', display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                }}>
                  <span style={{ fontSize: '15px', fontWeight: 700, color: 'var(--purple)' }}>Pending Approvals</span>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <span style={{ fontSize: '12px', color: 'var(--muted)', fontFamily: MONO }}>{approvals.length} pending</span>
                    <button onClick={() => setShowApprovals(false)} style={{
                      background: 'none', border: '1px solid var(--border)', borderRadius: 'var(--radius)',
                      color: 'var(--muted)', cursor: 'pointer', fontSize: '11px', padding: '2px 8px',
                    }}>✕</button>
                  </div>
                </div>

                {/* Search */}
                <div style={{ padding: '0 16px 8px' }}>
                  <input
                    type="text" placeholder="Search by tool, agent, or reason..."
                    value={approvalSearch} onChange={e => setApprovalSearch(e.target.value)}
                    style={{
                      width: '100%', padding: '7px 10px', fontSize: '12px', borderRadius: 'var(--radius)',
                      border: '1px solid var(--border)', background: 'var(--bg)', color: 'var(--text)',
                      outline: 'none', boxSizing: 'border-box',
                    }}
                  />
                </div>

                {/* Filter pills */}
                <div style={{ padding: '0 16px 10px', display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
                  {['all', 'autonomous', 'delegated', 'expiring'].map(f => (
                    <button key={f} onClick={() => setApprovalFilter(f)} style={{
                      fontSize: '11px', padding: '3px 10px', borderRadius: '12px', cursor: 'pointer',
                      background: 'transparent', fontWeight: 500, textTransform: 'capitalize',
                      border: approvalFilter === f ? '1.5px solid var(--accent)' : '1px solid var(--border)',
                      color: approvalFilter === f ? 'var(--accent)' : 'var(--muted)',
                    }}>{f === 'expiring' ? 'Expiring soon' : f === 'all' ? 'All' : f.charAt(0).toUpperCase() + f.slice(1)}</button>
                  ))}
                </div>

                {/* Divider */}
                <div style={{ height: '1px', background: 'var(--border)' }} />

                {/* Content */}
                {(() => {
                  const search = approvalSearch.toLowerCase()
                  const filtered = approvals.filter(a => {
                    if (search && !a.tool.toLowerCase().includes(search) && !a.agentId.toLowerCase().includes(search)) return false
                    if (approvalFilter === 'expiring') {
                      const ms = a.expiresAt ? new Date(a.expiresAt).getTime() - Date.now() : Infinity
                      return ms < 120000 // < 2 minutes
                    }
                    // archetype filter would need agent config data — for now show all
                    return true
                  })
                  if (filtered.length === 0) return (
                    <div style={{ padding: '28px 16px', fontSize: '13px', color: 'var(--muted)', textAlign: 'center' }}>
                      All clear — no pending approvals
                    </div>
                  )
                  const archetypeLabel: Record<string, string> = { autonomous: 'AUTO', user_embedded: 'USER', delegated: 'DELEG' }
                  const archetypeColor: Record<string, string> = { autonomous: 'var(--accent)', user_embedded: 'var(--green)', delegated: 'var(--purple)' }
                  const timeStr = (iso: string) => { try { return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) } catch { return '' } }
                  return (
                    <>
                      {filtered.map(a => {
                        const agent = agentMap[a.agentId]
                        const arch = agent?.archetype || 'autonomous'
                        return (
                          <div key={a.id} style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)' }}>
                            {/* Tool + archetype + agent + timer */}
                            <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '2px' }}>
                              <span style={{ fontFamily: MONO, color: 'var(--text)', fontWeight: 700, fontSize: '13px' }}>{a.tool}</span>
                              <span style={{
                                fontSize: '9px', fontWeight: 700, padding: '1px 5px', borderRadius: '3px', letterSpacing: '0.04em',
                                background: `color-mix(in srgb, ${archetypeColor[arch]} 15%, transparent)`,
                                color: archetypeColor[arch],
                              }}>{archetypeLabel[arch] || 'AUTO'}</span>
                              <span style={{ fontSize: '12px', color: 'var(--muted)' }}>{a.agentId}</span>
                              {a.expiresAt && (
                                <span style={{ marginLeft: 'auto', fontSize: '11px', color: 'var(--muted)', fontFamily: MONO, display: 'flex', alignItems: 'center', gap: '3px' }}>
                                  ⏱ {timeLeft(a.expiresAt)}
                                </span>
                              )}
                            </div>
                            {/* Reason */}
                            <div style={{ fontSize: '11px', color: 'var(--muted)', marginBottom: '6px' }}>
                              Admin review required
                            </div>
                            {/* Actions + meta */}
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                              <div style={{ display: 'flex', gap: '6px' }}>
                                <button onClick={(e) => { e.stopPropagation(); handleApprovalAction(a.id, 'approve') }} style={{
                                  padding: '4px 14px', borderRadius: 'var(--radius)', fontSize: '11px',
                                  background: 'var(--green)', color: '#fff', border: 'none', cursor: 'pointer', fontWeight: 600,
                                }}>Approve</button>
                                <button onClick={(e) => { e.stopPropagation(); handleApprovalAction(a.id, 'deny') }} style={{
                                  padding: '4px 14px', borderRadius: 'var(--radius)', fontSize: '11px',
                                  background: 'transparent', color: 'var(--red)', border: '1px solid color-mix(in srgb, var(--red) 30%, transparent)',
                                  cursor: 'pointer', fontWeight: 600,
                                }}>Deny</button>
                              </div>
                              <span style={{ fontSize: '10px', color: 'var(--muted)', fontFamily: MONO }}>
                                {a.createdAt ? timeStr(a.createdAt) : ''} · rule: require_approval
                              </span>
                            </div>
                          </div>
                        )
                      })}
                      {/* Bulk actions */}
                      {filtered.length > 1 && (
                        <div style={{
                          padding: '10px 16px', display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                          borderTop: '1px solid var(--border)', background: 'var(--bg)',
                        }}>
                          <div style={{ display: 'flex', gap: '8px' }}>
                            <button onClick={() => filtered.forEach(a => handleApprovalAction(a.id, 'approve'))} style={{
                              fontSize: '12px', padding: '4px 12px', borderRadius: 'var(--radius)',
                              background: 'transparent', color: 'var(--text)', border: '1px solid var(--border)',
                              cursor: 'pointer', fontWeight: 500,
                            }}>Approve all visible</button>
                            <button onClick={() => filtered.forEach(a => handleApprovalAction(a.id, 'deny'))} style={{
                              fontSize: '12px', padding: '4px 12px', borderRadius: 'var(--radius)',
                              background: 'transparent', color: 'var(--text)', border: '1px solid var(--border)',
                              cursor: 'pointer', fontWeight: 500,
                            }}>Deny all visible</button>
                          </div>
                          <span style={{ fontSize: '11px', color: 'var(--muted)', fontFamily: MONO }}>
                            {filtered.length} of {approvals.length}
                          </span>
                        </div>
                      )}
                    </>
                  )
                })()}
              </div>
            </>
          )}
        </div>

        {/* Alerts badge + dropdown */}
        <div style={{ position: 'relative', zIndex: 1001 }}>
          <div onClick={() => { setShowAlerts(!showAlerts); setShowApprovals(false) }} style={{
            display: 'flex', alignItems: 'center', gap: '6px', padding: '6px 12px',
            borderRadius: 'var(--radius)', fontSize: '15px', fontWeight: 500,
            color: 'var(--muted)', cursor: 'pointer',
            border: showAlerts ? '1px solid var(--border)' : '1px solid transparent',
            background: showAlerts ? 'var(--surface)' : 'transparent',
          }}>
            <AlertTriangle size={17} style={{ color: 'var(--amber)' }} />
            <span>Alerts</span>
            <span style={{
              fontSize: '11px', fontWeight: 700, fontFamily: MONO,
              minWidth: '20px', height: '20px', display: 'inline-flex', alignItems: 'center',
              justifyContent: 'center', borderRadius: '10px', padding: '0 5px',
              background: alertCount > 0 ? 'var(--amber)' : 'hsla(40,95%,52%,0.15)',
              color: alertCount > 0 ? 'var(--bg)' : 'var(--amber)',
            }}>{alertCount}</span>
          </div>

          {showAlerts && (
            <>
              <div onClick={() => setShowAlerts(false)} style={{ position: 'fixed', inset: 0, zIndex: 998 }} />
              <div style={panelStyle}>
                {/* Header */}
                <div style={{
                  padding: '14px 16px 10px', display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                }}>
                  <span style={{ fontSize: '15px', fontWeight: 700, color: 'var(--amber)' }}>Alerts &amp; Signals</span>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <span style={{ fontSize: '12px', color: 'var(--muted)', fontFamily: MONO }}>{alertCount} active</span>
                    <button onClick={() => setShowAlerts(false)} style={{
                      background: 'none', border: '1px solid var(--border)', borderRadius: 'var(--radius)',
                      color: 'var(--muted)', cursor: 'pointer', fontSize: '11px', padding: '2px 8px',
                    }}>✕</button>
                  </div>
                </div>

                {/* Search */}
                <div style={{ padding: '0 16px 8px' }}>
                  <input
                    type="text" placeholder="Search alerts..."
                    value={alertSearch} onChange={e => setAlertSearch(e.target.value)}
                    style={{
                      width: '100%', padding: '7px 10px', fontSize: '12px', borderRadius: 'var(--radius)',
                      border: '1px solid var(--border)', background: 'var(--bg)', color: 'var(--text)',
                      outline: 'none', boxSizing: 'border-box',
                    }}
                  />
                </div>

                {/* Filter pills */}
                <div style={{ padding: '0 16px 10px', display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
                  {['all', 'pii', 'credentials', 'destructive', 'rate_limits'].map(f => {
                    const label: Record<string, string> = { all: 'All', pii: 'PII', credentials: 'Credentials', destructive: 'Destructive', rate_limits: 'Rate limits' }
                    return (
                      <button key={f} onClick={() => setAlertFilter(f)} style={{
                        fontSize: '11px', padding: '3px 10px', borderRadius: '12px', cursor: 'pointer',
                        background: 'transparent', fontWeight: 500,
                        border: alertFilter === f ? '1.5px solid var(--accent)' : '1px solid var(--border)',
                        color: alertFilter === f ? 'var(--accent)' : 'var(--muted)',
                      }}>{label[f]}</button>
                    )
                  })}
                </div>

                {/* Divider */}
                <div style={{ height: '1px', background: 'var(--border)' }} />

                {/* Content */}
                {(() => {
                  const search = alertSearch.toLowerCase()
                  const filtered = activeAlerts.filter(a => {
                    if (search && !a.message.toLowerCase().includes(search) && !(a.tool || '').toLowerCase().includes(search)) return false
                    if (alertFilter !== 'all' && a.type !== alertFilter) return false
                    return true
                  })
                  return filtered.length === 0 ? (
                    <div style={{ padding: '28px 16px', fontSize: '13px', color: 'var(--muted)', textAlign: 'center' }}>
                      No alerts — everything looks normal
                    </div>
                  ) : filtered.map((a) => {
                    const color = a.severity === 'critical' ? 'var(--red)' : a.severity === 'warning' ? 'var(--amber)' : 'var(--muted)'
                    const key = `${alerts.indexOf(a)}-${a.message}`
                    return (
                      <div key={key} style={{
                        display: 'flex', alignItems: 'center', gap: '8px',
                        padding: '10px 16px', borderBottom: '1px solid var(--border)', fontSize: '12px',
                      }}>
                        <span style={{ width: '6px', height: '6px', borderRadius: '50%', background: color, flexShrink: 0 }} />
                        <span style={{ color: 'var(--text)', flex: 1 }}>{a.message}</span>
                        {a.tool && <span style={{ fontFamily: MONO, color: 'var(--muted)', fontSize: '11px' }}>{a.tool}</span>}
                        <button onClick={() => setDismissedAlerts(prev => new Set(prev).add(key))} style={{
                          background: 'none', border: '1px solid var(--border)', borderRadius: 'var(--radius)',
                          color: 'var(--muted)', cursor: 'pointer', fontSize: '10px', padding: '1px 6px', flexShrink: 0,
                        }}>Dismiss</button>
                      </div>
                    )
                  })
                })()}
              </div>
            </>
          )}
        </div>

        <div style={{ width: '1px', height: '20px', background: 'var(--border)', margin: '0 2px' }} />

        {/* Connection dot */}
        <span style={{
          width: '8px', height: '8px', borderRadius: '50%', display: 'inline-block',
          background: connected ? 'var(--green)' : 'var(--red)',
        }} title={connected ? 'Connected' : 'Disconnected'} />

        {/* User menu */}
        {user && (
          <DropdownMenu.Root>
            <DropdownMenu.Trigger asChild>
              <button style={{
                width: '34px', height: '34px', borderRadius: '50%', display: 'flex',
                alignItems: 'center', justifyContent: 'center', fontSize: '14px', fontWeight: 700,
                background: 'var(--accent)', color: 'white', cursor: 'pointer', border: 'none',
              }}>
                {(user || 'A')[0].toUpperCase()}
              </button>
            </DropdownMenu.Trigger>
            <DropdownMenu.Portal>
              <DropdownMenu.Content
                style={{
                  minWidth: '160px', borderRadius: '8px', padding: '4px 0',
                  background: 'var(--bg)', border: '1px solid var(--border)',
                  boxShadow: '0 4px 24px rgba(0,0,0,0.25)', zIndex: 1000,
                }}
                sideOffset={5}
                align="end"
              >
                <div style={{ padding: '8px 12px', fontSize: '12px', color: 'var(--muted)' }}>
                  Signed in as <strong style={{ color: 'var(--text)' }}>{user || 'admin'}</strong>
                </div>
                <div style={{ height: '1px', background: 'var(--border)', margin: '4px 0' }} />
                <DropdownMenu.Item
                  style={{
                    display: 'flex', alignItems: 'center', gap: '8px', padding: '6px 12px',
                    fontSize: '13px', cursor: 'pointer', color: 'var(--text)', outline: 'none',
                  }}
                  onSelect={() => { window.location.href = '/dashboard/logout' }}
                >
                  <LogOut size={14} /> Sign out
                </DropdownMenu.Item>
              </DropdownMenu.Content>
            </DropdownMenu.Portal>
          </DropdownMenu.Root>
        )}
      </div>
    </div>
  )
}
