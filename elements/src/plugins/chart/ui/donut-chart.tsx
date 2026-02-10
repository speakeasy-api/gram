'use client'

import { cn } from '@/lib/utils'
import { FC } from 'react'
import {
  PieChart as RechartsPieChart,
  Pie,
  Cell,
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

export interface DonutChartProps {
  title?: string
  data: DataPoint[]
  showLabels?: boolean
  showLegend?: boolean
  innerLabel?: string
  innerValue?: string | number
  className?: string
}

export const DonutChart: FC<DonutChartProps> = ({
  title,
  data,
  showLabels = false,
  showLegend = true,
  innerLabel,
  innerValue,
  className,
}) => {
  // Transform data to use 'name' for Recharts
  const chartData = data.map((d) => ({
    name: d.label,
    value: d.value,
    color: d.color,
  }))

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      {title && (
        <h3 className="text-foreground text-sm font-medium">{title}</h3>
      )}
      <div className="relative h-[320px] w-full">
        <ResponsiveContainer width="100%" height="100%">
          <RechartsPieChart
            margin={{ top: 20, right: 20, bottom: 20, left: 20 }}
          >
            <Pie
              data={chartData}
              cx="50%"
              cy="45%"
              innerRadius={50}
              outerRadius={80}
              paddingAngle={2}
              dataKey="value"
              label={
                showLabels
                  ? ({
                      name,
                      percent,
                      cx,
                      cy,
                      midAngle,
                      outerRadius,
                    }: {
                      name?: string
                      percent?: number
                      cx?: number
                      cy?: number
                      midAngle?: number
                      outerRadius?: number
                    }) => {
                      const RADIAN = Math.PI / 180
                      const radius = (outerRadius ?? 80) + 25
                      const x =
                        (cx ?? 0) +
                        radius * Math.cos(-((midAngle ?? 0) * RADIAN))
                      const y =
                        (cy ?? 0) +
                        radius * Math.sin(-((midAngle ?? 0) * RADIAN))
                      return (
                        <text
                          x={x}
                          y={y}
                          fill="var(--foreground)"
                          textAnchor={x > (cx ?? 0) ? 'start' : 'end'}
                          dominantBaseline="central"
                          fontSize={12}
                        >
                          {`${name ?? ''} (${((percent ?? 0) * 100).toFixed(0)}%)`}
                        </text>
                      )
                    }
                  : undefined
              }
              labelLine={showLabels}
              isAnimationActive={false}
            >
              {chartData.map((entry, index) => (
                <Cell
                  key={`cell-${index}`}
                  fill={entry.color || COLORS[index % COLORS.length]}
                />
              ))}
            </Pie>
            <Tooltip content={<CustomTooltip />} />
            {showLegend && (
              <Legend
                verticalAlign="bottom"
                wrapperStyle={{ color: 'var(--foreground)', paddingTop: 20 }}
                formatter={(value) => (
                  <span style={{ color: 'var(--foreground)' }}>{value}</span>
                )}
              />
            )}
          </RechartsPieChart>
        </ResponsiveContainer>
        {/* Center label - positioned at 45% from top to match pie cy */}
        {(innerLabel !== undefined || innerValue !== undefined) && (
          <div className="pointer-events-none absolute inset-x-0 top-[45%] flex -translate-y-1/2 flex-col items-center">
            {innerValue !== undefined && (
              <span className="text-foreground text-2xl font-bold">
                {innerValue}
              </span>
            )}
            {innerLabel && (
              <span className="text-muted-foreground text-xs">
                {innerLabel}
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
