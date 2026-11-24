import { memo, useState } from 'react'
import type { Entry } from '@/lib/types'
import { FeedDetail } from './FeedDetail'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

function edgeColor(entry: Entry): string {
  if (entry.isError) return 'var(--amber)'
  switch (entry.decision) {
    case 'block': return 'var(--red)'
    case 'rate_limited': return 'var(--amber)'
    case 'pending_approval': case 'pending_consent': return 'var(--purple)'
    default: return 'var(--green)'
  }
}

function dotColor(entry: Entry): string {
  if (entry.isError) return 'var(--amber)'
  switch (entry.decision) {
    case 'block': return 'var(--red)'
    case 'rate_limited': return 'var(--amber)'
    case 'pending_approval': case 'pending_consent': return 'var(--purple)'
    default: return 'var(--green)'
  }
}

// Shorten classification labels to match vanilla dashboard (READ not READ_ONLY)
const classLabels: Record<string, string> = {
  read_only: 'READ', READ_ONLY: 'READ',
  destructive: 'DESTR', DESTRUCTIVE: 'DESTR',
  read_write: 'R/W', READ_WRITE: 'R/W',
  critical: 'CRIT', CRITICAL: 'CRIT',
  high: 'HIGH', HIGH: 'HIGH',
  medium: 'MED', MEDIUM: 'MED',
  low: 'LOW', LOW: 'LOW',
}

function classificationBadge(classification: string | undefined) {
  if (!classification || classification === 'unknown') return null
  const lc = classification.toLowerCase()
  const colors: Record<string, { bg: string; color: string }> = {
    critical: { bg: 'rgba(239,68,68,0.15)', color: 'var(--red)' },
    high: { bg: 'rgba(245,158,11,0.15)', color: 'var(--amber)' },
    medium: { bg: 'rgba(59,130,246,0.1)', color: 'var(--accent)' },
    low: { bg: 'rgba(255,255,255,0.06)', color: 'var(--muted)' },
    read_only: { bg: 'rgba(34,197,94,0.15)', color: 'var(--green)' },
    read_write: { bg: 'rgba(245,158,11,0.15)', color: 'var(--amber)' },
    destructive: { bg: 'rgba(239,68,68,0.15)', color: 'var(--red)' },
  }
  const c = colors[lc] || colors.low
  const label = classLabels[classification] || classification.toUpperCase()
  return (
    <span style={{
      fontSize: '10px', fontWeight: 600, padding: '1px 5px', borderRadius: '3px',
      background: c.bg, color: c.color, marginLeft: '6px',
      fontFamily: MONO, letterSpacing: '0.02em',
    }}>
      {label}
    </span>
  )
}

function decisionBadge(entry: Entry) {
  if (entry.decision === 'allow' && !entry.isError && !entry.consentPhase) return null
  const label = entry.decision === 'rate_limited' ? 'rate limited' : entry.decision
  const color = entry.decision === 'block' ? 'var(--red)'
    : entry.decision === 'rate_limited' ? 'var(--amber)'
    : entry.decision === 'pending_approval' || entry.decision === 'pending_consent' ? 'var(--purple)'
    : entry.isError ? 'var(--amber)'
    : 'var(--muted)'

  const consentColor = entry.consentPhase === 'granted' ? 'var(--green)'
    : entry.consentPhase === 'denied' ? 'var(--red)'
    : 'var(--purple)'

  return (
    <>
      <span style={{
        fontSize: '10px', fontWeight: 600, padding: '1px 5px', borderRadius: '3px',
        background: `color-mix(in srgb, ${color} 15%, transparent)`,
        color, marginLeft: '6px', textTransform: 'uppercase',
        fontFamily: MONO,
      }}>
        {entry.isError ? 'error' : label}
      </span>
      {entry.consentPhase && (
        <span style={{
          fontSize: '10px', fontWeight: 600, padding: '1px 5px', borderRadius: '3px',
          background: `color-mix(in srgb, ${consentColor} 12%, transparent)`,
          color: consentColor, marginLeft: '3px', textTransform: 'uppercase',
          fontFamily: MONO,
        }}>
          {entry.consentPhase === 'requested' ? '⟶ consent sent' : entry.consentPhase === 'granted' ? '✓ consented' : '✕ declined'}
        </span>
      )}
    </>
  )
}

function eraBadge(era: string | undefined) {
  if (!era) return null
  const color = era === 'modern' ? 'var(--green)' : 'var(--amber)'
  return (
    <span style={{
      fontSize: '10px', fontWeight: 500, padding: '1px 5px', borderRadius: '3px',
      background: `color-mix(in srgb, ${color} 15%, transparent)`, color,
      marginLeft: '6px', fontFamily: MONO,
    }}>
      {era}
    </span>
  )
}

function identityBadge(source: string | undefined) {
  if (!source) return null
  return (
    <span style={{
      fontSize: '10px', fontWeight: 500, padding: '1px 5px', borderRadius: '3px',
      background: 'rgba(255,255,255,0.06)', color: 'var(--muted)',
      marginLeft: '6px', fontFamily: MONO,
    }}>
      {source}
    </span>
  )
}

function SignalChips({ signals }: { signals: Entry['signals'] }) {
  if (!signals || signals.length === 0) return null
  const colorMap: Record<string, { bg: string; color: string }> = {
    pii: { bg: 'rgba(245,158,11,0.10)', color: 'var(--amber)' },
    credential: { bg: 'rgba(239,68,68,0.12)', color: 'var(--red)' },
    destructive: { bg: 'rgba(245,158,11,0.10)', color: 'var(--amber)' },
    sequence: { bg: 'rgba(168,85,247,0.12)', color: 'var(--purple)' },
  }
  const severityColors: Record<string, { bg: string; color: string }> = {
    critical: { bg: 'rgba(239,68,68,0.15)', color: 'var(--red)' },
    high: { bg: 'rgba(245,158,11,0.15)', color: 'var(--amber)' },
    warning: { bg: 'rgba(245,158,11,0.15)', color: 'var(--amber)' },
    info: { bg: 'rgba(59,130,246,0.1)', color: 'var(--accent)' },
  }
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', marginTop: '3px' }}>
      {signals.map((s, i) => {
        const c = colorMap[s.category] || severityColors[s.severity] || severityColors.info
        return (
          <span key={i} style={{
            display: 'inline-flex', alignItems: 'center', gap: '3px',
            fontSize: '11px', fontWeight: 500, padding: '1px 6px', borderRadius: '3px',
            fontFamily: MONO, background: c.bg, color: c.color,
          }}>
            {s.name}
          </span>
        )
      })}
    </div>
  )
}

function formatTime(ts: string): string {
  try {
    const d = new Date(ts)
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch {
    return ts
  }
}

interface FeedRowProps {
  entry: Entry
  classification?: string
  isFlowChild?: boolean
  showSensitive?: boolean
  onAgentClick?: (agentId: string) => void
}

function navigateToRule(tool: string, server: string, action: string, agent?: string) {
  sessionStorage.setItem('prefillRule', JSON.stringify({ tool, server, action, reason: '', agent }))
  window.location.hash = 'control'
}

export const FeedRow = memo(function FeedRow({ entry, classification, isFlowChild, showSensitive, onAgentClick }: FeedRowProps) {
  const [expanded, setExpanded] = useState(false)

  const isSystem = entry.agentId === 'system' || entry.tool === '(server-lifecycle)'
  const argKeys = entry.arguments ? Object.keys(entry.arguments) : []
  const argSummary = argKeys.length > 0
    ? argKeys.slice(0, 3).map(k => {
        const v = entry.arguments![k]
        return `${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`
      }).join(', ') + (argKeys.length > 3 ? ` +${argKeys.length - 3}` : '')
    : null

  return (
    <div
      className="feed-row-hover"
      onClick={() => setExpanded(!expanded)}
      style={{
        position: 'relative',
        display: 'grid',
        gridTemplateColumns: '10px 1fr auto',
        gap: '8px',
        alignItems: 'start',
        padding: isFlowChild ? '6px 12px 6px 24px' : '8px 12px',
        borderBottom: '1px solid var(--border)',
        cursor: 'pointer',
        transition: 'background 0.1s',
      }}
      onMouseEnter={e => { (e.currentTarget as HTMLDivElement).style.background = 'rgba(255,255,255,0.02)' }}
      onMouseLeave={e => { (e.currentTarget as HTMLDivElement).style.background = 'transparent' }}
    >
      {/* Edge bar */}
      <div style={{
        position: 'absolute', left: 0, top: 0, bottom: 0, width: '3px',
        borderRadius: '0 2px 2px 0', background: edgeColor(entry),
      }} />

      {/* Status dot */}
      <div style={{ paddingTop: '5px' }}>
        <div style={{
          width: '6px', height: '6px', borderRadius: '50%',
          background: dotColor(entry),
        }} />
      </div>

      {/* Main content */}
      <div style={{ minWidth: 0 }}>
        {/* Tool line */}
        <div style={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: '2px' }}>
          <span style={{
            fontSize: '13px', fontWeight: 600, fontFamily: MONO,
            color: isSystem ? 'var(--muted)' : 'var(--text)',
          }}>
            {entry.tool}
          </span>
          {classificationBadge(classification)}
          {eraBadge(entry.protocolEra)}
          {identityBadge(entry.identitySource)}
          {decisionBadge(entry)}
        </div>

        {/* Agent / server line */}
        <div style={{
          fontSize: '12px', color: 'var(--muted)', marginTop: '2px',
          display: 'flex', flexWrap: 'wrap', gap: '4px', alignItems: 'center',
        }}>
          <span onClick={e => { e.stopPropagation(); onAgentClick?.(entry.agentId) }}
            style={{ cursor: onAgentClick ? 'pointer' : 'default' }}
            onMouseEnter={e => { if (onAgentClick) (e.currentTarget as HTMLElement).style.color = 'var(--accent)' }}
            onMouseLeave={e => { if (onAgentClick) (e.currentTarget as HTMLElement).style.color = 'var(--muted)' }}
          >{entry.agentId}</span>
          {entry.userId && entry.userId !== entry.agentId && (
            <span style={{ opacity: 0.7 }}>({entry.userId})</span>
          )}
          {entry.actingFor && (
            <span style={{ opacity: 0.7 }}>for {entry.actingFor}</span>
          )}
          <span style={{ color: 'var(--border)' }}>/</span>
          <span style={{ color: 'var(--accent)' }}>{entry.server}</span>
          {entry.orgId && (
            <>
              <span style={{ color: 'var(--border)' }}>/</span>
              <span>{entry.orgId}</span>
            </>
          )}
          {entry.traceId && (
            <span style={{
              fontSize: '10px', fontFamily: MONO, opacity: 0.5,
              marginLeft: '4px',
            }}>
              {entry.traceId.slice(0, 8)}
            </span>
          )}
        </div>

        {/* Argument summary */}
        {argSummary && (
          <div style={{
            fontSize: '11px', color: 'var(--muted)', fontFamily: MONO,
            marginTop: '2px', opacity: 0.7,
          }}>
            {argSummary}
          </div>
        )}

        {/* Error message */}
        {entry.isError && entry.errorMessage && (
          <div style={{
            fontSize: '11px', color: 'var(--red)', marginTop: '2px',
            fontFamily: MONO,
          }}>
            {entry.errorMessage.length > 80
              ? entry.errorMessage.slice(0, 80) + '...'
              : entry.errorMessage}
          </div>
        )}

        {/* Signal chips */}
        <SignalChips signals={entry.signals} />

        {/* Expanded detail */}
        {expanded && <FeedDetail entry={entry} showSensitive={showSensitive} />}
      </div>

      {/* Right column: latency + time */}
      <div style={{ textAlign: 'right', flexShrink: 0 }}>
        <div style={{
          fontSize: '12px', fontFamily: MONO, color: 'var(--text)',
          fontWeight: 500,
        }}>
          {`${entry.latencyMs || 0}ms`}
        </div>
        <div style={{
          fontSize: '11px', fontFamily: MONO, color: 'var(--muted)',
          marginTop: '1px',
        }}>
          {formatTime(entry.timestamp)}
        </div>
        {entry.costPerCall != null && entry.costPerCall > 0 && (
          <div style={{
            fontSize: '10px', fontFamily: MONO, color: 'var(--muted)',
            marginTop: '1px',
          }}>
            ${entry.costPerCall.toFixed(4)}
          </div>
        )}
      </div>

      {/* Hover actions */}
      <HoverActions entry={entry} />
    </div>
  )
})

function HoverActions({ entry }: { entry: Entry }) {
  if (entry.agentId === 'system') return null

  return (
    <div
      className="feed-hover-actions"
      onClick={e => e.stopPropagation()}
      style={{
        display: 'none', gap: '4px', position: 'absolute', right: '8px', top: '6px',
        background: 'var(--surface)', padding: '2px 4px', borderRadius: '4px',
        border: '1px solid var(--border)',
        boxShadow: '0 2px 8px rgba(0,0,0,0.2)', zIndex: 5,
      }}
    >
      <HoverBtn label="block" danger onClick={() => navigateToRule(entry.tool, entry.server, 'block', entry.agentId)} />
      <HoverBtn label="rate limit" onClick={() => navigateToRule(entry.tool, entry.server, 'rate_limit', entry.agentId)} />
      <HoverBtn label="inspect" onClick={() => navigateToRule(entry.tool, entry.server, 'require_approval', entry.agentId)} />
    </div>
  )
}

function HoverBtn({ label, danger, onClick }: { label: string; danger?: boolean; onClick?: () => void }) {
  return (
    <button
      onClick={onClick}
      style={{
        fontSize: '10px', fontWeight: 500, color: 'var(--muted)',
        background: 'transparent', border: '1px solid var(--border)',
        padding: '2px 7px', borderRadius: '3px', cursor: 'pointer',
        whiteSpace: 'nowrap', transition: 'all 0.1s',
      }}
      onMouseEnter={e => {
        const el = e.currentTarget
        el.style.color = danger ? 'var(--red)' : 'var(--text)'
        el.style.borderColor = danger ? 'var(--red)' : 'var(--muted)'
      }}
      onMouseLeave={e => {
        const el = e.currentTarget
        el.style.color = 'var(--muted)'
        el.style.borderColor = 'var(--border)'
      }}
    >
      {label}
    </button>
  )
}
