'use client'

import { cn } from '@/lib/utils'
import type { FC } from 'react'
import { useMemo } from 'react'

export interface MetricProps {
  /** Label describing the metric */
  label: string
  /** Numeric value to display */
  value: number
  /** Format type for the value */
  format?: 'currency' | 'percent' | 'number'
  /** Additional class names */
  className?: string
}

/**
 * Metric component - Display a label with a formatted numeric value.
 * Supports currency, percentage, and number formatting.
 */
export const Metric: FC<MetricProps> = ({
  label,
  value,
  format = 'number',
  className,
}) => {
  const formattedValue = useMemo(() => {
    const numValue = Number(value)
    if (isNaN(numValue)) return String(value)

    switch (format) {
      case 'currency':
        return new Intl.NumberFormat('en-US', {
          style: 'currency',
          currency: 'USD',
        }).format(numValue)
      case 'percent':
        return new Intl.NumberFormat('en-US', {
          style: 'percent',
          minimumFractionDigits: 1,
        }).format(numValue)
      case 'number':
      default:
        return new Intl.NumberFormat('en-US').format(numValue)
    }
  }, [value, format])

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <span className="text-muted-foreground text-sm">{label}</span>
      <span className="text-3xl font-bold">{formattedValue}</span>
    </div>
  )
}
