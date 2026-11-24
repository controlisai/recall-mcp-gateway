import { create } from 'zustand'
import type { Entry } from '@/lib/types'

const MAX_ENTRIES = 500

interface SignalCounts {
  pii: number
  credential: number
  destructive: number
  sequence: number
}

interface FeedState {
  entries: Entry[]
  totalCount: number
  blockedCount: number
  signalCounts: SignalCounts
  toolCounts: Record<string, number>
  agentCounts: Record<string, number>
  totalLatency: number
  oldestTimestamp: string
  newestTimestamp: string
  addEntry: (entry: Entry) => void
  loadRecent: (entries: Entry[]) => void
}

function countSignals(signals: Entry['signals']): Partial<SignalCounts> {
  const counts: Partial<SignalCounts> = {}
  for (const s of signals || []) {
    const cat = s.category as keyof SignalCounts
    if (cat in ({ pii: 1, credential: 1, destructive: 1, sequence: 1 })) {
      counts[cat] = (counts[cat] || 0) + 1
    }
  }
  return counts
}

export const useFeedStore = create<FeedState>((set) => ({
  entries: [],
  totalCount: 0,
  blockedCount: 0,
  signalCounts: { pii: 0, credential: 0, destructive: 0, sequence: 0 },
  toolCounts: {},
  agentCounts: {},
  totalLatency: 0,
  oldestTimestamp: '',
  newestTimestamp: '',

  addEntry: (entry) =>
    set((state) => {
      const entries = [entry, ...state.entries].slice(0, MAX_ENTRIES)
      const totalCount = state.totalCount + 1
      const blocked = entry.decision === 'block' || entry.decision === 'rate_limited'
      const blockedCount = state.blockedCount + (blocked ? 1 : 0)
      const sc = { ...state.signalCounts }
      for (const [k, v] of Object.entries(countSignals(entry.signals))) {
        sc[k as keyof SignalCounts] += v as number
      }
      const toolCounts = { ...state.toolCounts }
      if (entry.tool) toolCounts[entry.tool] = (toolCounts[entry.tool] || 0) + 1
      const agentCounts = { ...state.agentCounts }
      if (entry.agentId) agentCounts[entry.agentId] = (agentCounts[entry.agentId] || 0) + 1
      const totalLatency = state.totalLatency + (entry.latencyMs || 0)
      const newestTimestamp = entry.timestamp || state.newestTimestamp
      const oldestTimestamp = state.oldestTimestamp || entry.timestamp
      return { entries, totalCount, blockedCount, signalCounts: sc, toolCounts, agentCounts, totalLatency, oldestTimestamp, newestTimestamp }
    }),

  loadRecent: (recent) =>
    set(() => {
      const entries = (recent || []).slice(0, MAX_ENTRIES)
      let blockedCount = 0
      let totalLatency = 0
      const sc: SignalCounts = { pii: 0, credential: 0, destructive: 0, sequence: 0 }
      const toolCounts: Record<string, number> = {}
      const agentCounts: Record<string, number> = {}
      for (const e of entries) {
        if (e.decision === 'block' || e.decision === 'rate_limited') blockedCount++
        totalLatency += e.latencyMs || 0
        for (const [k, v] of Object.entries(countSignals(e.signals))) {
          sc[k as keyof SignalCounts] += v as number
        }
        if (e.tool) toolCounts[e.tool] = (toolCounts[e.tool] || 0) + 1
        if (e.agentId) agentCounts[e.agentId] = (agentCounts[e.agentId] || 0) + 1
      }
      const newestTimestamp = entries.length > 0 ? entries[0].timestamp : ''
      const oldestTimestamp = entries.length > 0 ? entries[entries.length - 1].timestamp : ''
      return { entries, totalCount: entries.length, blockedCount, signalCounts: sc, toolCounts, agentCounts, totalLatency, oldestTimestamp, newestTimestamp }
    }),
}))
