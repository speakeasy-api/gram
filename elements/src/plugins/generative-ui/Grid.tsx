'use client'

import { useDensity } from '@/hooks/useDensity'
import { cn } from '@/lib/utils'
import type { FC, ReactNode } from 'react'

export interface GridProps {
  /** Number of columns (default: 2) */
  columns?: number
  /** Grid content */
  children?: ReactNode
  /** Additional class names */
  className?: string
}

/**
 * Grid component - Multi-column layout with responsive gaps.
 * Use for arranging items in a grid pattern.
 */
export const Grid: FC<GridProps> = ({ columns = 2, children, className }) => {
  const d = useDensity()

  return (
    <div
      className={cn('grid', d('gap-lg'), className)}
      style={{
        gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`,
      }}
    >
      {children}
    </div>
  )
}
