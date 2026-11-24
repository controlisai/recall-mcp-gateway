import type { Entry } from '@/lib/types'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

// Client-side PII redaction matching vanilla dashboard behavior
function redactPII(text: string): string {
  return text
    // Emails: alice@example.com → ***@***.***
    .replace(/[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/g, '***@***.***')
    // Quoted names: "Alice Chen" → "*** ***"
    .replace(/"([A-Z][a-z]+ [A-Z][a-z]+)"/g, '"*** ***"')
    // Phone numbers: 555-1234 or (555) 123-4567
    .replace(/\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}/g, '***-***-****')
    // SSN patterns: 123-45-6789
    .replace(/\d{3}-\d{2}-\d{4}/g, '***-**-****')
}

const labelStyle: React.CSSProperties = {
  fontSize: '12px', color: 'var(--muted)', textTransform: 'uppercase',
  letterSpacing: '0.04em', marginBottom: '3px', fontWeight: 600,
}

const contentStyle: React.CSSProperties = {
  fontSize: '12px', fontFamily: MONO, color: 'var(--text)',
  whiteSpace: 'pre-wrap', wordBreak: 'break-all', lineHeight: 1.5,
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: '8px' }}>
      <div style={labelStyle}>{label}</div>
      <div style={contentStyle}>{children}</div>
    </div>
  )
}

function formatArgs(args: Record<string, unknown>, redact?: boolean): React.ReactNode {
  return Object.entries(args).map(([k, v]) => {
    const raw = typeof v === 'string' ? v : JSON.stringify(v)
    const display = redact ? redactPII(raw) : raw
    return (
      <div key={k}>
        <span style={{ color: 'var(--accent)' }}>{k}</span>
        <span style={{ color: 'var(--muted)' }}>: </span>
        <span style={{ color: '#94a3b8' }}>{display}</span>
      </div>
    )
  })
}

const actionBtnBase: React.CSSProperties = {
  fontSize: '12px', fontWeight: 500, padding: '4px 12px',
  borderRadius: '4px', cursor: 'pointer', border: '1px solid',
  fontFamily: 'inherit',
}

export function FeedDetail({ entry, showSensitive }: { entry: Entry; showSensitive?: boolean }) {
  return (
    <div
      onClick={e => e.stopPropagation()}
      style={{
        marginTop: '6px', padding: '10px 12px', borderRadius: '6px',
        border: '1px solid var(--border)', background: 'transparent',
      }}
    >
      {/* Arguments */}
      {entry.arguments && Object.keys(entry.arguments).length > 0 && (
        <Section label="Arguments">
          {formatArgs(entry.arguments, !showSensitive)}
        </Section>
      )}

      {/* Result preview */}
      {entry.result_preview && (
        <Section label="Result Preview">
          <div style={{
            padding: '6px 10px', borderRadius: '4px',
            background: 'rgba(255,255,255,0.03)',
            border: '1px solid var(--border)',
          }}>
            {(() => {
              const raw = entry.result_preview.length > 500
                ? entry.result_preview.slice(0, 500) + '...'
                : entry.result_preview
              return showSensitive ? raw : redactPII(raw)
            })()}
          </div>
        </Section>
      )}

      {/* Error */}
      {entry.isError && entry.errorMessage && (
        <Section label="Error">
          <span style={{ color: 'var(--red)' }}>{entry.errorMessage}</span>
          {entry.errorCategory && (
            <span style={{ color: 'var(--muted)', marginLeft: '8px' }}>
              [{entry.errorCategory}]
            </span>
          )}
        </Section>
      )}

      {/* Metadata line (compact, single-line) */}
      <div style={{
        fontSize: '11px', fontFamily: MONO, color: 'var(--muted)',
        marginBottom: '8px',
      }}>
        ID: {entry.requestId}
        {entry.mode && <> · Mode: {entry.mode}</>}
        {entry.identitySource && <> · Identity: {entry.identitySource}</>}
        {entry.protocolEra && <> · Era: {entry.protocolEra}</>}
        {entry.traceId && <> · Trace: {entry.traceId.slice(0, 16)}</>}
      </div>

      {/* Governance section */}
      <Section label="Governance">
        {entry.matchedRule ? (
          <div>
            <span style={{ color: 'var(--green)' }}>●</span>{' '}
            Rule matched: {entry.matchedRule}
            {entry.matchedRuleIndex >= 0 && ` (#${entry.matchedRuleIndex})`}
            {entry.decisionReason && (
              <div style={{ color: 'var(--muted)', marginTop: '2px' }}>
                {entry.decisionReason}
              </div>
            )}
          </div>
        ) : (
          <div>
            <span style={{ color: 'var(--muted)' }}>○</span>{' '}
            No rule matched — default {entry.decision || 'allow'}
          </div>
        )}
      </Section>

      {/* Signal details */}
      {entry.signals && entry.signals.length > 0 && (
        <Section label="Signals">
          {entry.signals.map((s, i) => (
            <div key={i} style={{ marginBottom: '2px' }}>
              <span style={{ color: s.severity === 'critical' ? 'var(--red)' : 'var(--amber)' }}>
                {s.name}
              </span>
              <span style={{ color: 'var(--muted)' }}> [{s.severity}] </span>
              <span style={{ color: '#94a3b8' }}>{s.message}</span>
            </div>
          ))}
        </Section>
      )}

      {/* Action buttons */}
      {entry.agentId !== 'system' && (
        <div style={{ display: 'flex', gap: '8px', marginTop: '4px' }}>
          <button onClick={() => { sessionStorage.setItem('prefillRule', JSON.stringify({ tool: entry.tool, server: entry.server, action: 'block', agent: entry.agentId })); window.location.hash = 'control' }} style={{
            ...actionBtnBase,
            color: 'var(--red)', borderColor: 'var(--red)',
            background: 'transparent',
          }}>
            Add block rule
          </button>
          <button onClick={() => { sessionStorage.setItem('prefillRule', JSON.stringify({ tool: entry.tool, server: entry.server, action: 'require_approval', agent: entry.agentId })); window.location.hash = 'control' }} style={{
            ...actionBtnBase,
            color: 'var(--text)', borderColor: 'var(--border)',
            background: 'transparent',
          }}>
            Add approval rule
          </button>
          <button onClick={() => { sessionStorage.setItem('prefillRule', JSON.stringify({ tool: entry.tool, server: entry.server, action: 'rate_limit', agent: entry.agentId })); window.location.hash = 'control' }} style={{
            ...actionBtnBase,
            color: 'var(--text)', borderColor: 'var(--border)',
            background: 'transparent',
          }}>
            Add rate limit
          </button>
        </div>
      )}
    </div>
  )
}
