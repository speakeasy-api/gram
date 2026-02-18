'use client'

import { cn } from '@/lib/utils'
import { FC } from 'react'
import { MacOSWindowFrame } from './MacOSWindowFrame'

interface PluginLoadingStateProps {
  text: string
  className?: string
}

/**
 * Shared loading state component for plugins.
 * Displays a shimmer effect with loading text inside a macOS-style window.
 */
export const PluginLoadingState: FC<PluginLoadingStateProps> = ({
  text,
  className,
}) => {
  return (
    <MacOSWindowFrame className={className}>
      <div
        className={cn(
          'bg-background relative flex min-h-[400px] items-center justify-center'
        )}
      >
        <span className="shimmer text-muted-foreground text-sm">{text}</span>
      </div>
    </MacOSWindowFrame>
  )
}
