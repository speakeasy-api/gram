import { Plugin } from '@/types/plugins'
import { ChartRenderer } from './component'

/**
 * This plugin renders charts using json-render format.
 */
export const chart: Plugin = {
  language: 'chart',
  prompt: `WHEN TO USE CHARTS:
Create charts to visualize numerical data when it helps users understand patterns, trends, or comparisons. Use the 'chart' code block format.

CRITICAL JSON REQUIREMENTS:
- The code block MUST contain ONLY valid, parseable JSON
- NO comments (no // or /* */ anywhere)
- NO trailing commas
- Use double quotes for all strings and keys
- The JSON must start with { and end with }

AVAILABLE CHART TYPES:

BarChart - Compare categorical data
  props: {
    title?: string,
    data: [{ label: string, value: number, color?: string }],
    layout?: "vertical" | "horizontal",
    showGrid?: boolean,
    showLegend?: boolean
  }

LineChart - Show trends over time
  props: {
    title?: string,
    data: [{ label: string, [series]: number }],
    series?: string[],
    showGrid?: boolean,
    showLegend?: boolean,
    showDots?: boolean,
    curved?: boolean
  }

AreaChart - Show volume over time
  props: {
    title?: string,
    data: [{ label: string, [series]: number }],
    series?: string[],
    stacked?: boolean,
    showGrid?: boolean,
    showLegend?: boolean
  }

PieChart - Show proportions (2-6 categories)
  props: {
    title?: string,
    data: [{ label: string, value: number, color?: string }],
    showLabels?: boolean,
    showLegend?: boolean
  }

DonutChart - Proportions with center metric
  props: {
    title?: string,
    data: [{ label: string, value: number, color?: string }],
    showLabels?: boolean,
    showLegend?: boolean,
    innerLabel?: string,
    innerValue?: string | number
  }

ScatterChart - Show correlation
  props: {
    title?: string,
    data: [{ x: number, y: number, label?: string, size?: number, color?: string }],
    xLabel?: string,
    yLabel?: string,
    showGrid?: boolean
  }

RadarChart - Compare multiple attributes (3-8 dimensions)
  props: {
    title?: string,
    data: [{ label: string, value: number }],
    showLegend?: boolean
  }

STRUCTURE:
{
  "type": "ChartType",
  "props": { ... }
}

EXAMPLE - BAR CHART:
{
  "type": "BarChart",
  "props": {
    "title": "Sales by Region",
    "data": [
      { "label": "North", "value": 120000 },
      { "label": "South", "value": 98000 },
      { "label": "East", "value": 145000 },
      { "label": "West", "value": 87000 }
    ]
  }
}

EXAMPLE - LINE CHART (multiple series):
{
  "type": "LineChart",
  "props": {
    "title": "Monthly Revenue",
    "data": [
      { "label": "Jan", "revenue": 45000, "costs": 32000 },
      { "label": "Feb", "revenue": 52000, "costs": 35000 },
      { "label": "Mar", "revenue": 61000, "costs": 38000 }
    ],
    "series": ["revenue", "costs"]
  }
}

EXAMPLE - PIE CHART:
{
  "type": "PieChart",
  "props": {
    "title": "Market Share",
    "data": [
      { "label": "Product A", "value": 45 },
      { "label": "Product B", "value": 30 },
      { "label": "Product C", "value": 25 }
    ]
  }
}

EXAMPLE - DONUT CHART:
{
  "type": "DonutChart",
  "props": {
    "title": "Budget Allocation",
    "data": [
      { "label": "Marketing", "value": 35 },
      { "label": "Engineering", "value": 45 },
      { "label": "Operations", "value": 20 }
    ],
    "innerValue": "$1.2M",
    "innerLabel": "Total Budget"
  }
}

CHART SELECTION GUIDE:
- Comparing categories → BarChart
- Trends over time → LineChart
- Volume/magnitude over time → AreaChart
- Part-to-whole (2-6 items) → PieChart or DonutChart
- Correlation between variables → ScatterChart
- Multi-dimensional comparison → RadarChart

CONTENT GUIDELINES:
- Outside the code block, describe trends and insights found in the data
- Do not describe visual properties or technical implementation details
- Focus on what the data means, not how it's displayed`,
  Component: ChartRenderer,
  Header: undefined,
}
