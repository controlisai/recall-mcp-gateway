import { useState, useEffect } from 'react'
import { Server, ScrollText } from 'lucide-react'
import { ServersList } from './ServersList'
import { AgentsList } from './AgentsList'
import { RulesEditor } from './RulesEditor'

type SubTab = 'servers-agents' | 'rules'

export function ControlTab() {
  const [subTab, setSubTab] = useState<SubTab>(() => {
    if (sessionStorage.getItem('prefillRule')) return 'rules'
    return 'servers-agents'
  })

  // Watch for prefillRule changes (e.g., from tool popup on Servers tab)
  useEffect(() => {
    const check = () => {
      if (sessionStorage.getItem('prefillRule')) setSubTab('rules')
    }
    window.addEventListener('hashchange', check)
    window.addEventListener('focus', check)
    window.addEventListener('switchToRules', check)
    return () => {
      window.removeEventListener('hashchange', check)
      window.removeEventListener('focus', check)
      window.removeEventListener('switchToRules', check)
    }
  }, [])

  const tabBtnStyle = (active: boolean): React.CSSProperties => ({
    padding: '10px 16px',
    fontSize: '15px',
    fontWeight: 500,
    color: active ? 'var(--accent)' : 'var(--muted)',
    background: 'transparent',
    border: 'none',
    borderBottom: `2px solid ${active ? 'var(--accent)' : 'transparent'}`,
    cursor: 'pointer',
    transition: 'color 0.15s',
    outline: 'none',
  })

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* Sub-tab navigation */}
      <div style={{
        display: 'flex', gap: '0',
        borderBottom: '1px solid var(--border)',
        background: 'var(--bg)',
        padding: '0 20px',
        flexShrink: 0,
      }}>
        <button style={{ ...tabBtnStyle(subTab === 'servers-agents'), display: 'inline-flex', alignItems: 'center', gap: '5px' }}
          onClick={() => setSubTab('servers-agents')}>
          <Server size={14} strokeWidth={1.5} />Servers & Agents
        </button>
        <button style={{ ...tabBtnStyle(subTab === 'rules'), display: 'inline-flex', alignItems: 'center', gap: '5px' }}
          onClick={() => setSubTab('rules')}>
          <ScrollText size={14} strokeWidth={1.5} />Rules
        </button>
      </div>

      {/* Tab content */}
      <div style={{ flex: 1, minHeight: 0, overflow: 'hidden' }}>
        {subTab === 'servers-agents' ? (
          <div style={{
            display: 'grid', gridTemplateColumns: '1fr 1fr',
            height: '100%', minHeight: 0,
          }}>
            {/* Left: Servers */}
            <div style={{
              borderRight: '1px solid var(--border)',
              overflow: 'auto',
            }}>
              <ServersList />
            </div>
            {/* Right: Agents */}
            <div style={{ overflow: 'auto' }}>
              <AgentsList />
            </div>
          </div>
        ) : (
          <RulesEditor />
        )}
      </div>
    </div>
  )
}
