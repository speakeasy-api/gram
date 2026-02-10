'use client'

import { cn } from '@/lib/utils'
import { FC, useMemo } from 'react'
import {
  LineChart as RechartsLineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  TooltipProps,
} from 'recharts'

const CustomTooltip = ({ active, payload }: TooltipProps<number, string>) => {
  if (!active || !payload || payload.length === 0) return null
  return (
    <div className="bg-background text-foreground border-border rounded-md border px-2 py-1.5 text-xs shadow-sm">
      {payload.map((entry, index) => (
        <div key={index} className="flex items-center gap-2">
          <span
            className="h-2 w-2 rounded-full"
            style={{ backgroundColor: entry.color }}
          />
          <span>{entry.name}:</span>
          <span className="font-medium">
            {typeof entry.value === 'number'
              ? entry.value.toLocaleString()
              : entry.value}
          </span>
        </div>
      ))}
    </div>
  )
}

const COLORS = [
  'var(--chart-1)',
  'var(--chart-2)',
  'var(--chart-3)',
  'var(--chart-4)',
  'var(--chart-5)',
]

interface SeriesDataPoint {
  label: string
  [key: string]: string | number
}

export interface LineChartProps {
  title?: string
  data: SeriesDataPoint[]
  series?: string[]
  showGrid?: boolean
  showLegend?: boolean
  showDots?: boolean
  curved?: boolean
  className?: string
}

export const LineChart: FC<LineChartProps> = ({
  title,
  data,
  series,
  showGrid = true,
  showLegend = true,
  showDots = true,
  curved = true,
  className,
}) => {
  // Auto-detect series from data keys if not provided
  const seriesKeys = useMemo(() => {
    if (series && series.length > 0) return series
    if (data.length === 0) return []
    const keys = Object.keys(data[0]).filter((k) => k !== 'label')
    return keys
  }, [data, series])

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      {title && (
        <h3 className="text-foreground text-sm font-medium">{title}</h3>
      )}
      <div className="h-[250px] w-full">
        <ResponsiveContainer width="100%" height="100%">
          <RechartsLineChart
            data={data}
            margin={{ top: 10, right: 10, left: 10, bottom: 10 }}
          >
            {showGrid && (
              <CartesianGrid
                strokeDasharray="3 3"
                className="stroke-muted/30"
              />
            )}
            <XAxis
              dataKey="label"
              tick={{ fill: 'var(--foreground)', fontSize: 12 }}
              axisLine={{ stroke: 'var(--border)' }}
              tickLine={{ stroke: 'var(--border)' }}
            />
            <YAxis
              tick={{ fill: 'var(--foreground)', fontSize: 12 }}
              axisLine={{ stroke: 'var(--border)' }}
              tickLine={{ stroke: 'var(--border)' }}
            />
            <Tooltip content={<CustomTooltip />} />
            {showLegend && seriesKeys.length > 1 && (
              <Legend
                wrapperStyle={{ color: 'var(--foreground)' }}
                formatter={(value) => (
                  <span style={{ color: 'var(--foreground)' }}>{value}</span>
                )}
              />
            )}
            {seriesKeys.map((key, index) => (
              <Line
                key={key}
                type={curved ? 'monotone' : 'linear'}
                dataKey={key}
                stroke={COLORS[index % COLORS.length]}
                strokeWidth={2}
                dot={showDots}
                activeDot={{ r: 6 }}
                isAnimationActive={false}
              />
            ))}
          </RechartsLineChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}
