import { useState, useEffect } from 'react'
import * as Tabs from '@radix-ui/react-tabs'
import { TopBar } from './TopBar'
import { StatsStrip } from './StatsStrip'
import { DiscoverTab } from './DiscoverTab'
import { ControlTab } from './ControlTab'
import { UnderstandTab } from './UnderstandTab'
import { SettingsTab } from './SettingsTab'
import { DocsTab } from './DocsTab'
import type { TabId } from '@/lib/types'

function getHashTab(): TabId {
  const hash = window.location.hash.slice(1)
  const valid: TabId[] = ['discover', 'understand', 'control', 'docs', 'settings']
  return valid.includes(hash as TabId) ? (hash as TabId) : 'discover'
}

export function Shell() {
  const [tab, setTab] = useState<TabId>(getHashTab)

  useEffect(() => {
    const onHash = () => setTab(getHashTab())
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])

  const onTabChange = (value: string) => {
    const t = value as TabId
    setTab(t)
    window.location.hash = t
  }

  return (
    <Tabs.Root value={tab} onValueChange={onTabChange} style={{ display: 'flex', flexDirection: 'column', height: '100vh' }}>
      <div style={{ flexShrink: 0, height: '7px', background: 'var(--border)', boxShadow: '0 2px 4px rgba(0,0,0,0.15)' }} />
      <TopBar tab={tab} />
      <StatsStrip />
      <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', background: 'var(--surface)' }}>
        <div style={{ flex: 1, minHeight: 0, overflow: 'hidden', position: 'relative' }}>
          <Tabs.Content value="discover" style={{ height: '100%' }}>
            <DiscoverTab />
          </Tabs.Content>
          <Tabs.Content value="understand" style={{ height: '100%' }}>
            <UnderstandTab />
          </Tabs.Content>
          <Tabs.Content value="control" style={{ height: '100%' }}>
            <ControlTab />
          </Tabs.Content>
          <Tabs.Content value="docs" style={{ height: '100%' }}>
            <DocsTab />
          </Tabs.Content>
          <Tabs.Content value="settings" style={{ height: '100%' }}>
            <SettingsTab />
          </Tabs.Content>
          {/* Overflow fade overlay */}
          <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, height: '40px', background: 'linear-gradient(transparent, var(--surface))', pointerEvents: 'none', zIndex: 2, borderBottom: '1px solid var(--border)' }} />
        </div>
        {/* Bottom grey bar */}
        <div style={{ flexShrink: 0, height: '7px', background: 'var(--border)', boxShadow: '0 -2px 4px rgba(0,0,0,0.15)' }} />
      </div>
    </Tabs.Root>
  )
}
