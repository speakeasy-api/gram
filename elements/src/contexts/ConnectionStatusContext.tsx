'use client'

import {
  createContext,
  useContext,
  useState,
  useCallback,
  useRef,
  useEffect,
  type ReactNode,
} from 'react'

export type ConnectionState = 'connected' | 'reconnecting' | 'disconnected'

interface ConnectionStatusContextValue {
  /** Current connection state */
  state: ConnectionState
  /** Number of reconnection attempts */
  retryCount: number
  /** Whether the browser reports being online */
  isOnline: boolean
  /** Mark connection as failed - will trigger reconnecting state */
  markDisconnected: () => void
  /** Mark connection as restored */
  markConnected: () => void
  /** Reset the connection state */
  reset: () => void
}

const ConnectionStatusContext =
  createContext<ConnectionStatusContextValue | null>(null)

interface ConnectionStatusProviderProps {
  children: ReactNode
}

export const ConnectionStatusProvider = ({
  children,
}: ConnectionStatusProviderProps) => {
  const [state, setState] = useState<ConnectionState>('connected')
  const [retryCount, setRetryCount] = useState(0)
  const [isOnline, setIsOnline] = useState(
    typeof navigator !== 'undefined' ? navigator.onLine : true
  )
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // Monitor browser online/offline status
  useEffect(() => {
    if (typeof window === 'undefined') return

    const handleOnline = () => {
      setIsOnline(true)
      // When coming back online, move from disconnected to reconnecting
      setState((current) =>
        current === 'disconnected' ? 'reconnecting' : current
      )
    }

    const handleOffline = () => {
      setIsOnline(false)
      // Immediately mark as disconnected when browser goes offline
      setState('disconnected')
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
        reconnectTimeoutRef.current = null
      }
    }

    window.addEventListener('online', handleOnline)
    window.addEventListener('offline', handleOffline)

    return () => {
      window.removeEventListener('online', handleOnline)
      window.removeEventListener('offline', handleOffline)
    }
  }, [])

  const markDisconnected = useCallback(() => {
    setState('reconnecting')
    setRetryCount((prev) => prev + 1)

    // After 10 seconds of reconnecting, mark as fully disconnected
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current)
    }
    reconnectTimeoutRef.current = setTimeout(() => {
      setState((current) =>
        current === 'reconnecting' ? 'disconnected' : current
      )
    }, 10000)
  }, [])

  const markConnected = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }
    setState('connected')
    setRetryCount(0)
  }, [])

  const reset = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }
    setState('connected')
    setRetryCount(0)
  }, [])

  return (
    <ConnectionStatusContext.Provider
      value={{
        state,
        retryCount,
        isOnline,
        markDisconnected,
        markConnected,
        reset,
      }}
    >
      {children}
    </ConnectionStatusContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useConnectionStatus = () => {
  const context = useContext(ConnectionStatusContext)
  if (!context) {
    throw new Error(
      'useConnectionStatus must be used within a ConnectionStatusProvider'
    )
  }
  return context
}

/**
 * Hook that returns connection status helpers for use in sendMessages.
 * Returns null if not within a ConnectionStatusProvider (for backwards compatibility).
 */
// eslint-disable-next-line react-refresh/only-export-components
export const useConnectionStatusOptional = () => {
  return useContext(ConnectionStatusContext)
}
