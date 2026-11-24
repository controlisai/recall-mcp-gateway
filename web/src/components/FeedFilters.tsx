import { useMemo } from 'react'
import { Search } from 'lucide-react'
import { useFeedStore } from '@/stores/feed-store'
import { useGatewayStore } from '@/stores/gateway-store'

export interface FeedFilterState {
  search: string
  server: string
  agent: string
  blocked: boolean
  signals: boolean
  errors: boolean
  showSystem: boolean
  showToolsList: boolean
  showSensitive: boolean
}

const inputStyle: React.CSSProperties = {
  fontSize: '13px', padding: '6px 10px', paddingLeft: '30px',
  borderRadius: '6px', border: '1px solid var(--border)',
  background: 'var(--surface)', color: 'var(--text)',
  width: '170px', outline: 'none',
}

const selectStyle: React.CSSProperties = {
  fontSize: '13px', padding: '5px 10px', borderRadius: '4px',
  border: '1px solid var(--border)', background: 'var(--surface)',
  color: 'var(--text)', outline: 'none', cursor: 'pointer',
}

const checkboxLabelStyle: React.CSSProperties = {
  fontSize: '13px', color: 'var(--muted)', display: 'flex',
  alignItems: 'center', gap: '5px', cursor: 'pointer', userSelect: 'none',
}

interface FeedFiltersProps {
  filters: FeedFilterState
  onChange: (filters: FeedFilterState) => void
}

export function FeedFilters({ filters, onChange }: FeedFiltersProps) {
  const agentCounts = useFeedStore(s => s.agentCounts)
  const blockedCount = useFeedStore(s => s.blockedCount)
  const signalCounts = useFeedStore(s => s.signalCounts)
  const entries = useFeedStore(s => s.entries)
  const servers = useGatewayStore(s => s.servers)

  const signalTotal = signalCounts.pii + signalCounts.credential + signalCounts.destructive + signalCounts.sequence

  const errorCount = useMemo(() => entries.filter(e => e.isError).length, [entries])

  const serverNames = useMemo(() => servers.map(s => s.name).sort(), [servers])
  const agentNames = useMemo(() =>
    Object.keys(agentCounts).filter(a => a !== 'system' && a !== 'anonymous').sort(),
    [agentCounts]
  )

  const set = (partial: Partial<FeedFilterState>) =>
    onChange({ ...filters, ...partial })

  return (
    <div style={{
      display: 'flex', gap: '6px', padding: '8px 14px',
      borderBottom: '1px solid var(--border)',
      flexWrap: 'wrap', alignItems: 'center',
    }}>
      {/* Search */}
      <div style={{ position: 'relative' }}>
        <Search size={13} style={{
          position: 'absolute', left: '8px', top: '50%', transform: 'translateY(-50%)',
          color: 'var(--muted)',
        }} />
        <input
          type="text"
          placeholder="Search tools..."
          value={filters.search}
          onChange={e => set({ search: e.target.value })}
          style={inputStyle}
        />
      </div>

      {/* Server dropdown */}
      <select
        value={filters.server}
        onChange={e => set({ server: e.target.value })}
        style={selectStyle}
      >
        <option value="">All servers</option>
        {serverNames.map(s => <option key={s} value={s}>{s}</option>)}
      </select>

      {/* Agent dropdown */}
      <select
        value={filters.agent}
        onChange={e => set({ agent: e.target.value })}
        style={selectStyle}
      >
        <option value="">All agents</option>
        {agentNames.map(a => <option key={a} value={a}>{a}</option>)}
      </select>

      {/* Pill toggles */}
      <PillToggle
        active={filters.blocked}
        onClick={() => set({ blocked: !filters.blocked })}
        label={`Blocked (${blockedCount})`}
        activeColor={blockedCount > 0 ? 'red' : undefined}
      />
      <PillToggle
        active={filters.signals}
        onClick={() => set({ signals: !filters.signals })}
        label={`Signals (${signalTotal})`}
        activeColor={signalTotal > 0 ? 'amber' : undefined}
      />
      <PillToggle
        active={filters.errors}
        onClick={() => set({ errors: !filters.errors })}
        label={`Errors (${errorCount})`}
        activeColor={errorCount > 0 ? 'red' : undefined}
      />

      {/* Spacer */}
      <div style={{ flex: 1 }} />

      {/* Checkboxes */}
      <label style={checkboxLabelStyle}>
        <input
          type="checkbox"
          checked={filters.showSystem}
          onChange={e => set({ showSystem: e.target.checked })}
        />
        System events
      </label>
      <label style={checkboxLabelStyle}>
        <input
          type="checkbox"
          checked={filters.showToolsList}
          onChange={e => set({ showToolsList: e.target.checked })}
        />
        tools/list
      </label>
      <label style={checkboxLabelStyle}>
        <input
          type="checkbox"
          checked={filters.showSensitive}
          onChange={e => set({ showSensitive: e.target.checked })}
        />
        Show sensitive data
      </label>
    </div>
  )
}

function PillToggle({
  active, onClick, label, activeColor,
}: {
  active: boolean; onClick: () => void; label: string; activeColor?: string
}) {
  const colorVar = activeColor === 'red' ? 'var(--red)'
    : activeColor === 'amber' ? 'var(--amber)'
    : 'var(--accent)'

  return (
    <button
      onClick={onClick}
      style={{
        fontSize: '13px', padding: '5px 12px', borderRadius: '4px',
        border: `1px solid ${active ? colorVar : 'var(--border)'}`,
        background: active
          ? (activeColor === 'red' ? 'rgba(239,68,68,0.15)'
            : activeColor === 'amber' ? 'rgba(245,158,11,0.15)'
            : 'var(--accent)')
          : 'transparent',
        color: active
          ? (activeColor ? colorVar : 'white')
          : 'var(--muted)',
        cursor: 'pointer', transition: 'all 0.1s',
        fontWeight: 500,
      }}
    >
      {label}
    </button>
  )
}
