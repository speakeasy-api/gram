'use client'

import {
  useConnectionStatus,
  type ConnectionState,
} from '@/contexts/ConnectionStatusContext'
import { cn } from '@/lib/utils'
import { Loader2Icon, WifiOffIcon } from 'lucide-react'
import { AnimatePresence, motion } from 'motion/react'
import { FC } from 'react'

interface ConnectionStatusIndicatorProps {
  className?: string
}

/**
 * iOS-style floating island that appears when connection is lost.
 * Shows "Reconnecting..." with a spinner, or "Connection Lost" after timeout.
 */
export const ConnectionStatusIndicator: FC<ConnectionStatusIndicatorProps> = ({
  className,
}) => {
  const { state, isOnline } = useConnectionStatus()

  const shouldShow = state === 'reconnecting' || state === 'disconnected'

  return (
    <AnimatePresence>
      {shouldShow && (
        <motion.div
          initial={{ opacity: 0, y: -20, scale: 0.95 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          exit={{ opacity: 0, y: -20, scale: 0.95 }}
          transition={{
            type: 'spring',
            stiffness: 500,
            damping: 30,
          }}
          className={cn(
            'pointer-events-none absolute top-4 right-0 left-0 z-50 flex justify-center',
            className
          )}
        >
          <StatusPill state={state} isOnline={isOnline} />
        </motion.div>
      )}
    </AnimatePresence>
  )
}

const StatusPill: FC<{ state: ConnectionState; isOnline: boolean }> = ({
  state,
  isOnline,
}) => {
  const isReconnecting = state === 'reconnecting'
  const isOffline = !isOnline

  // Show "Offline" when browser is offline, otherwise show connection state
  const showOffline = isOffline && state === 'disconnected'
  const showReconnecting =
    isReconnecting || (isOffline && state !== 'connected')

  return (
    <div
      className={cn(
        'pointer-events-auto flex items-center gap-2 rounded-full px-4 py-2 shadow-lg backdrop-blur-md',
        'border border-white/10',
        showOffline
          ? 'bg-zinc-700/90 text-white dark:bg-zinc-800/90'
          : showReconnecting
            ? 'bg-amber-500/90 text-white dark:bg-amber-600/90'
            : 'bg-red-500/90 text-white dark:bg-red-600/90'
      )}
    >
      {showOffline ? (
        <>
          <WifiOffIcon className="size-4" />
          <span className="text-sm font-medium">Offline</span>
        </>
      ) : showReconnecting ? (
        <>
          <Loader2Icon className="size-4 animate-spin" />
          <span className="text-sm font-medium">Reconnecting...</span>
        </>
      ) : (
        <>
          <WifiOffIcon className="size-4" />
          <span className="text-sm font-medium">Connection Lost</span>
        </>
      )}
    </div>
  )
}

/**
 * Wrapper version that handles the case when ConnectionStatusProvider is not available.
 * This is useful for backwards compatibility.
 */
export const ConnectionStatusIndicatorSafe: FC<
  ConnectionStatusIndicatorProps
> = (props) => {
  try {
    return <ConnectionStatusIndicator {...props} />
  } catch {
    // ConnectionStatusProvider not available, render nothing
    return null
  }
}
