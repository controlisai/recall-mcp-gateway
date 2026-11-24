import { useStatsStore } from '@/stores/stats-store'
import { useGatewayStore } from '@/stores/gateway-store'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

const classLabels: Record<string, string> = {
  read_only: 'READ', READ_ONLY: 'READ',
  destructive: 'DESTR', DESTRUCTIVE: 'DESTR',
  read_write: 'R/W', READ_WRITE: 'R/W',
  critical: 'CRIT', CRITICAL: 'CRIT',
  high: 'HIGH', HIGH: 'HIGH',
  medium: 'MED', MEDIUM: 'MED',
  low: 'LOW', LOW: 'LOW',
}

interface TopToolsProps {
  onToolClick?: (toolName: string) => void
}

export function TopTools({ onToolClick }: TopToolsProps) {
  const topTools = useStatsStore(s => s.topTools)
  const tools = useGatewayStore(s => s.tools)

  // Build classification lookup from gateway tool profiles
  const classMap = new Map<string, string>()
  for (const t of tools) {
    classMap.set(t.name, t.classification)
  }

  const toolStats = topTools.slice(0, 10)
  const maxCount = toolStats.length > 0 ? toolStats[0].count : 1

  if (toolStats.length === 0) {
    return (
      <div style={{
        textAlign: 'center', padding: '24px 16px', margin: '8px 0',
        border: '1px dashed var(--border)', borderRadius: 'var(--radius)',
      }}>
        <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" strokeLinecap="round" style={{ color: 'var(--muted)', opacity: 0.3, display: 'block', margin: '0 auto 8px' }}>
          <rect x="3" y="3" width="7" height="7" /><rect x="14" y="3" width="7" height="7" /><rect x="3" y="14" width="7" height="7" /><rect x="14" y="14" width="7" height="7" />
        </svg>
        <div style={{ fontSize: '12px', color: 'var(--muted)' }}>No tools called yet — your leaderboard awaits</div>
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
      {toolStats.map(tool => {
        const barWidth = (tool.count / maxCount) * 100
        const classification = classMap.get(tool.name) || 'unknown'
        const classColors: Record<string, { bg: string; color: string }> = {
          critical: { bg: 'rgba(239,68,68,0.15)', color: 'var(--red)' },
          high: { bg: 'rgba(245,158,11,0.15)', color: 'var(--amber)' },
          medium: { bg: 'rgba(59,130,246,0.1)', color: 'var(--accent)' },
          low: { bg: 'rgba(255,255,255,0.06)', color: 'var(--muted)' },
          read_only: { bg: 'rgba(34,197,94,0.15)', color: 'var(--green)' },
          read_write: { bg: 'rgba(245,158,11,0.15)', color: 'var(--amber)' },
          destructive: { bg: 'rgba(239,68,68,0.15)', color: 'var(--red)' },
        }
        const lc = classification.toLowerCase()
        const cls = classColors[lc] || classColors.low
        const clsLabel = classLabels[classification] || classification.toUpperCase()

        return (
          <div key={tool.name} onClick={() => onToolClick?.(tool.name)} style={{
            cursor: 'pointer', padding: '4px 0', borderRadius: '4px',
          }}
          onMouseEnter={e => { (e.currentTarget as HTMLDivElement).style.background = 'rgba(255,255,255,0.04)' }}
          onMouseLeave={e => { (e.currentTarget as HTMLDivElement).style.background = 'transparent' }}
          >
            <div style={{
              display: 'flex', alignItems: 'center', justifyContent: 'space-between',
              marginBottom: '2px',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '6px', minWidth: 0 }}>
                <span style={{
                  fontSize: '12px', color: 'var(--text)', fontWeight: 400, fontFamily: MONO,
                  overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                }}>
                  {tool.name}
                </span>
                {classification !== 'unknown' && (
                  <span style={{
                    fontSize: '10px', fontWeight: 700, padding: '2px 5px',
                    borderRadius: '2px', fontFamily: MONO,
                    background: cls.bg, color: cls.color,
                    letterSpacing: '0.03em',
                  }}>
                    {clsLabel}
                  </span>
                )}
                <span style={{ fontSize: '12px', color: 'var(--muted)' }}>
                  {tool.server}
                </span>
              </div>
              <span style={{
                fontSize: '14px', fontWeight: 600, fontFamily: MONO,
                color: 'var(--text)', flexShrink: 0, marginLeft: '8px',
              }}>
                {tool.count}
              </span>
            </div>

            {/* Proportional bar */}
            <div style={{
              height: '4px', borderRadius: '2px',
              background: 'rgba(255,255,255,0.06)', overflow: 'hidden',
            }}>
              <div style={{
                width: `${barWidth}%`, height: '100%', borderRadius: '2px',
                background: 'var(--accent)', transition: 'width 0.3s ease',
              }} />
            </div>
          </div>
        )
      })}
    </div>
  )
}
