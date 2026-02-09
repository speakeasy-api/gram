import type { Meta, StoryFn } from '@storybook/react-vite'
import z from 'zod'
import { Chat } from '..'
import { defineFrontendTool } from '../../../lib/tools'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Plugins/Charts',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

// Mock data generator for orders summary
const generateOrdersSummary = (params: {
  period?: string
  groupBy?: string
}) => {
  const { period = 'last_30_days', groupBy = 'day' } = params

  // Generate time-series data based on groupBy
  const generateTimeSeries = () => {
    if (groupBy === 'month') {
      return [
        {
          date: '2024-07',
          orders: 1247,
          revenue: 125400,
          avgOrderValue: 100.5,
        },
        {
          date: '2024-08',
          orders: 1389,
          revenue: 142300,
          avgOrderValue: 102.4,
        },
        {
          date: '2024-09',
          orders: 1456,
          revenue: 151200,
          avgOrderValue: 103.8,
        },
        {
          date: '2024-10',
          orders: 1523,
          revenue: 162800,
          avgOrderValue: 106.9,
        },
        {
          date: '2024-11',
          orders: 1834,
          revenue: 198500,
          avgOrderValue: 108.2,
        },
        {
          date: '2024-12',
          orders: 2156,
          revenue: 245600,
          avgOrderValue: 113.9,
        },
      ]
    }
    if (groupBy === 'week') {
      return [
        {
          date: '2024-12-01',
          orders: 423,
          revenue: 48200,
          avgOrderValue: 114.0,
        },
        {
          date: '2024-12-08',
          orders: 512,
          revenue: 58900,
          avgOrderValue: 115.0,
        },
        {
          date: '2024-12-15',
          orders: 489,
          revenue: 55100,
          avgOrderValue: 112.7,
        },
        {
          date: '2024-12-22',
          orders: 732,
          revenue: 83400,
          avgOrderValue: 113.9,
        },
      ]
    }
    // Default: daily
    return [
      { date: '2024-12-23', orders: 89, revenue: 10230, avgOrderValue: 114.9 },
      { date: '2024-12-24', orders: 112, revenue: 12890, avgOrderValue: 115.1 },
      { date: '2024-12-25', orders: 67, revenue: 7450, avgOrderValue: 111.2 },
      { date: '2024-12-26', orders: 145, revenue: 16780, avgOrderValue: 115.7 },
      { date: '2024-12-27', orders: 134, revenue: 15340, avgOrderValue: 114.5 },
      { date: '2024-12-28', orders: 98, revenue: 11200, avgOrderValue: 114.3 },
      { date: '2024-12-29', orders: 87, revenue: 9870, avgOrderValue: 113.4 },
    ]
  }

  // Category breakdown
  const categoryBreakdown = [
    { category: 'Electronics', orders: 542, revenue: 89400, percentage: 32 },
    { category: 'Clothing', orders: 423, revenue: 45200, percentage: 25 },
    { category: 'Home & Garden', orders: 312, revenue: 38900, percentage: 18 },
    { category: 'Sports', orders: 234, revenue: 28700, percentage: 14 },
    { category: 'Other', orders: 189, revenue: 19300, percentage: 11 },
  ]

  // Status breakdown
  const statusBreakdown = [
    { status: 'Completed', count: 1423, percentage: 72 },
    { status: 'Processing', count: 287, percentage: 15 },
    { status: 'Shipped', count: 198, percentage: 10 },
    { status: 'Cancelled', count: 59, percentage: 3 },
  ]

  // Regional data
  const regionalData = [
    { region: 'North America', orders: 823, revenue: 98400 },
    { region: 'Europe', orders: 567, revenue: 72300 },
    { region: 'Asia Pacific', orders: 412, revenue: 51200 },
    { region: 'Latin America', orders: 156, revenue: 18900 },
  ]

  return {
    period,
    groupBy,
    summary: {
      totalOrders: 1967,
      totalRevenue: 221500,
      avgOrderValue: 112.6,
      orderGrowth: 12.4,
      revenueGrowth: 15.2,
    },
    timeSeries: generateTimeSeries(),
    categoryBreakdown,
    statusBreakdown,
    regionalData,
  }
}

// Define the GET /admin/orders/summary tool
const getOrdersSummaryTool = defineFrontendTool<
  { period?: string; groupBy?: string },
  ReturnType<typeof generateOrdersSummary>
>(
  {
    description:
      'Get a summary of orders including totals, time-series data, category breakdown, and regional distribution. Use this data to create visualizations.',
    parameters: z.object({
      period: z
        .enum(['last_7_days', 'last_30_days', 'last_90_days', 'last_year'])
        .optional()
        .describe('Time period for the summary'),
      groupBy: z
        .enum(['day', 'week', 'month'])
        .optional()
        .describe('How to group the time-series data'),
    }),
    execute: async (params) => {
      // Simulate API latency
      await new Promise((resolve) => setTimeout(resolve, 300))
      return generateOrdersSummary(params)
    },
  },
  'admin_api_get_orders_summary'
)

const chartTools = {
  admin_api_get_orders_summary: getOrdersSummaryTool,
}

/**
 * Bar chart demo - comparing categorical data using orders summary
 */
export const BarChart: Story = () => <Chat />
BarChart.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Orders Analytics',
        subtitle: 'Bar Chart Visualizations',
        suggestions: [
          {
            title: 'Revenue by Category',
            label: 'Bar chart',
            prompt:
              'Get the orders summary and show me a bar chart of revenue by product category',
          },
          {
            title: 'Orders by Region',
            label: 'Regional comparison',
            prompt:
              'Fetch the orders summary and create a horizontal bar chart comparing orders by region',
          },
        ],
      },
      tools: {
        frontendTools: chartTools,
      },
    },
  },
}

/**
 * Line chart demo - showing trends over time
 */
export const LineChart: Story = () => <Chat />
LineChart.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Orders Analytics',
        subtitle: 'Line Chart Visualizations',
        suggestions: [
          {
            title: 'Daily Orders Trend',
            label: 'Line chart',
            prompt:
              'Get the orders summary grouped by day and show me a line chart of daily order counts',
          },
          {
            title: 'Revenue vs Orders',
            label: 'Multiple series',
            prompt:
              'Fetch weekly orders data and create a line chart showing both revenue and order count trends',
          },
        ],
      },
      tools: {
        frontendTools: chartTools,
      },
    },
  },
}

/**
 * Area chart demo - showing volume over time
 */
export const AreaChart: Story = () => <Chat />
AreaChart.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Orders Analytics',
        subtitle: 'Area Chart Visualizations',
        suggestions: [
          {
            title: 'Monthly Revenue',
            label: 'Area chart',
            prompt:
              'Get the orders summary grouped by month and show me an area chart of revenue over time',
          },
          {
            title: 'Order Growth',
            label: 'Trend visualization',
            prompt:
              'Fetch monthly orders data and create an area chart showing order volume growth',
          },
        ],
      },
      tools: {
        frontendTools: chartTools,
      },
    },
  },
}

/**
 * Pie chart demo - showing proportions
 */
export const PieChart: Story = () => <Chat />
PieChart.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Orders Analytics',
        subtitle: 'Pie Chart Visualizations',
        suggestions: [
          {
            title: 'Category Distribution',
            label: 'Pie chart',
            prompt:
              'Get the orders summary and show me a pie chart of order distribution by category',
          },
          {
            title: 'Order Status',
            label: 'Status breakdown',
            prompt:
              'Fetch the orders summary and create a pie chart showing the breakdown of order statuses',
          },
        ],
      },
      tools: {
        frontendTools: chartTools,
      },
    },
  },
}

/**
 * Donut chart demo - proportions with center metric
 */
export const DonutChart: Story = () => <Chat />
DonutChart.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Orders Analytics',
        subtitle: 'Donut Chart Visualizations',
        suggestions: [
          {
            title: 'Revenue Share',
            label: 'Donut with total',
            prompt:
              'Get the orders summary and show a donut chart of revenue by category with the total revenue in the center',
          },
          {
            title: 'Order Completion',
            label: 'Status donut',
            prompt:
              'Fetch the orders summary and create a donut chart showing order status distribution with completion rate in the center',
          },
        ],
      },
      tools: {
        frontendTools: chartTools,
      },
    },
  },
}

/**
 * Scatter chart demo - showing correlation
 */
export const ScatterChart: Story = () => <Chat />
ScatterChart.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Orders Analytics',
        subtitle: 'Scatter Chart Visualizations',
        suggestions: [
          {
            title: 'Orders vs Revenue',
            label: 'Correlation',
            prompt:
              'Get the orders summary and create a scatter chart showing the relationship between order count and revenue by region',
          },
          {
            title: 'Category Performance',
            label: 'Orders vs AOV',
            prompt:
              'Fetch the orders summary and show a scatter plot of order count vs average order value for each category',
          },
        ],
      },
      tools: {
        frontendTools: chartTools,
      },
    },
  },
}

/**
 * Radar chart demo - multi-dimensional comparison
 */
export const RadarChart: Story = () => <Chat />
RadarChart.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Orders Analytics',
        subtitle: 'Radar Chart Visualizations',
        suggestions: [
          {
            title: 'Regional Performance',
            label: 'Radar chart',
            prompt:
              'Get the orders summary and create a radar chart comparing regions across orders, revenue, and average order value',
          },
          {
            title: 'Category Analysis',
            label: 'Multi-metric',
            prompt:
              'Fetch the orders summary and show a radar chart comparing categories by order volume, revenue share, and growth',
          },
        ],
      },
      tools: {
        frontendTools: chartTools,
      },
    },
  },
}

/**
 * All chart types overview
 */
export const AllCharts: Story = () => <Chat />
AllCharts.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Orders Dashboard',
        subtitle: 'Explore order analytics with different chart types',
        suggestions: [
          {
            title: 'Revenue Trend',
            label: 'Line chart',
            prompt:
              'Get the monthly orders summary and show me a line chart of revenue trends',
          },
          {
            title: 'Category Breakdown',
            label: 'Bar chart',
            prompt:
              'Fetch the orders summary and create a bar chart of orders by category',
          },
          {
            title: 'Status Distribution',
            label: 'Pie chart',
            prompt:
              'Get the orders summary and show a pie chart of order status distribution',
          },
        ],
      },
      tools: {
        frontendTools: chartTools,
      },
    },
  },
}
