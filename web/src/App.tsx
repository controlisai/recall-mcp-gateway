import { useEffect } from 'react'
import { useSSE } from '@/hooks/use-sse'
import { Shell } from '@/components/Shell'
import { startStatsRefresh, stopStatsRefresh } from '@/stores/stats-store'

export function App() {
  useSSE()
  useEffect(() => {
    startStatsRefresh()
    return () => stopStatsRefresh()
  }, [])
  return <Shell />
}
