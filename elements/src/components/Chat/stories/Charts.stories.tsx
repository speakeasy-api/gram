import type { Meta, StoryFn } from '@storybook/react-vite'
import { Chat } from '..'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Plugins/Charts',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

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
    },
  },
}
