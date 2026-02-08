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
 * Bar chart demo - comparing categorical data
 */
export const BarChart: Story = () => <Chat />
BarChart.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        title: 'Chart Demo',
        subtitle: 'Bar Chart Example',
        suggestions: [
          {
            title: 'Sales by Region',
            label: 'Bar chart',
            prompt:
              'Create a bar chart showing sales by region: North $120k, South $98k, East $145k, West $87k',
          },
          {
            title: 'Product Comparison',
            label: 'Horizontal bars',
            prompt:
              'Show me a horizontal bar chart comparing product ratings: Phone 4.5, Laptop 4.2, Tablet 3.9, Watch 4.7',
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
        title: 'Chart Demo',
        subtitle: 'Line Chart Example',
        suggestions: [
          {
            title: 'Monthly Revenue',
            label: 'Line chart',
            prompt:
              'Create a line chart showing monthly revenue: Jan $45k, Feb $52k, Mar $61k, Apr $58k, May $72k, Jun $68k',
          },
          {
            title: 'Revenue vs Costs',
            label: 'Multiple series',
            prompt:
              'Show a line chart with revenue and costs over 6 months. Revenue: 45k, 52k, 61k, 58k, 72k, 68k. Costs: 32k, 35k, 38k, 36k, 42k, 40k',
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
        title: 'Chart Demo',
        subtitle: 'Area Chart Example',
        suggestions: [
          {
            title: 'User Growth',
            label: 'Area chart',
            prompt:
              'Create an area chart showing user growth over 6 months: Jan 1000, Feb 1500, Mar 2200, Apr 3100, May 4500, Jun 6000',
          },
          {
            title: 'Traffic Sources',
            label: 'Stacked area',
            prompt:
              'Show a stacked area chart with traffic sources. Organic: 500, 600, 800, 950, 1100. Paid: 200, 350, 500, 700, 900. Direct: 300, 320, 350, 380, 420',
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
        title: 'Chart Demo',
        subtitle: 'Pie Chart Example',
        suggestions: [
          {
            title: 'Market Share',
            label: 'Pie chart',
            prompt:
              'Create a pie chart showing market share: Company A 35%, Company B 28%, Company C 22%, Others 15%',
          },
          {
            title: 'Expense Breakdown',
            label: 'Budget allocation',
            prompt:
              'Show expense breakdown as a pie chart: Salaries 45%, Marketing 20%, Operations 18%, R&D 12%, Other 5%',
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
        title: 'Chart Demo',
        subtitle: 'Donut Chart Example',
        suggestions: [
          {
            title: 'Budget Status',
            label: 'Donut with total',
            prompt:
              'Create a donut chart showing budget allocation with total $1.2M in the center: Marketing 35%, Engineering 45%, Operations 20%',
          },
          {
            title: 'Project Status',
            label: 'Task completion',
            prompt:
              'Show project status as a donut chart with "75% Complete" in the center: Done 75%, In Progress 15%, Todo 10%',
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
        title: 'Chart Demo',
        subtitle: 'Scatter Chart Example',
        suggestions: [
          {
            title: 'Price vs Rating',
            label: 'Correlation',
            prompt:
              'Create a scatter chart showing price vs customer rating for products: (10, 3.5), (25, 4.0), (50, 4.2), (75, 4.5), (100, 4.8), (150, 4.3)',
          },
          {
            title: 'Sales Performance',
            label: 'Calls vs Revenue',
            prompt:
              'Show a scatter plot of sales calls vs revenue closed: (20 calls, $5k), (35 calls, $12k), (50 calls, $18k), (45 calls, $15k), (60 calls, $25k)',
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
        title: 'Chart Demo',
        subtitle: 'Radar Chart Example',
        suggestions: [
          {
            title: 'Skill Assessment',
            label: 'Radar chart',
            prompt:
              'Create a radar chart for skill assessment: Communication 85, Technical 90, Leadership 75, Creativity 80, Problem Solving 88, Teamwork 92',
          },
          {
            title: 'Product Comparison',
            label: 'Feature scores',
            prompt:
              'Show a radar chart comparing product features: Performance 85, Reliability 90, Usability 75, Support 80, Price 70, Features 88',
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
        title: 'Chart Gallery',
        subtitle: 'Explore all chart types',
        suggestions: [
          {
            title: 'Bar Chart',
            label: 'Compare categories',
            prompt:
              'Create a bar chart showing quarterly sales: Q1 $250k, Q2 $310k, Q3 $280k, Q4 $390k',
          },
          {
            title: 'Line Chart',
            label: 'Show trends',
            prompt:
              'Create a line chart of monthly users: Jan 1000, Feb 1200, Mar 1800, Apr 2100, May 2500',
          },
          {
            title: 'Pie Chart',
            label: 'Show proportions',
            prompt:
              'Create a pie chart of traffic sources: Organic 45%, Direct 30%, Referral 15%, Social 10%',
          },
        ],
      },
    },
  },
}
