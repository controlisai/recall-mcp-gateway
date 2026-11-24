import { useState, useEffect } from 'react'
import { useStatsStore } from '@/stores/stats-store'
import { useGatewayStore } from '@/stores/gateway-store'
import { fetchAgentConfigs, type AgentConfig } from '@/lib/api'

const MONO = "'JetBrains Mono', 'Cascadia Code', 'SF Mono', 'Fira Code', monospace"

interface TopAgentsProps {
  onAgentClick?: (agentId: string) => void
}

export function TopAgents({ onAgentClick }: TopAgentsProps) {
  const topAgents = useStatsStore(s => s.topAgents)
  const totalServers = useGatewayStore(s => s.servers.length)
  const [agentConfigs, setAgentConfigs] = useState<Record<string, AgentConfig>>({})

  useEffect(() => {
    fetchAgentConfigs()
      .then(d => {
        const map: Record<string, AgentConfig> = {}
        for (const a of (d.agents || [])) map[a.id] = a
        setAgentConfigs(map)
      })
      .catch(() => {})
  }, [])

  const agents = topAgents.slice(0, 8)
  const maxCount = agents.length > 0 ? agents[0].count : 1

  if (agents.length === 0) {
    return (
      <div style={{
        textAlign: 'center', padding: '24px 16px', margin: '8px 0',
        border: '1px dashed var(--border)', borderRadius: 'var(--radius)',
      }}>
        <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" strokeLinecap="round" style={{ color: 'var(--muted)', opacity: 0.3, marginBottom: '8px', display: 'block', margin: '0 auto 8px' }}>
          <path d="M16 21v-2a4 4 0 00-4-4H6a4 4 0 00-4 4v2" /><circle cx="9" cy="7" r="4" /><path d="M22 21v-2a4 4 0 00-3-3.87" /><path d="M16 3.13a4 4 0 010 7.75" />
        </svg>
        <div style={{ fontSize: '12px', color: 'var(--muted)' }}>Agents are warming up...</div>
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
      {agents.map(agent => {
        const barWidth = (agent.count / maxCount) * 100
        const errorWidth = agent.errors > 0 ? (agent.errors / maxCount) * 100 : 0
        const cfg = agentConfigs[agent.id]
        const archetype = cfg?.archetype || 'autonomous'
        const archetypeLabel = archetype === 'user_embedded' ? 'USER'
          : archetype === 'delegated' ? 'DELEG'
          : 'AUTO'
        const archetypeColor = archetype === 'user_embedded' ? 'var(--accent)'
          : archetype === 'delegated' ? 'var(--purple)'
          : 'var(--muted)'
        const serverAccessCount = (() => {
          if (!cfg) return 0
          const allowed = cfg.allowedServers || []
          if (cfg.serverAccessRestricted && allowed.length === 0) return 0
          if (!cfg.serverAccessRestricted && allowed.length === 0) return totalServers
          return allowed.length
        })()

        return (
          <div key={agent.id} onClick={() => onAgentClick?.(agent.id)} style={{
            cursor: 'pointer', padding: '4px 0', borderRadius: '4px',
          }}
          onMouseEnter={e => { (e.currentTarget as HTMLDivElement).style.background = 'rgba(255,255,255,0.04)' }}
          onMouseLeave={e => { (e.currentTarget as HTMLDivElement).style.background = 'transparent' }}
          >
            {/* Name row */}
            <div style={{
              display: 'flex', alignItems: 'center', justifyContent: 'space-between',
              marginBottom: '2px',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '6px', minWidth: 0 }}>
                <span style={{
                  fontSize: '12px', color: 'var(--text)', fontWeight: 400, fontFamily: MONO,
                  overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                }}>
                  {agent.id}
                </span>
                <span style={{
                  fontSize: '10px', fontWeight: 700, padding: '2px 5px',
                  borderRadius: '2px', fontFamily: MONO,
                  background: `color-mix(in srgb, ${archetypeColor} 15%, transparent)`,
                  color: archetypeColor, letterSpacing: '0.03em',
                }}>
                  {archetypeLabel}
                </span>
                <span style={{
                  fontSize: '12px', color: 'var(--muted)',
                }}>
                  {serverAccessCount} {serverAccessCount === 1 ? 'server' : 'servers'}
                </span>
              </div>
              <span style={{
                fontSize: '14px', fontWeight: 600, fontFamily: MONO,
                color: 'var(--text)', flexShrink: 0, marginLeft: '8px',
              }}>
                {agent.count}
              </span>
            </div>

            {/* Proportional bar */}
            <div style={{
              height: '4px', borderRadius: '2px',
              background: 'rgba(255,255,255,0.06)', position: 'relative',
              overflow: 'hidden',
            }}>
              <div style={{
                position: 'absolute', left: 0, top: 0, bottom: 0,
                width: `${barWidth}%`, borderRadius: '2px',
                background: 'var(--green)', transition: 'width 0.3s ease',
              }} />
              {errorWidth > 0 && (
                <div style={{
                  position: 'absolute', left: `${barWidth - errorWidth}%`, top: 0, bottom: 0,
                  width: `${errorWidth}%`, borderRadius: '2px',
                  background: 'var(--red)', transition: 'left 0.3s ease, width 0.3s ease',
                }} />
              )}
            </div>
          </div>
        )
      })}
    </div>
  )
}
