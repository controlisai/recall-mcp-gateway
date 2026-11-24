import { useState, useEffect } from 'react'
import { Check, ArrowRight, Server, Radio, Zap, Shield, Bot } from 'lucide-react'
import { useGatewayStore } from '@/stores/gateway-store'
import { useFeedStore } from '@/stores/feed-store'
import { fetchRulesConfig, fetchAgentConfigs } from '@/lib/api'

interface Step {
  id: string
  label: string
  doneLabel: string
  done: boolean
  action?: { label: string; hash: string }
  icon: React.ReactNode
}

export function OnboardingChecklist() {
  const servers = useGatewayStore(s => s.servers)
  const totalCount = useFeedStore(s => s.totalCount)
  const [hasRules, setHasRules] = useState(false)
  const [hasAgentToken, setHasAgentToken] = useState(false)

  useEffect(() => {
    fetchRulesConfig()
      .then(d => setHasRules((d.rules || []).length > 0))
      .catch(() => {})
    fetchAgentConfigs()
      .then(d => setHasAgentToken((d.agents || []).some(a => a.hasToken)))
      .catch(() => {})
  }, [])

  // Re-check when feed gets entries (rules/agents may have been created)
  useEffect(() => {
    if (totalCount > 0) {
      fetchRulesConfig()
        .then(d => setHasRules((d.rules || []).length > 0))
        .catch(() => {})
      fetchAgentConfigs()
        .then(d => setHasAgentToken((d.agents || []).some(a => a.hasToken)))
        .catch(() => {})
    }
  }, [totalCount])

  const steps: Step[] = [
    {
      id: 'proxy',
      label: 'Proxy running',
      doneLabel: 'Proxy running',
      done: true,
      icon: <Radio size={14} />,
    },
    {
      id: 'servers',
      label: 'MCP servers connected',
      doneLabel: 'MCP servers connected',
      done: servers.length > 0,
      icon: <Server size={14} />,
    },
    {
      id: 'calls',
      label: 'Make your first tool call',
      doneLabel: 'Tool calls received',
      done: totalCount > 0,
      action: { label: 'How to connect', hash: '#docs' },
      icon: <Zap size={14} />,
    },
    {
      id: 'rule',
      label: 'Add your first rule',
      doneLabel: 'Rules configured',
      done: hasRules,
      action: { label: 'Add rule', hash: '#control' },
      icon: <Shield size={14} />,
    },
    {
      id: 'agent',
      label: 'Create an agent with token',
      doneLabel: 'Agent with token created',
      done: hasAgentToken,
      action: { label: 'Create agent', hash: '#control' },
      icon: <Bot size={14} />,
    },
  ]

  const doneCount = steps.filter(s => s.done).length
  const allDone = doneCount === steps.length

  if (allDone) return null

  return (
    <div style={{ padding: '20px' }}>
      <div style={{
        border: '1px dashed var(--border)', borderRadius: 'var(--radius)',
        padding: '24px 20px',
      }}>
        <div style={{ textAlign: 'center', marginBottom: '20px' }}>
          <div style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text)', marginBottom: '4px' }}>
            Getting Started
          </div>
          <div style={{ fontSize: '12px', color: 'var(--muted)' }}>
            Complete these steps to start monitoring your AI agents
          </div>
          {/* Progress */}
          <div style={{
            display: 'flex', alignItems: 'center', gap: '8px',
            justifyContent: 'center', marginTop: '12px',
          }}>
            <div style={{
              width: '120px', height: '4px', borderRadius: '2px',
              background: 'var(--border)', overflow: 'hidden',
            }}>
              <div style={{
                width: `${(doneCount / steps.length) * 100}%`,
                height: '100%', borderRadius: '2px',
                background: 'var(--green)',
                transition: 'width 0.3s ease',
              }} />
            </div>
            <span style={{ fontSize: '11px', color: 'var(--muted)' }}>
              {doneCount}/{steps.length}
            </span>
          </div>
        </div>

        <div style={{ maxWidth: '340px', margin: '0 auto' }}>
          {steps.map(step => (
            <div key={step.id} style={{
              display: 'flex', alignItems: 'center', gap: '10px',
              padding: '8px 0', fontSize: '13px',
            }}>
              {/* Checkbox circle */}
              <div style={{
                width: '22px', height: '22px', borderRadius: '50%', flexShrink: 0,
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                background: step.done ? 'var(--green)' : 'transparent',
                border: step.done ? '2px solid var(--green)' : '2px solid var(--border)',
                color: step.done ? '#fff' : 'var(--muted)',
                transition: 'all 0.2s ease',
              }}>
                {step.done ? <Check size={12} strokeWidth={3} /> : step.icon}
              </div>

              {/* Label */}
              <div style={{
                flex: 1, color: step.done ? 'var(--muted)' : 'var(--text)',
                textDecoration: step.done ? 'line-through' : 'none',
              }}>
                {step.done ? step.doneLabel : step.label}
              </div>

              {/* Action link */}
              {!step.done && step.action && (
                <a
                  href={step.action.hash}
                  style={{
                    fontSize: '12px', color: 'var(--accent)', cursor: 'pointer',
                    textDecoration: 'none', display: 'inline-flex', alignItems: 'center', gap: '2px',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {step.action.label} <ArrowRight size={11} />
                </a>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
