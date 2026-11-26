import { useEffect, useRef } from 'react'
import { useGatewayStore } from '@/stores/gateway-store'
import { useFeedStore } from '@/stores/feed-store'
import type { SSEMessage } from '@/lib/types'

/** Close and reopen the SSE connection to get a fresh init message. */
export function reconnectSSE() {
  // Signal the hook to reconnect by dispatching a custom event
  window.dispatchEvent(new CustomEvent('sse-reconnect'))
}

export function useSSE() {
  const retryRef = useRef(0)
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    let mounted = true

    // Check if auth is needed — if so, verify we have a session before connecting SSE
    async function checkAuthAndConnect() {
      try {
        const resp = await fetch('/dashboard/auth-status', { credentials: 'same-origin' })
        const data = await resp.json()
        if (data.authEnabled) {
          // Test if we have a valid session by trying SSE preflight
          const testResp = await fetch('/ws/audit', { credentials: 'same-origin' })
          const ct = testResp.headers.get('content-type') || ''
          if (!ct.includes('text/event-stream')) {
            // SSE returned login page, not event stream — redirect to login
            window.location.href = '/dashboard/login?redirect=' + encodeURIComponent(window.location.pathname + window.location.hash)
            return
          }
        }
      } catch {
        // Auth check failed — proceed anyway
      }
      connect()
    }

    function connect() {
      if (!mounted) return
      const es = new EventSource('/ws/audit')
      esRef.current = es

      es.onopen = () => {
        retryRef.current = 0
        useGatewayStore.getState().setConnected(true)
      }

      es.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data) as SSEMessage
          if (data.type === 'init') {
            useGatewayStore.getState().setInit(data)
            useFeedStore.getState().loadRecent(data.recent)
          } else if (data.type === 'entry') {
            useFeedStore.getState().addEntry(data.entry)
          }
        } catch {
          // ignore malformed messages
        }
      }

      es.onerror = () => {
        es.close()
        esRef.current = null
        useGatewayStore.getState().setConnected(false)

        if (!mounted) return
        // After 3 failed retries, check if we need to login
        if (retryRef.current >= 3) {
          checkAuthAndConnect()
          return
        }
        const delay = Math.min(1000 * Math.pow(2, retryRef.current), 30000)
        retryRef.current++
        setTimeout(connect, delay)
      }
    }

    checkAuthAndConnect()

    // Listen for reconnect requests (e.g., after adding/removing a server)
    function handleReconnect() {
      esRef.current?.close()
      esRef.current = null
      retryRef.current = 0
      connect()
    }
    window.addEventListener('sse-reconnect', handleReconnect)

    return () => {
      mounted = false
      esRef.current?.close()
      esRef.current = null
      window.removeEventListener('sse-reconnect', handleReconnect)
    }
  }, [])
}
