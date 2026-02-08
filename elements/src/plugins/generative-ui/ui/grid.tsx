import * as React from 'react'
import { cn } from '@/lib/utils'

export interface GridProps extends React.ComponentProps<'div'> {
  columns?: number
  gap?: 'sm' | 'md' | 'lg'
}

const gapClasses = {
  sm: 'gap-2',
  md: 'gap-4',
  lg: 'gap-6',
}

export function Grid({
  columns = 2,
  gap = 'md',
  className,
  ...props
}: GridProps) {
  return (
    <div
      data-slot="grid"
      className={cn('grid', gapClasses[gap], className)}
      style={{ gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}
      {...props}
    />
  )
}
