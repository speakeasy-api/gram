'use client'

import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import { FC, ReactNode } from 'react'

interface MacOSWindowFrameProps {
  children: ReactNode
  className?: string
  /** Optional title to display in the title bar */
  title?: string
}

/**
 * A macOS-style window frame with traffic light buttons.
 * Wraps content in a bordered container with a title bar.
 */
export const MacOSWindowFrame: FC<MacOSWindowFrameProps> = ({
  children,
  className,
  title,
}) => {
  const r = useRadius()

  return (
    <div className="@container my-4 w-full first:mt-0">
      <div
        className={cn(
          // after:hidden prevents assistant-ui from showing its default code block loading indicator
          'border-border w-full overflow-hidden border after:hidden @sm:max-w-md @md:max-w-lg @lg:max-w-xl @xl:max-w-2xl',
          r('lg'),
          className
        )}
      >
        {/* Title bar */}
        <div className="border-border bg-muted/50 flex h-8 items-center gap-2 border-b px-3">
          {/* Traffic lights */}
          <div className="flex items-center gap-1.5">
            <div className="size-3 rounded-full bg-[#FF5F57]" />
            <div className="size-3 rounded-full bg-[#FEBC2E]" />
            <div className="size-3 rounded-full bg-[#28C840]" />
          </div>
          {/* Title */}
          {title && (
            <span className="text-muted-foreground flex-1 text-center text-xs font-medium">
              {title}
            </span>
          )}
        </div>
        {/* Content */}
        {children}
      </div>
    </div>
  )
}
