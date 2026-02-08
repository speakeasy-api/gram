'use client'

import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import type { FC } from 'react'

export interface ProgressProps {
  /** Current progress value */
  value: number
  /** Maximum value (default: 100) */
  max?: number
  /** Optional label */
  label?: string
  /** Additional class names */
  className?: string
}

/**
 * Progress component - Progress bar with percentage display.
 * Use for showing completion or loading states.
 */
export const Progress: FC<ProgressProps> = ({
  value,
  max = 100,
  label,
  className,
}) => {
  const r = useRadius()
  const numValue = Number(value)
  const numMax = Number(max)
  const percentage =
    isNaN(numValue) || isNaN(numMax) || numMax === 0
      ? 0
      : Math.min(100, Math.max(0, (numValue / numMax) * 100))

  return (
    <div className={cn('w-full space-y-2', className)}>
      {label && (
        <div className="flex justify-between text-sm">
          <span>{label}</span>
          <span className="text-muted-foreground">
            {percentage.toFixed(0)}%
          </span>
        </div>
      )}
      <div className={cn('bg-muted h-3 overflow-hidden', r('sm'))}>
        <div
          className={cn('bg-primary h-full transition-all', r('sm'))}
          style={{ width: `${percentage}%` }}
        />
      </div>
    </div>
  )
}
