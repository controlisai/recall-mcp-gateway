import { create } from 'zustand'
import { fetchAuditStats, type AuditStatsResponse, type TopAgentStat, type TopToolStat, type HourlyBucket } from '@/lib/api'

export type Period = '24h'

const PERIOD_MS = 24 * 60 * 60 * 1000

interface StatsState {
  period: Period
  loading: boolean
  totalCalls: number
  blockedCount: number
  signalCounts: Record<string, number>
  topAgents: TopAgentStat[]
  topTools: TopToolStat[]
  avgLatencyMs: number
  uniqueTools: number
  uniqueAgents: number
  errorCount: number
  hourlyActivity: HourlyBucket[]
  lastFetched: number
  refresh: () => Promise<void>
}

let refreshTimer: ReturnType<typeof setInterval> | null = null

export const useStatsStore = create<StatsState>((set) => ({
  period: '24h',
  loading: false,
  totalCalls: 0,
  blockedCount: 0,
  signalCounts: {},
  topAgents: [],
  topTools: [],
  avgLatencyMs: 0,
  uniqueTools: 0,
  uniqueAgents: 0,
  errorCount: 0,
  hourlyActivity: [],
  lastFetched: 0,

  refresh: async () => {
    set({ loading: true })
    try {
      const from = new Date(Date.now() - PERIOD_MS).toISOString()
      const data: AuditStatsResponse = await fetchAuditStats(from)
      set({
        totalCalls: data.totalCount,
        blockedCount: data.blockedCount,
        signalCounts: data.signalCounts || {},
        topAgents: data.topAgents || [],
        topTools: data.topTools || [],
        avgLatencyMs: data.avgLatencyMs,
        uniqueTools: data.uniqueTools,
        uniqueAgents: data.uniqueAgents,
        errorCount: data.errorCount,
        hourlyActivity: data.hourlyActivity || [],
        lastFetched: Date.now(),
        loading: false,
      })
    } catch {
      set({ loading: false })
    }
  },
}))

/** Start auto-refresh interval (60s). Call once at app mount. */
export function startStatsRefresh() {
  if (refreshTimer) return
  useStatsStore.getState().refresh()
  refreshTimer = setInterval(() => {
    useStatsStore.getState().refresh()
  }, 60_000)
}

/** Stop auto-refresh. */
export function stopStatsRefresh() {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}
