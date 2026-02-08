'use client'

import { useDensity } from '@/hooks/useDensity'
import { cn } from '@/lib/utils'
import type { FC, ReactNode } from 'react'

export interface StackProps {
  /** Direction of stacking */
  direction?: 'vertical' | 'horizontal'
  /** Stack content */
  children?: ReactNode
  /** Additional class names */
  className?: string
}

/**
 * Stack component - Flex container for vertical or horizontal layouts.
 * Use for arranging items in a single direction with consistent spacing.
 */
export const Stack: FC<StackProps> = ({
  direction = 'vertical',
  children,
  className,
}) => {
  const d = useDensity()

  return (
    <div
      className={cn(
        'flex',
        direction === 'horizontal' ? 'flex-row' : 'flex-col',
        d('gap-md'),
        className
      )}
    >
      {children}
    </div>
  )
}
