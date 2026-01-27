'use client'

import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import { FC } from 'react'

interface PluginLoadingStateProps {
  text: string
  className?: string
}

/**
 * Shared loading state component for plugins.
 * Displays a shimmer effect with loading text.
 */
export const PluginLoadingState: FC<PluginLoadingStateProps> = ({
  text,
  className,
}) => {
  const r = useRadius()

  return (
    <div
      className={cn(
        'border-border bg-card relative min-h-[400px] w-fit max-w-full min-w-[400px] overflow-hidden border after:hidden',
        r('lg'),
        className
      )}
    >
      <div className="shimmer text-muted-foreground absolute inset-0 flex items-center justify-center">
        {text}
      </div>
    </div>
  )
}
