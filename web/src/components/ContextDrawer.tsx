import { useEffect } from 'react'
import { TopAgents } from './TopAgents'
import { TopTools } from './TopTools'
import { useStatsStore } from '@/stores/stats-store'
import { periodLabel } from './StatsStrip'
import { ActivityTimeline } from './ActivityTimeline'

interface ContextDrawerProps {
  collapsed: boolean
  onToggle: () => void
  onAgentClick?: (agentId: string) => void
  onToolClick?: (toolName: string) => void
}

export function ContextDrawer({ collapsed, onToggle, onAgentClick, onToolClick }: ContextDrawerProps) {
  const period = useStatsStore((s) => s.period)
  const timeRange = periodLabel(period)

  // ] key toggle
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === ']' && !e.ctrlKey && !e.metaKey && !e.altKey) {
        const tag = (e.target as HTMLElement).tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return
        onToggle()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onToggle])

  return (
    <div
      className={`ctx-drawer${collapsed ? ' collapsed' : ''}`}
      style={{
        display: 'flex', flexDirection: 'column',
        borderLeft: '1px solid var(--border)',
        flex: collapsed ? '0 0 0%' : '0 0 40%',
        maxWidth: collapsed ? '0%' : '40%',
        opacity: collapsed ? 0 : 1,
        overflow: 'hidden',
      }}
    >
      {/* Top Agents */}
      <DrawerSection title="Top Agents" flex={2} first timeRange={timeRange}>
        <TopAgents onAgentClick={onAgentClick} />
      </DrawerSection>

      {/* Top Tools */}
      <DrawerSection title="Top Tools" flex={2} timeRange={timeRange}>
        <TopTools onToolClick={onToolClick} />
      </DrawerSection>

      {/* Activity Timeline */}
      <DrawerSection title="Activity Timeline" flex={1.5} timeRange={timeRange}>
        <ActivityTimeline />
      </DrawerSection>
    </div>
  )
}

function DrawerSection({ title, flex, children, first, timeRange }: {
  title: string; flex: number; children: React.ReactNode; first?: boolean; timeRange?: string
}) {
  return (
    <div style={{
      flex, display: 'flex', flexDirection: 'column',
      minHeight: 0, borderTop: first ? 'none' : '1px solid var(--border)',
    }}>
      <div style={{
        padding: '12px 14px', fontWeight: 600, fontSize: '14px',
        color: 'var(--text)', flexShrink: 0,
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
      }}>
        {title}
        {timeRange && <span style={{ fontSize: '11px', fontWeight: 400, color: 'var(--muted)' }}>Last {timeRange}</span>}
      </div>
      <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '0 14px 10px' }}>
        {children}
      </div>
    </div>
  )
}
