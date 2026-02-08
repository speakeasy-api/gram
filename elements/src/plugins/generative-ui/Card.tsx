'use client'

import { useRadius } from '@/hooks/useRadius'
import { useDensity } from '@/hooks/useDensity'
import { cn } from '@/lib/utils'
import type { FC, ReactNode } from 'react'

export interface CardProps {
  /** Optional title displayed at the top of the card */
  title?: string
  /** Card content */
  children?: ReactNode
  /** Additional class names */
  className?: string
}

/**
 * Card component - A container with optional title, border and padding.
 * Used to group related content together.
 */
export const Card: FC<CardProps> = ({ title, children, className }) => {
  const r = useRadius()
  const d = useDensity()

  return (
    <div
      className={cn(
        'border-border bg-card border',
        r('lg'),
        d('p-lg'),
        className
      )}
    >
      {title && <h3 className="mb-4 text-lg font-semibold">{title}</h3>}
      {children}
    </div>
  )
}
