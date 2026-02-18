import * as React from 'react'
import { cn } from '@/lib/utils'

export interface CardWrapperProps extends React.ComponentProps<'div'> {
  title?: string
}

/**
 * CardWrapper is a frameless container for generative UI content.
 * It has no border or shadow since it's rendered inside MacOSWindowFrame.
 * Children are spaced using flex gap for proper composition.
 */
export function CardWrapper({
  title,
  className,
  children,
  ...props
}: CardWrapperProps) {
  return (
    <div
      data-slot="card-wrapper"
      className={cn(
        'bg-card text-card-foreground flex flex-col gap-6 p-6',
        className
      )}
      {...props}
    >
      {title && (
        <div className="text-foreground text-lg leading-none font-semibold">
          {title}
        </div>
      )}
      <div className="flex flex-col gap-6">{children}</div>
    </div>
  )
}
