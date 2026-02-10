import { createCatalog } from '@json-render/core'
import { z } from 'zod'

/**
 * Data point schema - common structure for all chart types
 */
const dataPointSchema = z.object({
  label: z.string(),
  value: z.number(),
  color: z.string().optional(),
})

/**
 * Multi-series data point for line/area charts
 */
const seriesDataPointSchema = z
  .object({
    label: z.string(),
  })
  .catchall(z.number())

/**
 * Chart Catalog
 *
 * Defines all available chart components for LLM-generated visualizations.
 * Uses Recharts under the hood for rendering.
 */
export const chartCatalog = createCatalog({
  name: 'chart',
  components: {
    BarChart: {
      props: z.object({
        title: z.string().optional(),
        data: z.array(dataPointSchema),
        layout: z.enum(['vertical', 'horizontal']).optional(),
        showGrid: z.boolean().optional(),
        showLegend: z.boolean().optional(),
        className: z.string().optional(),
      }),
      description:
        'Bar chart for comparing categorical data. Use vertical for few categories, horizontal for many or long labels.',
    },

    LineChart: {
      props: z.object({
        title: z.string().optional(),
        data: z.array(seriesDataPointSchema),
        series: z.array(z.string()).optional(),
        showGrid: z.boolean().optional(),
        showLegend: z.boolean().optional(),
        showDots: z.boolean().optional(),
        curved: z.boolean().optional(),
        className: z.string().optional(),
      }),
      description:
        'Line chart for showing trends over time or continuous data. Supports multiple series.',
    },

    AreaChart: {
      props: z.object({
        title: z.string().optional(),
        data: z.array(seriesDataPointSchema),
        series: z.array(z.string()).optional(),
        stacked: z.boolean().optional(),
        showGrid: z.boolean().optional(),
        showLegend: z.boolean().optional(),
        className: z.string().optional(),
      }),
      description:
        'Area chart for showing volume/magnitude over time. Use stacked for part-to-whole relationships.',
    },

    PieChart: {
      props: z.object({
        title: z.string().optional(),
        data: z.array(dataPointSchema),
        showLabels: z.boolean().optional(),
        showLegend: z.boolean().optional(),
        className: z.string().optional(),
      }),
      description:
        'Pie chart for showing proportions of a whole. Best for 2-6 categories.',
    },

    DonutChart: {
      props: z.object({
        title: z.string().optional(),
        data: z.array(dataPointSchema),
        showLabels: z.boolean().optional(),
        showLegend: z.boolean().optional(),
        innerLabel: z.string().optional(),
        innerValue: z.union([z.string(), z.number()]).optional(),
        className: z.string().optional(),
      }),
      description:
        'Donut chart (pie with center hole). Good for showing a key metric in the center.',
    },

    ScatterChart: {
      props: z.object({
        title: z.string().optional(),
        data: z.array(
          z.object({
            x: z.number(),
            y: z.number(),
            label: z.string().optional(),
            size: z.number().optional(),
            color: z.string().optional(),
          })
        ),
        xLabel: z.string().optional(),
        yLabel: z.string().optional(),
        showGrid: z.boolean().optional(),
        className: z.string().optional(),
      }),
      description:
        'Scatter plot for showing correlation between two variables.',
    },

    RadarChart: {
      props: z.object({
        title: z.string().optional(),
        data: z.array(dataPointSchema),
        showLegend: z.boolean().optional(),
        className: z.string().optional(),
      }),
      description:
        'Radar/spider chart for comparing multiple attributes. Best for 3-8 dimensions.',
    },
  },
})

export type ChartCatalogComponentProps = typeof chartCatalog extends {
  components: infer C
}
  ? {
      [K in keyof C]: C[K] extends { props: infer P }
        ? z.infer<P extends z.ZodType ? P : never>
        : never
    }
  : never
