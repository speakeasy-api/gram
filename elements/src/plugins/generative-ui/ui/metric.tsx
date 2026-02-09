import * as React from 'react'
import { cn } from '@/lib/utils'

export interface MetricProps extends React.ComponentProps<'div'> {
  label: string
  value: string | number
  format?: 'number' | 'currency' | 'percent'
}

function formatValue(
  value: string | number,
  format?: 'number' | 'currency' | 'percent'
): string {
  if (typeof value === 'string') return value

  switch (format) {
    case 'currency':
      return new Intl.NumberFormat('en-US', {
        style: 'currency',
        currency: 'USD',
      }).format(value)
    case 'percent':
      return new Intl.NumberFormat('en-US', {
        style: 'percent',
        minimumFractionDigits: 0,
        maximumFractionDigits: 1,
      }).format(value)
    case 'number':
    default:
      return new Intl.NumberFormat('en-US').format(value)
  }
}

export function Metric({
  label,
  value,
  format,
  className,
  ...props
}: MetricProps) {
  return (
    <div
      data-slot="metric"
      className={cn('flex flex-col', className)}
      {...props}
    >
      <span className="text-muted-foreground text-sm">{label}</span>
      <span className="text-foreground text-2xl font-semibold">
        {formatValue(value, format)}
      </span>
    </div>
  )
}
