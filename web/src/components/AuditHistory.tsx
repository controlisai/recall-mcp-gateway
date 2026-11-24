import { useState, useCallback, useEffect } from 'react'
import { Search, Download } from 'lucide-react'
import { fetchAuditSearch, type AuditSearchParams } from '@/lib/api'
import type { Entry } from '@/lib/types'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

const inputStyle: React.CSSProperties = {
  fontSize: '12px', padding: '5px 10px', borderRadius: '6px',
  border: '1px solid var(--border)', background: 'var(--surface)',
  color: 'var(--text)', outline: 'none', width: '140px',
}

const selectStyle: React.CSSProperties = {
  fontSize: '12px', padding: '5px 10px', borderRadius: '6px',
  border: '1px solid var(--border)', background: 'var(--surface)',
  color: 'var(--text)', outline: 'none', cursor: 'pointer',
}

const thStyle: React.CSSProperties = {
  padding: '6px 16px', textAlign: 'left', fontSize: '12px',
  fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase',
  letterSpacing: '0.04em', background: 'rgba(255,255,255,0.03)',
  position: 'sticky', top: 0, zIndex: 1,
}

const tdStyle: React.CSSProperties = {
  padding: '10px 16px', borderBottom: '1px solid var(--border)',
  verticalAlign: 'middle', whiteSpace: 'nowrap',
  overflow: 'hidden', textOverflow: 'ellipsis', fontSize: '13px',
}

type TimeRange = '1h' | '6h' | '24h' | '7d' | '30d'

function rangeToISO(range: TimeRange): string {
  const ms: Record<TimeRange, number> = {
    '1h': 3600000,
    '6h': 21600000,
    '24h': 86400000,
    '7d': 604800000,
    '30d': 2592000000,
  }
  return new Date(Date.now() - ms[range]).toISOString()
}

export function AuditHistory() {
  const [agent, setAgent] = useState('')
  const [tool, setTool] = useState('')
  const [decision, setDecision] = useState('')
  const [server, setServer] = useState('')
  const [range, setRange] = useState<TimeRange>('24h')
  const [showSystem, setShowSystem] = useState(false)
  const [showToolsList, setShowToolsList] = useState(false)
  const [results, setResults] = useState<Entry[] | null>(null)
  const [sortCol, setSortCol] = useState<string>('timestamp')
  const [sortAsc, setSortAsc] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const runSearch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const params: AuditSearchParams = {
        from: rangeToISO(range),
        limit: 500,
      }
      if (agent) params.agent = agent
      if (tool) params.tool = tool
      if (decision) params.decision = decision
      if (server) params.server = server

      const resp = await fetchAuditSearch(params)
      let entries = resp.entries || []

      // Client-side filters for system/tools-list
      if (!showSystem) {
        entries = entries.filter(e => e.agentId !== 'system' && e.tool !== '(server-lifecycle)')
      }
      if (!showToolsList) {
        entries = entries.filter(e => e.tool !== 'tools/list')
      }

      setResults(entries)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Search failed')
    } finally {
      setLoading(false)
    }
  }, [agent, tool, decision, server, range, showSystem, showToolsList])

  // Auto-search on mount
  useEffect(() => { runSearch() }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const exportCSV = useCallback(() => {
    if (!results || results.length === 0) return
    const headers = ['timestamp', 'agent', 'tool', 'server', 'decision', 'latencyMs', 'mode', 'signals']
    const rows = results.map(e => [
      e.timestamp, e.agentId, e.tool, e.server, e.decision,
      String(e.latencyMs), e.mode,
      (e.signals || []).map(s => s.name).join(';'),
    ])
    const csv = [headers.join(','), ...rows.map(r => r.map(v => `"${v}"`).join(','))].join('\n')
    const blob = new Blob([csv], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `audit-${new Date().toISOString().slice(0, 10)}.csv`
    a.click()
    URL.revokeObjectURL(url)
  }, [results])

  const checkboxLabel: React.CSSProperties = {
    fontSize: '12px', color: 'var(--muted)', display: 'flex',
    alignItems: 'center', gap: '4px', cursor: 'pointer', userSelect: 'none',
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
      {/* Search bar */}
      <div style={{
        flexShrink: 0, padding: '10px 14px',
        borderBottom: '1px solid var(--border)',
        display: 'flex', gap: '8px', flexWrap: 'wrap', alignItems: 'center',
      }}>
        <input style={inputStyle} placeholder="Agent" value={agent}
          onChange={e => setAgent(e.target.value)} />
        <input style={inputStyle} placeholder="Tool (glob)" value={tool}
          onChange={e => setTool(e.target.value)} />
        <select style={selectStyle} value={decision}
          onChange={e => setDecision(e.target.value)}>
          <option value="">All decisions</option>
          <option value="allow">allow</option>
          <option value="block">block</option>
          <option value="rate_limited">rate_limited</option>
          <option value="pending_approval">pending_approval</option>
          <option value="pending_consent">pending_consent</option>
        </select>
        <input style={inputStyle} placeholder="Server" value={server}
          onChange={e => setServer(e.target.value)} />
        <select style={selectStyle} value={range}
          onChange={e => setRange(e.target.value as TimeRange)}>
          <option value="1h">Last 1h</option>
          <option value="6h">Last 6h</option>
          <option value="24h">Last 24h</option>
          <option value="7d">Last 7d</option>
          <option value="30d">Last 30d</option>
        </select>
        <button
          onClick={runSearch}
          disabled={loading}
          style={{
            fontSize: '12px', padding: '5px 14px', borderRadius: '6px',
            border: 'none', background: 'var(--accent)', color: 'white',
            cursor: loading ? 'wait' : 'pointer', fontWeight: 600,
            display: 'flex', alignItems: 'center', gap: '4px',
          }}
        >
          <Search size={12} />
          {loading ? 'Searching...' : 'Search'}
        </button>
        {results !== null && (
          <span style={{ fontSize: '13px', color: 'var(--muted)' }}>
            {results.length} results
          </span>
        )}
        <label style={checkboxLabel}>
          <input type="checkbox" checked={showSystem}
            onChange={e => setShowSystem(e.target.checked)} />
          System
        </label>
        <label style={checkboxLabel}>
          <input type="checkbox" checked={showToolsList}
            onChange={e => setShowToolsList(e.target.checked)} />
          tools/list
        </label>

        {/* Export */}
        {results && results.length > 0 && (
          <>
            <div style={{ flex: 1 }} />
            <button
              onClick={exportCSV}
              style={{
                fontSize: '12px', padding: '5px 10px', borderRadius: '6px',
                border: '1px solid var(--border)', background: 'transparent',
                color: 'var(--muted)', cursor: 'pointer',
                display: 'flex', alignItems: 'center', gap: '4px',
              }}
            >
              <Download size={12} />
              Export CSV
            </button>
          </>
        )}
      </div>

      {/* Stats bar — matching vanilla: Total badge, avg latency, decision breakdown, agent breakdown */}
      {results !== null && results.length > 0 && (() => {
        const avgLat = results.length > 0
          ? (results.reduce((s, e) => s + (e.latencyMs || 0), 0) / results.length).toFixed(1)
          : '0'
        const decisions: Record<string, number> = {}
        const agents: Record<string, number> = {}
        for (const e of results) {
          decisions[e.decision] = (decisions[e.decision] || 0) + 1
          agents[e.agentId] = (agents[e.agentId] || 0) + 1
        }
        const decColors: Record<string, string> = {
          allow: 'var(--green)', block: 'var(--red)', rate_limited: 'var(--amber)',
          pending_approval: 'var(--purple)', pending_consent: 'var(--purple)',
        }
        return (
          <div style={{
            flexShrink: 0, padding: '8px 14px',
            borderBottom: '1px solid var(--border)',
            fontSize: '13px', color: 'var(--muted)',
            display: 'flex', gap: '12px', alignItems: 'center', flexWrap: 'wrap',
          }}>
            <span style={{
              fontWeight: 600, fontSize: '12px', padding: '2px 8px',
              borderRadius: '4px', border: '1px solid var(--border)',
              color: 'var(--text)',
            }}>
              Total: {results.length}
            </span>
            <span>Avg latency: <strong style={{ color: 'var(--text)' }}>{avgLat} ms</strong></span>
            <span style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
              Decisions:
              {Object.entries(decisions).map(([d, c]) => (
                <span key={d} style={{
                  fontSize: '12px', padding: '1px 6px', borderRadius: '3px',
                  background: `color-mix(in srgb, ${decColors[d] || 'var(--muted)'} 15%, transparent)`,
                  color: decColors[d] || 'var(--muted)', fontWeight: 500,
                }}>
                  {d}({c})
                </span>
              ))}
            </span>
            <span>
              Agents: {(() => {
                const sorted = Object.entries(agents).sort((a, b) => (b[1] as number) - (a[1] as number))
                const shown = sorted.slice(0, 5)
                const rest = sorted.length - shown.length
                return shown.map(([a, c]) => `${a} (${c})`).join(', ') + (rest > 0 ? `, +${rest} more` : '')
              })()}
            </span>
          </div>
        )
      })()}

      {/* Error */}
      {error && (
        <div style={{
          padding: '10px 14px', color: 'var(--red)', fontSize: '13px',
          borderBottom: '1px solid var(--border)',
        }}>
          {error}
        </div>
      )}

      {/* Results table */}
      <div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
        {results === null ? (
          <div style={{
            color: 'var(--muted)', fontSize: '13px', padding: '20px',
          }}>
            <div style={{
              textAlign: 'center', padding: '32px 20px',
              border: '1px dashed var(--border)', borderRadius: 'var(--radius)',
              width: '100%',
            }}>
              Use the search bar above to query audit history
            </div>
          </div>
        ) : results.length === 0 ? (
          <div style={{
            color: 'var(--muted)', fontSize: '13px', padding: '20px',
          }}>
            <div style={{
              textAlign: 'center', padding: '32px 20px',
              border: '1px dashed var(--border)', borderRadius: 'var(--radius)',
              width: '100%',
            }}>
              No results found
            </div>
          </div>
        ) : (
          <table style={{
            width: '100%', borderCollapse: 'collapse',
            fontSize: '13px',
          }}>
            <thead>
              <tr>
                {[
                  { key: 'timestamp', label: 'Time', pl: '40px' },
                  { key: 'agentId', label: 'Agent' },
                  { key: 'userId', label: 'User' },
                  { key: 'tool', label: 'Tool' },
                  { key: 'server', label: 'Server' },
                  { key: 'decision', label: 'Decision' },
                  { key: 'latencyMs', label: 'Latency' },
                  { key: 'isError', label: 'Error', pr: '40px' },
                ].map(col => (
                  <th key={col.key}
                    onClick={() => { setSortAsc(sortCol === col.key ? !sortAsc : false); setSortCol(col.key) }}
                    style={{
                      ...thStyle,
                      ...(col.pl ? { paddingLeft: col.pl } : {}),
                      ...(col.pr ? { paddingRight: col.pr } : {}),
                      cursor: 'pointer', userSelect: 'none',
                    }}
                  >
                    {col.label}
                    {sortCol === col.key && <span style={{ marginLeft: '4px', fontSize: '10px' }}>{sortAsc ? '▲' : '▼'}</span>}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {[...results].sort((a, b) => {
                const key = sortCol as keyof Entry
                const va = a[key], vb = b[key]
                const cmp = typeof va === 'number' && typeof vb === 'number' ? va - vb
                  : String(va ?? '').localeCompare(String(vb ?? ''))
                return sortAsc ? cmp : -cmp
              }).map(e => (
                <AuditRow key={e.requestId} entry={e} />
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

function AuditRow({ entry }: { entry: Entry }) {
  const decColors: Record<string, string> = {
    allow: 'var(--green)', block: 'var(--red)', rate_limited: 'var(--amber)',
    pending_approval: 'var(--purple)', pending_consent: 'var(--purple)',
  }
  const dc = decColors[entry.decision] || 'var(--muted)'

  const time = (() => {
    try {
      return new Date(entry.timestamp).toLocaleTimeString([], {
        hour: '2-digit', minute: '2-digit', second: '2-digit',
      })
    } catch { return entry.timestamp }
  })()

  return (
    <tr className="audit-row">
      <td style={{ ...tdStyle, paddingLeft: '40px', fontFamily: MONO, fontSize: '12px' }}>
        {time}
      </td>
      <td style={{ ...tdStyle, fontWeight: 500 }}>{entry.agentId}</td>
      <td style={{ ...tdStyle, color: 'var(--muted)' }}>{entry.userId || ''}</td>
      <td style={{ ...tdStyle, fontFamily: MONO }}>{entry.tool}</td>
      <td style={tdStyle}>{entry.server}</td>
      <td style={tdStyle}>
        <span style={{
          fontSize: '12px', padding: '1px 6px', borderRadius: '3px',
          background: `color-mix(in srgb, ${dc} 15%, transparent)`,
          color: dc, fontWeight: 500,
        }}>
          {entry.decision}
        </span>
      </td>
      <td style={{ ...tdStyle, fontFamily: MONO, textAlign: 'right' }}>
        {entry.latencyMs || 0} ms
      </td>
      <td style={{ ...tdStyle, paddingRight: '40px', color: 'var(--red)', fontSize: '12px' }}>
        {entry.isError ? (entry.errorMessage || 'error') : ''}
      </td>
    </tr>
  )
}
