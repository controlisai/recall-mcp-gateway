import { useRef, useMemo, useCallback, useState, useEffect, memo } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useFeedStore } from '@/stores/feed-store'
import { useGatewayStore } from '@/stores/gateway-store'
import { FeedRow } from './FeedRow'
import { FeedFilters, type FeedFilterState } from './FeedFilters'
import { OnboardingChecklist } from './OnboardingChecklist'
import type { Entry } from '@/lib/types'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

interface FlowGroup {
  id: string
  agent: string
  entries: Entry[]
}

function isSystemEntry(e: Entry): boolean {
  return e.agentId === 'system' || e.tool === '(server-lifecycle)'
}

function buildFlowGroups(entries: Entry[]): FlowGroup[] {
  const groups: FlowGroup[] = []
  let current: FlowGroup | null = null

  for (let i = 0; i < entries.length; i++) {
    const e = entries[i]
    if (
      !isSystemEntry(e) && current && !isSystemEntry(current.entries[0]) &&
      e.agentId === current.agent
    ) {
      const lastTs = new Date(current.entries[current.entries.length - 1].timestamp).getTime()
      const thisTs = new Date(e.timestamp).getTime()
      if (Math.abs(lastTs - thisTs) < 10000) {
        current.entries.push(e)
        continue
      }
    }
    current = { id: `flow-${i}`, agent: e.agentId, entries: [e] }
    groups.push(current)
  }

  return groups
}

function applyFilters(entries: Entry[], filters: FeedFilterState): Entry[] {
  return entries.filter(e => {
    if (!filters.showSystem && isSystemEntry(e)) return false
    if (!filters.showToolsList && e.tool === 'tools/list') return false
    if (filters.search && !e.tool.toLowerCase().includes(filters.search.toLowerCase())) return false
    if (filters.server && e.server !== filters.server) return false
    if (filters.agent && e.agentId !== filters.agent) return false
    if (filters.blocked && e.decision !== 'block' && e.decision !== 'rate_limited') return false
    if (filters.signals && (!e.signals || e.signals.length === 0)) return false
    if (filters.errors && !e.isError) return false
    return true
  })
}

const defaultFilters: FeedFilterState = {
  search: '', server: '', agent: '',
  blocked: false, signals: false, errors: false,
  showSystem: false, showToolsList: false, showSensitive: false,
}

interface FeedListProps {
  searchOverride?: string | null
  onAgentClick?: (agentId: string) => void
}

export function FeedList({ searchOverride, onAgentClick }: FeedListProps) {
  const entries = useFeedStore(s => s.entries)
  const tools = useGatewayStore(s => s.tools)
  const [filters, setFilters] = useState<FeedFilterState>(defaultFilters)

  // Apply external search override (e.g. from Top Tools click)
  useEffect(() => {
    if (searchOverride != null) {
      setFilters(f => ({ ...f, search: searchOverride }))
    }
  }, [searchOverride])

  const toolClassifications = useMemo(() => {
    const m = new Map<string, string>()
    for (const t of tools) m.set(t.name, t.classification)
    return m
  }, [tools])

  const filtered = useMemo(
    () => applyFilters(entries, filters),
    [entries, filters]
  )

  const flowGroups = useMemo(() => buildFlowGroups(filtered), [filtered])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <FeedFilters filters={filters} onChange={setFilters} />
      <div style={{ flex: 1, minHeight: 0 }}>
        {flowGroups.length === 0 ? (
          <EmptyFeed hasFilters={
            filters.search !== '' || filters.server !== '' || filters.agent !== '' ||
            filters.blocked || filters.signals || filters.errors
          } />
        ) : (
          <VirtualFeedList
            flowGroups={flowGroups}
            toolClassifications={toolClassifications}
            showSensitive={filters.showSensitive}
            onAgentClick={onAgentClick}
          />
        )}
      </div>
    </div>
  )
}

const VirtualFeedList = memo(function VirtualFeedList({
  flowGroups,
  toolClassifications,
  showSensitive,
  onAgentClick,
}: {
  flowGroups: FlowGroup[]
  toolClassifications: Map<string, string>
  showSensitive: boolean
  onAgentClick?: (agentId: string) => void
}) {
  const parentRef = useRef<HTMLDivElement>(null)

  const virtualizer = useVirtualizer({
    count: flowGroups.length,
    getScrollElement: () => parentRef.current,
    estimateSize: useCallback(() => 64, []),
    overscan: 15,
  })

  return (
    <div ref={parentRef} style={{ height: '100%', overflow: 'auto' }}>
      <div style={{
        height: `${virtualizer.getTotalSize()}px`,
        width: '100%', position: 'relative',
      }}>
        {virtualizer.getVirtualItems().map(virtualRow => {
          const group = flowGroups[virtualRow.index]
          return (
            <div
              key={group.id}
              data-index={virtualRow.index}
              ref={virtualizer.measureElement}
              style={{
                position: 'absolute', top: 0, left: 0, width: '100%',
                transform: `translateY(${virtualRow.start}px)`,
              }}
            >
              {group.entries.length === 1 ? (
                <FeedRow
                  entry={group.entries[0]}
                  classification={toolClassifications.get(group.entries[0].tool)}
                  showSensitive={showSensitive}
                  onAgentClick={onAgentClick}
                />
              ) : (
                <FlowGroupRow
                  group={group}
                  toolClassifications={toolClassifications}
                  showSensitive={showSensitive}
                  onAgentClick={onAgentClick}
                />
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
})

function FlowGroupRow({
  group,
  toolClassifications,
  showSensitive,
  onAgentClick,
}: {
  group: FlowGroup
  toolClassifications: Map<string, string>
  showSensitive: boolean
  onAgentClick?: (agentId: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const hasErrors = group.entries.some(e => e.isError || e.decision === 'block')
  const totalLatency = group.entries.reduce((sum, e) => sum + (e.latencyMs || 0), 0)
  const toolChain = group.entries.map(e => e.tool).join(' → ')

  return (
    <div>
      {/* Flow header */}
      <div
        onClick={() => setExpanded(!expanded)}
        style={{
          position: 'relative',
          display: 'grid', gridTemplateColumns: '10px 1fr auto',
          gap: '8px', alignItems: 'center',
          padding: '6px 12px',
          borderBottom: '1px solid var(--border)',
          cursor: 'pointer', transition: 'background 0.1s',
        }}
        onMouseEnter={e => { (e.currentTarget as HTMLDivElement).style.background = 'rgba(255,255,255,0.02)' }}
        onMouseLeave={e => { (e.currentTarget as HTMLDivElement).style.background = 'transparent' }}
      >
        {/* Edge bar */}
        <div style={{
          position: 'absolute', left: 0, top: 0, bottom: 0, width: '3px',
          borderRadius: '0 2px 2px 0',
          background: hasErrors ? 'var(--red)' : 'var(--green)',
        }} />

        {/* Expand indicator */}
        <div style={{ fontSize: '10px', color: 'var(--muted)', paddingTop: '1px' }}>
          {expanded ? '▼' : '▶'}
        </div>

        {/* Flow info */}
        <div style={{ minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
            <span style={{
              fontSize: '11px', fontWeight: 600, color: 'var(--accent)',
              fontFamily: MONO,
            }}>
              Flow: {group.entries.length} calls
            </span>
            <span style={{ fontSize: '12px', color: 'var(--muted)' }}>
              {group.agent}
            </span>
          </div>
          <div style={{
            fontSize: '12px', fontFamily: MONO, color: 'var(--muted)',
            marginTop: '1px', overflow: 'hidden', textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}>
            {toolChain}
          </div>
        </div>

        {/* Latency */}
        <div style={{
          fontSize: '12px', fontFamily: MONO, color: 'var(--text)',
          fontWeight: 500, textAlign: 'right',
        }}>
          {totalLatency > 0 ? `${totalLatency}ms` : ''}
        </div>
      </div>

      {/* Expanded children */}
      {expanded && group.entries.map(entry => (
        <FeedRow
          key={entry.requestId}
          entry={entry}
          classification={toolClassifications.get(entry.tool)}
          showSensitive={showSensitive}
          isFlowChild
          onAgentClick={onAgentClick}
        />
      ))}
    </div>
  )
}

function EmptyFeed({ hasFilters }: { hasFilters: boolean }) {
  if (!hasFilters) {
    return <OnboardingChecklist />
  }

  return (
    <div style={{ padding: '20px' }}>
      <div style={{
        textAlign: 'center', padding: '32px 20px',
        border: '1px dashed var(--border)', borderRadius: 'var(--radius)',
        width: '100%',
      }}>
        <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" strokeLinecap="round" style={{ color: 'var(--muted)', opacity: 0.3, display: 'block', margin: '0 auto 10px' }}>
          <polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3" />
        </svg>
        <div style={{ fontSize: '13px', color: 'var(--muted)' }}>
          No calls match your filters
        </div>
      </div>
    </div>
  )
}
