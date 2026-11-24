import { useGatewayStore } from '@/stores/gateway-store'
import { useStatsStore } from '@/stores/stats-store'
import { useFeedStore } from '@/stores/feed-store'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Consolas', monospace"

function Stat({ label, value, danger, suffix, tooltip, small }: { label: string; value: string | number; danger?: boolean; suffix?: string; tooltip?: string; small?: boolean }) {
  return (
    <div style={{
      flex: 1, padding: '10px 18px', borderRight: '1px solid var(--border)',
      display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px',
    }}>
      <div>
        <div style={{
          fontSize: '12px', color: 'var(--muted)', textTransform: 'uppercase' as const,
          letterSpacing: '0.06em', fontWeight: 600,
        }}>
          {label}
        </div>
        {tooltip && <div style={{ fontSize: '12px', color: 'var(--muted)', opacity: 0.5, marginTop: '2px', lineHeight: 1.2 }}>{tooltip}</div>}
      </div>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: '3px' }}>
        <div style={{
          fontSize: small ? '14px' : '22px', fontWeight: small ? 600 : 700, letterSpacing: '-0.03em',
          fontVariantNumeric: 'tabular-nums', fontFamily: MONO,
          color: danger ? 'var(--red)' : 'var(--text)',
        }}>
          {value}
        </div>
        {suffix && <div style={{ fontSize: '12px', color: 'var(--muted)', fontWeight: 400 }}>{suffix}</div>}
      </div>
    </div>
  )
}

/** Period label for display in headers (e.g. "Last 24h"). */
export function periodLabel(period: string): string {
  return period
}

export function StatsStrip() {
  const servers = useGatewayStore((s) => s.servers)
  const period = useStatsStore((s) => s.period)
  const totalCalls = useStatsStore((s) => s.totalCalls)
  const blockedCount = useStatsStore((s) => s.blockedCount)
  const signalCounts = useStatsStore((s) => s.signalCounts)
  const uniqueTools = useStatsStore((s) => s.uniqueTools)
  const uniqueAgents = useStatsStore((s) => s.uniqueAgents)
  const avgLatency = useStatsStore((s) => s.avgLatencyMs)
  const loading = useStatsStore((s) => s.loading)

  // Calls/min still from live feed (real-time metric)
  const entries = useFeedStore((s) => s.entries)
  const now = Date.now()
  const callsPerMin = entries.filter((e) => now - new Date(e.timestamp).getTime() < 60000).length

  const pii = signalCounts.pii || 0
  const credential = signalCounts.credential || 0
  const destructive = signalCounts.destructive || 0
  const hasSignals = pii + credential + destructive > 0
  const roundedLatency = Math.round(avgLatency)

  return (
    <div style={{
      display: 'flex', background: 'var(--bg)',
      borderBottom: '1px solid var(--border)', flexShrink: 0, overflow: 'hidden',
      alignItems: 'stretch',
    }}>
      <Stat label="Servers" value={servers.length} tooltip="Upstream MCP servers" />
      <Stat label="Tools" value={loading ? '-' : uniqueTools} tooltip="Unique tools called" />
      <Stat label="Agents" value={loading ? '-' : uniqueAgents} tooltip="Active agents observed" />
      <Stat label="Total Calls" value={loading ? '-' : totalCalls} tooltip={`In last ${period}`} />
      <Stat label="Calls / Min" value={callsPerMin} tooltip="Last 60 seconds" />
      <Stat label="Avg Latency" value={totalCalls > 0 ? roundedLatency : '-'} suffix={totalCalls > 0 ? 'ms' : undefined} tooltip="Round-trip to upstream" />
      <Stat label="Blocked" value={loading ? '-' : blockedCount} danger={blockedCount > 0} tooltip="Rules, rate limits, access" />
      {hasSignals && (
        <div style={{
          display: 'flex', alignItems: 'center', gap: '6px', padding: '10px 18px',
          flexWrap: 'wrap',
        }}>
          <div style={{
            fontSize: '12px', color: 'var(--muted)', textTransform: 'uppercase' as const,
            letterSpacing: '0.06em', fontWeight: 600, width: '100%',
          }}>
            Signals
          </div>
          {pii > 0 && <SignalChip label="PII" count={pii} color="var(--amber)" />}
          {credential > 0 && <SignalChip label="Cred" count={credential} color="var(--red)" />}
          {destructive > 0 && <SignalChip label="Destr" count={destructive} color="var(--amber)" />}
        </div>
      )}

      {/* Period indicator */}
      <div style={{
        display: 'flex', alignItems: 'center',
        padding: '10px 14px', marginLeft: 'auto', flexShrink: 0,
      }}>
        <span style={{
          fontSize: '11px', fontWeight: 500, fontFamily: MONO,
          color: 'var(--muted)', letterSpacing: '0.02em',
        }}>
          {period}
        </span>
      </div>
    </div>
  )
}

function SignalChip({ label, count, color }: { label: string; count: number; color: string }) {
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: '3px',
      fontSize: '11px', fontWeight: 500, fontFamily: MONO,
      padding: '1px 6px', borderRadius: '3px',
      background: `color-mix(in srgb, ${color} 12%, transparent)`, color,
    }}>
      <span style={{ width: '5px', height: '5px', borderRadius: '50%', background: color, display: 'inline-block' }} />
      <strong>{count}</strong> {label}
    </span>
  )
}
