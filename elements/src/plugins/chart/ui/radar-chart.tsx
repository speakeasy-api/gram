'use client'

import { cn } from '@/lib/utils'
import { FC } from 'react'
import {
  RadarChart as RechartsRadarChart,
  Radar,
  PolarGrid,
  PolarAngleAxis,
  PolarRadiusAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
  TooltipProps,
} from 'recharts'

const CustomTooltip = ({ active, payload }: TooltipProps<number, string>) => {
  if (!active || !payload || payload.length === 0) return null
  const entry = payload[0]
  return (
    <div className="bg-background text-foreground border-border rounded-md border px-2 py-1 text-xs shadow-sm">
      <span className="font-medium">
        {typeof entry?.value === 'number'
          ? entry.value.toLocaleString()
          : entry?.value}
      </span>
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

interface DataPoint {
  label: string
  value: number
  color?: string
}

export interface RadarChartProps {
  title?: string
  data: DataPoint[]
  showLegend?: boolean
  className?: string
}

export const RadarChart: FC<RadarChartProps> = ({
  title,
  data,
  showLegend = false,
  className,
}) => {
  // Transform data for Recharts (uses 'subject' for labels)
  const chartData = data.map((d) => ({ subject: d.label, value: d.value }))

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      {title && (
        <h3 className="text-foreground text-sm font-medium">{title}</h3>
      )}
      <div className="h-[250px] w-full">
        <ResponsiveContainer width="100%" height="100%">
          <RechartsRadarChart
            data={chartData}
            cx="50%"
            cy="50%"
            outerRadius="80%"
          >
            <PolarGrid stroke="var(--border)" />
            <PolarAngleAxis
              dataKey="subject"
              tick={{ fill: 'var(--foreground)', fontSize: 12 }}
            />
            <PolarRadiusAxis
              tick={{ fill: 'var(--foreground)', fontSize: 10 }}
              axisLine={{ stroke: 'var(--border)' }}
            />
            <Tooltip content={<CustomTooltip />} />
            {showLegend && (
              <Legend
                wrapperStyle={{ color: 'var(--foreground)' }}
                formatter={(value) => (
                  <span style={{ color: 'var(--foreground)' }}>{value}</span>
                )}
              />
            )}
            <Radar
              name="Value"
              dataKey="value"
              stroke={COLORS[0]}
              fill={COLORS[0]}
              fillOpacity={0.3}
              isAnimationActive={false}
            />
          </RechartsRadarChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}
