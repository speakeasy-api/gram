import { Plugin } from '@/types/plugins'
import { ChartRenderer } from './component'

/**
 * This plugin renders Vega charts.
 */
export const chart: Plugin = {
  language: 'vega',
  prompt: `When a user requests a chart or visualization, respond with a valid Vega specification (https://vega.github.io/vega/) in a code block with language 'vega'.

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

The Vega spec will be parsed with JSON.parse() - if it fails, the chart will error. Ensure strict JSON validity.`,
  Component: ChartRenderer,
  Header: undefined,
}
