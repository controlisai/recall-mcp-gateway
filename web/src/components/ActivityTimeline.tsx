import { useMemo } from 'react'
import { useStatsStore } from '@/stores/stats-store'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"
const HOURS = 24
const BAR_GAP = 1

export function ActivityTimeline() {
  const hourlyActivity = useStatsStore(s => s.hourlyActivity)
  const avgLatencyMs = useStatsStore(s => s.avgLatencyMs)

  const { buckets, maxCount, labels, totalCalls, avgLatency, peakHour } = useMemo(() => {
    const now = Date.now()
    const bucketSize = 3600000 // 1 hour
    const buckets = new Array(HOURS).fill(null).map(() => ({ total: 0, errors: 0 }))

    // Map hourly activity from stats endpoint into 24-hour buckets
    for (const h of hourlyActivity) {
      const ts = new Date(h.hour).getTime()
      const hoursAgo = Math.floor((now - ts) / bucketSize)
      if (hoursAgo >= 0 && hoursAgo < HOURS) {
        const idx = HOURS - 1 - hoursAgo
        buckets[idx].total += h.total
        buckets[idx].errors += h.errors
      }
    }

    let totalCalls = 0
    for (const b of buckets) totalCalls += b.total
    const avgLatency = Math.round(avgLatencyMs)

    const maxCount = Math.max(1, ...buckets.map(b => b.total))

    // Find peak hour
    let peakIdx = HOURS - 1
    let peakVal = 0
    buckets.forEach((b, i) => {
      if (b.total > peakVal) { peakVal = b.total; peakIdx = i }
    })
    const peakHoursAgo = HOURS - 1 - peakIdx
    const peakStart = new Date(now - peakHoursAgo * bucketSize)
    const peakEnd = new Date(peakStart.getTime() + bucketSize)
    const pad = (n: number) => n.toString().padStart(2, '0')
    const peakHour = peakVal > 0
      ? `${pad(peakStart.getHours())}:00-${pad(peakEnd.getHours())}:00`
      : ''

    // Generate hour labels
    const labels: string[] = []
    for (let i = 0; i < HOURS; i++) {
      const hoursAgo = HOURS - 1 - i
      const d = new Date(now - hoursAgo * bucketSize)
      labels.push(d.getHours().toString().padStart(2, '0'))
    }

    return { buckets, maxCount, labels, totalCalls, avgLatency, peakHour }
  }, [hourlyActivity, avgLatencyMs])

  const svgHeight = 70
  const svgWidth = 400
  const leftPad = 18
  const rightPad = 4
  const chartWidth = svgWidth - leftPad - rightPad
  const barWidth = (chartWidth - (HOURS - 1) * BAR_GAP) / HOURS
  const chartTop = 4
  const chartHeight = svgHeight - chartTop - 16

  // Y-axis tick values
  const yTicks = maxCount <= 5
    ? Array.from({ length: maxCount + 1 }, (_, i) => i)
    : [0, Math.round(maxCount / 2), maxCount]

  if (totalCalls === 0) {
    return (
      <div style={{
        textAlign: 'center', padding: '24px 16px', margin: '8px 0',
        border: '1px dashed var(--border)', borderRadius: 'var(--radius)',
      }}>
        <div style={{ fontSize: '12px', color: 'var(--muted)' }}>No tool calls yet — system events excluded</div>
      </div>
    )
  }

  return (
    <div>
      {/* Stat header */}
      <div style={{
        fontSize: '13px', color: 'var(--muted)', marginBottom: '6px',
        display: 'flex', gap: '12px',
      }}>
        <span>
          <strong style={{ color: 'var(--text)' }}>{totalCalls}</strong> tool calls
        </span>
        <span>
          <strong style={{ color: 'var(--text)' }}>{avgLatency}ms</strong> avg
        </span>
        {peakHour && (
          <span>
            Peak: <strong style={{ color: 'var(--text)' }}>{peakHour}</strong>
          </span>
        )}
      </div>

      <svg viewBox={`0 0 ${svgWidth} ${svgHeight}`} style={{ display: 'block', width: '100%' }}>
        {/* Left axis line */}
        <line x1={leftPad} x2={leftPad} y1={chartTop} y2={chartTop + chartHeight}
          stroke="var(--border)" strokeWidth={0.5}
        />

        {/* Y-axis labels + grid lines */}
        {yTicks.map(val => {
          const y = chartTop + chartHeight * (1 - val / maxCount)
          return (
            <g key={val}>
              <text x={leftPad - 4} y={y + 3}
                textAnchor="end" fill="var(--muted)"
                style={{ fontSize: '6px', fontFamily: MONO }}
              >
                {val}
              </text>
              <line x1={leftPad} x2={svgWidth - rightPad} y1={y} y2={y}
                stroke="var(--border)" strokeWidth={0.5} strokeDasharray="2,2"
              />
            </g>
          )
        })}

        {/* Bars */}
        {buckets.map((b, i) => {
          const x = leftPad + i * (barWidth + BAR_GAP)
          const h = (b.total / maxCount) * chartHeight
          const errH = (b.errors / maxCount) * chartHeight
          const y = chartTop + chartHeight - h

          return (
            <g key={i}>
              {b.total > 0 && (
                <rect x={x} y={y} width={barWidth} height={h}
                  rx={1} fill="var(--accent)" opacity={0.7}
                />
              )}
              {b.errors > 0 && (
                <rect x={x} y={chartTop + chartHeight - errH}
                  width={barWidth} height={errH}
                  rx={1} fill="var(--red)" opacity={0.8}
                />
              )}
            </g>
          )
        })}

        {/* Hour labels */}
        {labels.map((label, i) => {
          if (i % 4 !== 0 && i !== HOURS - 1) return null
          const x = leftPad + i * (barWidth + BAR_GAP) + barWidth / 2
          const displayLabel = i === HOURS - 1 ? 'now' : label
          return (
            <text key={i} x={x} y={svgHeight - 2}
              textAnchor="middle" fill="var(--muted)"
              style={{ fontSize: '6px', fontFamily: MONO }}
            >
              {displayLabel}
            </text>
          )
        })}
        {/* "Now" indicator */}
        <line
          x1={leftPad + (HOURS - 1) * (barWidth + BAR_GAP) + barWidth}
          x2={leftPad + (HOURS - 1) * (barWidth + BAR_GAP) + barWidth}
          y1={chartTop} y2={chartTop + chartHeight}
          stroke="var(--accent)" strokeWidth={0.75} opacity={0.5}
        />
      </svg>

      {/* Legend */}
      <div style={{
        display: 'flex', justifyContent: 'space-between', alignItems: 'center',
        marginTop: '4px', fontSize: '11px', color: 'var(--muted)',
        fontStyle: 'italic',
      }}>
        <span>Excludes system events &amp; tools/list</span>
        <div style={{ display: 'flex', gap: '10px', fontStyle: 'normal' }}>
          <LegendItem color="var(--accent)" label="Calls" />
          <LegendItem color="var(--red)" label="Errors" />
          <LegendItem color="var(--accent)" label="Now" line />
        </div>
      </div>
    </div>
  )
}

function LegendItem({ color, label, line }: { color: string; label: string; line?: boolean }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: '3px', fontSize: '11px', color: 'var(--muted)' }}>
      {line ? (
        <div style={{ width: '10px', height: '2px', background: color, borderRadius: '1px' }} />
      ) : (
        <div style={{ width: '8px', height: '8px', borderRadius: '1px', background: color, opacity: 0.7 }} />
      )}
      {label}
    </div>
  )
}
