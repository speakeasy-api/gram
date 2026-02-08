'use client'

import { cn } from '@/lib/utils'
import { FC } from 'react'
import {
  BarChart as RechartsBarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  Cell,
  ResponsiveContainer,
  TooltipProps,
} from 'recharts'

const CustomTooltip = ({ active, payload }: TooltipProps<number, string>) => {
  if (!active || !payload || payload.length === 0) return null
  const value = payload[0]?.value
  return (
    <div className="bg-background text-foreground border-border rounded-md border px-2 py-1 text-xs shadow-sm">
      {typeof value === 'number' ? value.toLocaleString() : value}
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

export interface BarChartProps {
  title?: string
  data: DataPoint[]
  layout?: 'vertical' | 'horizontal'
  showGrid?: boolean
  showLegend?: boolean
  className?: string
}

export const BarChart: FC<BarChartProps> = ({
  title,
  data,
  layout = 'vertical',
  showGrid = true,
  showLegend = false,
  className,
}) => {
  const isHorizontal = layout === 'horizontal'

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      {title && (
        <h3 className="text-foreground text-sm font-medium">{title}</h3>
      )}
      <div className="h-[250px] w-full">
        <ResponsiveContainer width="100%" height="100%">
          <RechartsBarChart
            data={data}
            layout={isHorizontal ? 'vertical' : 'horizontal'}
            margin={{ top: 10, right: 10, left: 10, bottom: 10 }}
          >
            {showGrid && (
              <CartesianGrid
                strokeDasharray="3 3"
                className="stroke-muted/30"
              />
            )}
            {isHorizontal ? (
              <>
                <XAxis
                  type="number"
                  tick={{ fill: 'var(--foreground)', fontSize: 12 }}
                  axisLine={{ stroke: 'var(--border)' }}
                  tickLine={{ stroke: 'var(--border)' }}
                />
                <YAxis
                  dataKey="label"
                  type="category"
                  width={80}
                  tick={{ fill: 'var(--foreground)', fontSize: 12 }}
                  axisLine={{ stroke: 'var(--border)' }}
                  tickLine={{ stroke: 'var(--border)' }}
                />
              </>
            ) : (
              <>
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
              </>
            )}
            <Tooltip content={<CustomTooltip />} />
            {showLegend && (
              <Legend
                wrapperStyle={{ color: 'var(--foreground)' }}
                formatter={(value) => (
                  <span style={{ color: 'var(--foreground)' }}>{value}</span>
                )}
              />
            )}
            <Bar
              dataKey="value"
              radius={[4, 4, 0, 0]}
              isAnimationActive={false}
            >
              {data.map((entry, index) => (
                <Cell
                  key={`cell-${index}`}
                  fill={entry.color || COLORS[index % COLORS.length]}
                />
              ))}
            </Bar>
          </RechartsBarChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}
