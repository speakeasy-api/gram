import type { Meta, StoryObj } from '@storybook/react-vite'
import {
  AreaChart,
  BarChart,
  LineChart,
  PieChart,
  DonutChart,
  RadarChart,
  ScatterChart,
} from '@/plugins/chart/ui'

// Wrapper component to render different chart types
const ChartWrapper = ({ children }: { children: React.ReactNode }) => (
  <div className="w-full">{children}</div>
)

const meta: Meta = {
  title: 'Components/Charts',
  parameters: {
    layout: 'fullscreen',
  },
  decorators: [
    (Story) => (
      <div className="bg-background text-foreground min-h-screen p-6">
        <div className="max-w-2xl">
          <Story />
        </div>
      </div>
    ),
  ],
}

export default meta

// Sample data for time series charts
const timeSeriesData = [
  { label: 'Jan', value: 4000, revenue: 2400, profit: 1200 },
  { label: 'Feb', value: 3000, revenue: 1398, profit: 900 },
  { label: 'Mar', value: 2000, revenue: 9800, profit: 2100 },
  { label: 'Apr', value: 2780, revenue: 3908, profit: 1500 },
  { label: 'May', value: 1890, revenue: 4800, profit: 1800 },
  { label: 'Jun', value: 2390, revenue: 3800, profit: 1400 },
  { label: 'Jul', value: 3490, revenue: 4300, profit: 2000 },
]

// Sample data for category charts
const categoryData = [
  { label: 'Electronics', value: 4500 },
  { label: 'Clothing', value: 3200 },
  { label: 'Home & Garden', value: 2800 },
  { label: 'Sports', value: 2100 },
  { label: 'Books', value: 1500 },
]

// Sample data for pie/donut charts
const pieData = [
  { label: 'Desktop', value: 45 },
  { label: 'Mobile', value: 35 },
  { label: 'Tablet', value: 15 },
  { label: 'Other', value: 5 },
]

// Sample data for radar chart
const radarData = [
  { label: 'Speed', value: 85 },
  { label: 'Reliability', value: 90 },
  { label: 'Comfort', value: 78 },
  { label: 'Safety', value: 95 },
  { label: 'Efficiency', value: 82 },
]

// Sample data for scatter chart
const scatterData = [
  { x: 10, y: 30, label: 'A' },
  { x: 40, y: 50, label: 'B' },
  { x: 20, y: 80, label: 'C' },
  { x: 60, y: 40, label: 'D' },
  { x: 80, y: 90, label: 'E' },
  { x: 30, y: 60, label: 'F' },
  { x: 50, y: 20, label: 'G' },
  { x: 70, y: 70, label: 'H' },
]

/**
 * Area chart showing trends over time with filled areas.
 */
export const Area: StoryObj = {
  render: () => (
    <ChartWrapper>
      <AreaChart
        title="Revenue Over Time"
        data={timeSeriesData}
        series={['value', 'revenue']}
        showGrid
        showLegend
      />
    </ChartWrapper>
  ),
}

/**
 * Stacked area chart comparing multiple data series.
 */
export const AreaStacked: StoryObj = {
  render: () => (
    <ChartWrapper>
      <AreaChart
        title="Stacked Revenue Breakdown"
        data={timeSeriesData}
        series={['revenue', 'profit']}
        stacked
        showGrid
        showLegend
      />
    </ChartWrapper>
  ),
}

/**
 * Vertical bar chart for comparing categories.
 */
export const Bar: StoryObj = {
  render: () => (
    <ChartWrapper>
      <BarChart
        title="Sales by Category"
        data={categoryData}
        showGrid
        showLegend
      />
    </ChartWrapper>
  ),
}

/**
 * Horizontal bar chart layout.
 */
export const BarHorizontal: StoryObj = {
  render: () => (
    <ChartWrapper>
      <BarChart
        title="Sales by Category"
        data={categoryData}
        layout="horizontal"
        showGrid
      />
    </ChartWrapper>
  ),
}

/**
 * Line chart for tracking trends over time.
 */
export const Line: StoryObj = {
  render: () => (
    <ChartWrapper>
      <LineChart
        title="Monthly Performance"
        data={timeSeriesData}
        series={['value', 'revenue']}
        showGrid
        showLegend
        showDots
      />
    </ChartWrapper>
  ),
}

/**
 * Curved line chart with smooth interpolation.
 */
export const LineCurved: StoryObj = {
  render: () => (
    <ChartWrapper>
      <LineChart
        title="Smooth Trend Line"
        data={timeSeriesData}
        series={['value']}
        showGrid
        curved
        showDots
      />
    </ChartWrapper>
  ),
}

/**
 * Pie chart showing proportional data.
 */
export const Pie: StoryObj = {
  render: () => (
    <ChartWrapper>
      <PieChart
        title="Traffic by Device"
        data={pieData}
        showLegend
        showLabels
      />
    </ChartWrapper>
  ),
}

/**
 * Donut chart with center label.
 */
export const Donut: StoryObj = {
  render: () => (
    <ChartWrapper>
      <DonutChart
        title="Traffic by Device"
        data={pieData}
        showLegend
        innerLabel="Total"
        innerValue="100%"
      />
    </ChartWrapper>
  ),
}

/**
 * Radar chart for multi-dimensional comparison.
 */
export const Radar: StoryObj = {
  render: () => (
    <ChartWrapper>
      <RadarChart title="Product Metrics" data={radarData} showLegend />
    </ChartWrapper>
  ),
}

/**
 * Scatter chart for showing correlation between variables.
 */
export const Scatter: StoryObj = {
  render: () => (
    <ChartWrapper>
      <ScatterChart
        title="Price vs. Performance"
        data={scatterData}
        xLabel="Price"
        yLabel="Performance"
        showGrid
      />
    </ChartWrapper>
  ),
}
