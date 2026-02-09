'use client'

import { cn } from '@/lib/utils'
import { FC } from 'react'
import {
  ScatterChart as RechartsScatterChart,
  Scatter,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  ZAxis,
  Cell,
  TooltipProps,
} from 'recharts'

interface ScatterDataPoint {
  x: number
  y: number
  label?: string
  size?: number
  color?: string
}

const CustomTooltip = ({ active, payload }: TooltipProps<number, string>) => {
  if (!active || !payload || payload.length === 0) return null
  const point = payload[0]?.payload as ScatterDataPoint | undefined
  return (
    <div className="bg-background text-foreground border-border rounded-md border px-2 py-1.5 text-xs shadow-sm">
      {point?.label && <div className="font-medium">{point.label}</div>}
      <div>x: {point?.x?.toLocaleString()}</div>
      <div>y: {point?.y?.toLocaleString()}</div>
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

export interface ScatterChartProps {
  title?: string
  data: ScatterDataPoint[]
  xLabel?: string
  yLabel?: string
  showGrid?: boolean
  className?: string
}

export const ScatterChart: FC<ScatterChartProps> = ({
  title,
  data,
  xLabel,
  yLabel,
  showGrid = true,
  className,
}) => {
  // Check if we have size data for bubble chart effect
  const hasSizeData = data.some((d) => d.size !== undefined)

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      {title && (
        <h3 className="text-foreground text-sm font-medium">{title}</h3>
      )}
      <div className="h-[250px] w-full">
        <ResponsiveContainer width="100%" height="100%">
          <RechartsScatterChart
            margin={{ top: 10, right: 10, left: 10, bottom: 10 }}
          >
            {showGrid && (
              <CartesianGrid
                strokeDasharray="3 3"
                className="stroke-muted/30"
              />
            )}
            <XAxis
              type="number"
              dataKey="x"
              name={xLabel || 'x'}
              tick={{ fill: 'var(--foreground)', fontSize: 12 }}
              axisLine={{ stroke: 'var(--border)' }}
              tickLine={{ stroke: 'var(--border)' }}
              label={
                xLabel
                  ? {
                      value: xLabel,
                      position: 'bottom',
                      offset: -5,
                      fill: 'var(--foreground)',
                    }
                  : undefined
              }
            />
            <YAxis
              type="number"
              dataKey="y"
              name={yLabel || 'y'}
              tick={{ fill: 'var(--foreground)', fontSize: 12 }}
              axisLine={{ stroke: 'var(--border)' }}
              tickLine={{ stroke: 'var(--border)' }}
              label={
                yLabel
                  ? {
                      value: yLabel,
                      angle: -90,
                      position: 'left',
                      fill: 'var(--foreground)',
                    }
                  : undefined
              }
            />
            {hasSizeData && (
              <ZAxis type="number" dataKey="size" range={[50, 400]} />
            )}
            <Tooltip content={<CustomTooltip />} />
            <Scatter data={data} fill={COLORS[0]} isAnimationActive={false}>
              {data.map((entry, index) => (
                <Cell key={`cell-${index}`} fill={entry.color || COLORS[0]} />
              ))}
            </Scatter>
          </RechartsScatterChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}
