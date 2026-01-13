import { Plugin } from '@/types/plugins'
import { ChartRenderer } from './component'

/**
 * This plugin renders Vega charts.
 */
export const chart: Plugin = {
  language: 'vega',
  prompt: `When a user requests a chart or visualization, respond with a valid Vega specification (https://vega.github.io/vega/) in a code block annotated with the language identifier 'vega'.

CRITICAL JSON REQUIREMENTS:
- The code block MUST contain ONLY valid, parseable JSON
- NO comments (no // or /* */ anywhere)
- NO trailing commas
- Use double quotes for all strings and keys
- NO text before or after the JSON object
- The JSON must start with { and end with }

CONTENT GUIDELINES:
- Outside the code block, describe trends and insights found in the data
- Do not describe visual properties or technical implementation details
- Do not mention "Vega" or other technical terms - this is user-facing

The Vega spec will be parsed with JSON.parse() - if it fails, the chart will error. Ensure strict JSON validity.

REQUIRED STRUCTURE:

Every spec needs: "$schema", "width", "height", "data", "scales", "marks". Include "padding" (5 or object) and "axes" for readability.

Data format:
{"name": "table", "values": [{"category": "A", "amount": 28}]}

SCALES - Choose the right type:
- "band": categorical x-axis (bar charts) - domain from data field, range: "width", padding: 0.1
- "linear": numerical axes - domain from data field, range: "width"/"height", nice: true
- "time"/"utc": temporal data
- "ordinal": for colors use range: {"scheme": "category10"} or range: ["#1f77b4", "#ff7f0e", "#2ca02c"]

MARKS - Common types:
- "rect": bar charts (requires x, width, y, y2)
- "line": time series (requires x, y)
- "area": filled areas (requires x, y, y2)
- "symbol": scatter plots (requires x, y)

CHART PATTERNS:

Bar: band scale (x) + linear scale (y) + rect marks. Set y2: {"scale": "yscale", "value": 0}
Line: linear/point scale (x) + linear scale (y) + line mark. Add "interpolate": "monotone"
Scatter: linear scales (both) + symbol marks
Area: like line but use area mark with y2: {"scale": "yscale", "value": 0}
Stacked: add transform [{"type": "stack", "groupby": ["x"], "field": "y"}], use y0/y1 fields

CRITICAL RULES:

1. Data must contain at least one record with valid (non-null) values for ALL fields used in scales
2. ONLY reference fields that actually exist in your data - never use datum.meta, datum.id, or any field not in your values array
3. Always include y2 for rect/area marks (or bars/areas have zero height)
4. Use "band" for categories, not "linear"
5. For position scales use "range": "width" or "height". For color scales NEVER use "range": "category10" - use "range": {"scheme": "category10"} or an array
6. Match scale/data names exactly between definition and usage
7. Include "from": {"data": "dataName"} on marks
8. Add padding to prevent label cutoff

EXAMPLE: COMPLETE BAR CHART

{
  "$schema": "https://vega.github.io/schema/vega/v5.json",
  "width": 500,
  "height": 300,
  "padding": 5,
  "data": [
    {
      "name": "table",
      "values": [
        {"category": "A", "amount": 28},
        {"category": "B", "amount": 55},
        {"category": "C", "amount": 43}
      ]
    }
  ],
  "scales": [
    {
      "name": "xscale",
      "type": "band",
      "domain": {"data": "table", "field": "category"},
      "range": "width",
      "padding": 0.1
    },
    {
      "name": "yscale",
      "type": "linear",
      "domain": {"data": "table", "field": "amount"},
      "range": "height",
      "nice": true
    }
  ],
  "axes": [
    {"scale": "xscale", "orient": "bottom"},
    {"scale": "yscale", "orient": "left", "title": "Amount"}
  ],
  "marks": [
    {
      "type": "rect",
      "from": {"data": "table"},
      "encode": {
        "enter": {
          "x": {"scale": "xscale", "field": "category"},
          "width": {"scale": "xscale", "band": 1},
          "y": {"scale": "yscale", "field": "amount"},
          "y2": {"scale": "yscale", "value": 0},
          "fill": {"value": "steelblue"}
        }
      }
    }
  ]
}`,
  Component: ChartRenderer,
  Header: undefined,
}
