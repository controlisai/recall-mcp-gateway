import { useState, useCallback, useEffect } from 'react'
import { Activity, ClockArrowDown, Link2 } from 'lucide-react'
import { useFeedStore } from '@/stores/feed-store'
import { useGatewayStore } from '@/stores/gateway-store'
import { fetchSequences, assertWorkflow, type SequenceAlert } from '@/lib/api'
import { FeedList } from './FeedList'
import { ContextDrawer } from './ContextDrawer'
import { AuditHistory } from './AuditHistory'
import { AgentPanel } from './AgentPanel'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

type SubTab = 'live' | 'audit' | 'sequences'

export function DiscoverTab() {
  const [subTab, setSubTab] = useState<SubTab>('live')
  const [drawerCollapsed, setDrawerCollapsed] = useState(false)
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null)
  const [toolSearch, setToolSearch] = useState<string | null>(null)
  const toggleDrawer = useCallback(() => setDrawerCollapsed(c => !c), [])
  const totalCount = useFeedStore(s => s.totalCount)
  const [sequences, setSequences] = useState<SequenceAlert[]>([])
  const [dismissedSeqs, setDismissedSeqs] = useState<Set<number>>(new Set())

  useEffect(() => {
    const load = () => { fetchSequences().then(d => setSequences(d.sequences || [])).catch(() => {}) }
    load()
    const interval = setInterval(load, 15000)
    return () => clearInterval(interval)
  }, [])

  const visibleSeqs = sequences.filter((s, i) => !dismissedSeqs.has(i) && !s.asserted)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      {/* Sub-tab bar */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: '0',
        borderBottom: '1px solid var(--border)',
        padding: '0 16px', flexShrink: 0,
        background: 'var(--bg)',
      }}>
        <SubTabButton active={subTab === 'live'} onClick={() => setSubTab('live')}>
          <Activity size={14} />
          Live{totalCount > 0 && <span style={{
            fontSize: '11px', fontWeight: 600, marginLeft: '2px',
            color: 'var(--muted)',
          }}>({totalCount})</span>}
        </SubTabButton>
        <SubTabButton active={subTab === 'audit'} onClick={() => setSubTab('audit')}>
          <ClockArrowDown size={14} />
          Audit History
        </SubTabButton>
        <SubTabButton active={subTab === 'sequences'} onClick={() => setSubTab('sequences')}>
          <Link2 size={14} />
          Sequences{visibleSeqs.length > 0 && <span style={{
            fontSize: '11px', fontWeight: 600, marginLeft: '2px',
            color: 'var(--red)',
          }}>({visibleSeqs.length})</span>}
        </SubTabButton>

        <div style={{ flex: 1 }} />

        {/* Drawer toggle (only on Live) */}
        {subTab === 'live' && (
          <button
            onClick={toggleDrawer}
            title="Toggle context drawer (])"
            style={{
              fontSize: '11px', padding: '3px 8px', borderRadius: '4px',
              border: '1px solid var(--border)', background: 'transparent',
              color: 'var(--muted)', cursor: 'pointer',
              display: 'flex', alignItems: 'center', gap: '4px',
            }}
          >
            {drawerCollapsed ? 'Show' : 'Hide'} panel
            <kbd style={{
              fontSize: '10px', padding: '0 3px', borderRadius: '2px',
              border: '1px solid var(--border)', color: 'var(--muted)',
              fontFamily: "'JetBrains Mono', monospace",
            }}>]</kbd>
          </button>
        )}
      </div>

      {/* Content */}
      <div style={{
        flex: 1, minHeight: 0, display: 'flex',
        overflow: 'hidden',
      }}>
        {subTab === 'live' ? (
          <>
            <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
              <FeedList searchOverride={toolSearch} onAgentClick={setSelectedAgent} />
            </div>
            <ContextDrawer collapsed={drawerCollapsed} onToggle={toggleDrawer} onAgentClick={setSelectedAgent} onToolClick={name => setToolSearch(name)} />
          </>
        ) : subTab === 'audit' ? (
          <div style={{ flex: 1, minWidth: 0 }}>
            <AuditHistory />
          </div>
        ) : (
          <div style={{ flex: 1, minWidth: 0, overflow: 'auto' }}>
            <SequencesList sequences={sequences} visibleSeqs={visibleSeqs}
              onAssert={async (agent, tools) => { await assertWorkflow(agent, tools); fetchSequences().then(d => setSequences(d.sequences || [])) }}
              onDismiss={i => setDismissedSeqs(prev => new Set(prev).add(i))}
              getIndex={seq => sequences.indexOf(seq)}
            />
          </div>
        )}
      </div>

      {/* Agent profile panel */}
      {selectedAgent && (
        <AgentPanel agentId={selectedAgent} onClose={() => setSelectedAgent(null)} />
      )}
    </div>
  )
}

const patternColor: Record<string, string> = {
  data_exfiltration: 'var(--red)', bulk_delete: 'var(--red)', privilege_escalation: 'var(--amber)',
}
const classLabels: Record<string, string> = {
  READ_ONLY: 'READ', WRITE: 'WRITE', DESTRUCTIVE: 'DELETE',
  EXTERNAL_COMM: 'EXT', CODE_EXECUTION: 'EXEC', DATA_TRANSFER: 'XFER',
}
const classColors: Record<string, string> = {
  READ_ONLY: 'var(--muted)', WRITE: 'var(--amber)', DESTRUCTIVE: 'var(--red)',
  EXTERNAL_COMM: 'var(--purple)', CODE_EXECUTION: 'var(--red)', DATA_TRANSFER: 'var(--amber)',
}

function SequencesList({ sequences, visibleSeqs, onAssert, onDismiss, getIndex }: {
  sequences: SequenceAlert[]
  visibleSeqs: SequenceAlert[]
  onAssert: (agent: string, tools: string[]) => void
  onDismiss: (index: number) => void
  getIndex: (seq: SequenceAlert) => number
}) {
  const tools = useGatewayStore(s => s.tools)
  const toolClassMap = Object.fromEntries(tools.map(t => [t.name, t.classification]))

  if (visibleSeqs.length === 0) {
    return (
      <div style={{ textAlign: 'center', padding: '60px 20px' }}>
        <Link2 size={32} strokeWidth={1} style={{ color: 'var(--muted)', opacity: 0.3, display: 'block', margin: '0 auto 12px' }} />
        <div style={{ fontSize: '13px', color: 'var(--muted)' }}>No suspicious sequences detected</div>
        <div style={{ fontSize: '12px', color: 'var(--muted)', opacity: 0.7, marginTop: '4px' }}>
          Patterns are monitored as agents use tools — data exfiltration, bulk deletes, and privilege escalation
        </div>
      </div>
    )
  }

  return (
    <div style={{ padding: '0 20px' }}>
      <div style={{ padding: '12px 0 8px', fontSize: '11px', color: 'var(--muted)' }}>
        {visibleSeqs.length} suspicious {visibleSeqs.length === 1 ? 'pattern' : 'patterns'} detected
        {sequences.filter(s => s.asserted).length > 0 && (
          <span> · {sequences.filter(s => s.asserted).length} marked legitimate</span>
        )}
      </div>
      {visibleSeqs.map((seq) => {
        const idx = getIndex(seq)
        const color = patternColor[seq.pattern] || 'var(--accent)'
        return (
          <div key={idx} style={{
            padding: '12px 0 12px 12px', borderBottom: '1px solid var(--border)',
            borderLeft: `3px solid ${color}`,
          }}>
            {/* Pattern + agent */}
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '4px' }}>
              <span style={{ fontSize: '13px', fontWeight: 500, color, textTransform: 'capitalize' }}>
                {seq.pattern.replace(/_/g, ' ')}
              </span>
              <span style={{ fontSize: '11px', color: 'var(--muted)', fontFamily: MONO }}>{seq.agent}</span>
              <span style={{ fontSize: '10px', color: 'var(--muted)', marginLeft: 'auto' }}>
                {seq.count}× detected
              </span>
            </div>

            {/* Tool flow with classification badges */}
            <div style={{ display: 'flex', alignItems: 'center', gap: '4px', flexWrap: 'wrap', marginBottom: '6px' }}>
              {seq.tools.map((tool, i) => {
                const cls = toolClassMap[tool] || 'READ_ONLY'
                const clsLabel = classLabels[cls] || cls
                const clsColor = classColors[cls] || 'var(--muted)'
                return (
                  <span key={i} style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
                    {i > 0 && <span style={{ color: 'var(--muted)', fontSize: '11px' }}>→</span>}
                    <span style={{
                      display: 'inline-flex', alignItems: 'center', gap: '3px',
                      fontSize: '11px', fontFamily: MONO, padding: '2px 6px', borderRadius: '3px',
                      background: 'var(--bg)', border: '1px solid var(--border)', color: 'var(--text)',
                    }}>
                      {tool}
                      <span style={{
                        fontSize: '9px', fontWeight: 700, padding: '1px 4px', borderRadius: '2px',
                        background: `color-mix(in srgb, ${clsColor} 15%, transparent)`, color: clsColor,
                      }}>
                        {clsLabel}
                      </span>
                    </span>
                  </span>
                )
              })}
            </div>

            {/* Message */}
            {seq.message && (
              <div style={{ fontSize: '11px', color: 'var(--muted)', marginBottom: '6px' }}>{seq.message}</div>
            )}

            {/* Actions */}
            <div style={{ display: 'flex', gap: '6px' }}>
              <button onClick={() => onAssert(seq.agent, seq.tools)} style={{
                fontSize: '12px', padding: '4px 12px', borderRadius: 'var(--radius)',
                background: 'transparent', color: 'var(--green)', border: '1px solid color-mix(in srgb, var(--green) 30%, transparent)',
                cursor: 'pointer', fontWeight: 500,
              }}>
                Mark as Legitimate
              </button>
              <button onClick={() => onDismiss(idx)} style={{
                fontSize: '12px', padding: '4px 12px', borderRadius: 'var(--radius)',
                background: 'transparent', color: 'var(--muted)', border: '1px solid var(--border)',
                cursor: 'pointer',
              }}>
                Dismiss
              </button>
            </div>
          </div>
        )
      })}
    </div>
  )
}

function SubTabButton({
  active, onClick, children,
}: {
  active: boolean; onClick: () => void; children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      style={{
        fontSize: '15px', fontWeight: active ? 600 : 500,
        color: active ? 'var(--text)' : 'var(--muted)',
        background: 'transparent', border: 'none',
        borderBottom: active ? '2px solid var(--accent)' : '2px solid transparent',
        padding: '10px 16px', cursor: 'pointer',
        transition: 'color 0.15s, border-color 0.15s',
        display: 'inline-flex', alignItems: 'center', gap: '5px',
      }}
    >
      {children}
    </button>
  )
}
