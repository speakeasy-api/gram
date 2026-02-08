import { Plugin } from '@/types/plugins'
import { GenerativeUIRenderer } from './component'

/**
 * This plugin renders json-render UI trees as dynamic widgets.
 * Use the language identifier 'ui' or 'json-render' in code blocks.
 */
export const generativeUI: Plugin = {
  language: 'ui',
  prompt: `WHEN TO USE UI VISUALIZATION:
Proactively render tool results and structured data as visual UI components whenever it would improve comprehension. Use the 'ui' code block format for:
- Tool results that return structured data (lists, objects, metrics)
- Data that benefits from visual hierarchy (dashboards, summaries)
- Information with multiple related fields (user profiles, order details)
- Anything with numbers, statuses, or progress that can be visualized
- Results that would otherwise be displayed as raw JSON or verbose text

Do NOT use UI for: simple text responses, single values, error messages, or when the user explicitly asks for raw data.

To render UI, output a json-render specification in a code block with the language identifier 'ui'.

CRITICAL JSON REQUIREMENTS:
- The code block MUST contain ONLY valid, parseable JSON
- NO comments (no // or /* */ anywhere)
- NO trailing commas
- Use double quotes for all strings and keys
- The JSON must start with { and end with }

AVAILABLE COMPONENTS:

Card - Container with optional title
  props: { title?: string }
  children: any components

Grid - Multi-column layout
  props: { columns?: number } (default: 2)
  children: any components

Stack - Vertical or horizontal flex layout
  props: { direction?: "vertical" | "horizontal" } (default: "vertical")
  children: any components

Metric - Displays a label and formatted value
  props: { label: string, value: number, format?: "currency" | "percent" | "number" }

Table - Data table
  props: { headers: string[], rows: any[][] }

Text - Text with variants
  props: { content: string, variant?: "heading" | "body" | "caption" | "code" }

Badge - Status badge
  props: { content: string, variant?: "default" | "success" | "warning" | "error" }

Progress - Progress bar
  props: { value: number, max?: number, label?: string }

List - Bullet or numbered list
  props: { items: string[], ordered?: boolean }

Divider - Horizontal line separator

ActionButton - Interactive button that triggers a tool call
  props: {
    label: string,           // Button text
    action: string,          // Tool name to invoke when clicked
    args?: object,           // Arguments to pass to the tool
    variant?: "default" | "secondary" | "outline" | "destructive"
  }
  NOTE: Only use ActionButton with tools you know are available

STRUCTURE:
Every UI spec is a tree with:
- "type": component name (required)
- "props": component properties (optional)
- "children": array of child components (optional)

EXAMPLE - DASHBOARD:
{
  "type": "Card",
  "props": { "title": "Sales Overview" },
  "children": [
    {
      "type": "Grid",
      "props": { "columns": 3 },
      "children": [
        { "type": "Metric", "props": { "label": "Revenue", "value": 125000, "format": "currency" } },
        { "type": "Metric", "props": { "label": "Growth", "value": 0.15, "format": "percent" } },
        { "type": "Metric", "props": { "label": "Orders", "value": 1420, "format": "number" } }
      ]
    },
    { "type": "Divider" },
    { "type": "Progress", "props": { "label": "Q1 Target", "value": 75, "max": 100 } }
  ]
}

EXAMPLE - TABLE:
{
  "type": "Card",
  "props": { "title": "Users" },
  "children": [
    {
      "type": "Table",
      "props": {
        "headers": ["Name", "Email", "Status"],
        "rows": [
          ["Alice", "alice@example.com", "Active"],
          ["Bob", "bob@example.com", "Pending"]
        ]
      }
    }
  ]
}

EXAMPLE - WITH ACTIONS:
{
  "type": "Card",
  "props": { "title": "Pending Request #123" },
  "children": [
    { "type": "Text", "props": { "variant": "body" }, "children": [{ "type": "Text", "props": {}, "children": [] }] },
    {
      "type": "Stack",
      "props": { "direction": "horizontal" },
      "children": [
        { "type": "ActionButton", "props": { "label": "Approve", "action": "approve_request", "args": { "id": 123 } } },
        { "type": "ActionButton", "props": { "label": "Reject", "action": "reject_request", "args": { "id": 123 }, "variant": "destructive" } }
      ]
    }
  ]
}

STYLE GUIDELINES:
- Prefer spacious, breathable layouts with adequate visual hierarchy
- Use Grid with 2-3 columns max for metrics; avoid cramming too many items
- Group related content in Cards with clear titles
- Use Dividers to separate logical sections
- Balance information density: show what matters, hide the noise
- For dashboards, lead with the most important metrics at the top

CONTENT GUIDELINES:
- Outside the code block, provide context and insights about the data
- Do not describe technical implementation details
- Focus on what the data means, not how it's displayed

ACTION RESULT HANDLING:
When you receive a message starting with "[Action completed]" or "[Action failed]", the user clicked an action button and the tool has already been executed. Provide a brief, friendly acknowledgment of what happened. Keep your response concise - one sentence is usually enough. Do not re-execute the action or ask if they want to do something they just did.

Examples:
- "[Action completed] approve_request: Request #123 approved" → "Done! Request #123 has been approved."
- "[Action failed] delete_task: Permission denied" → "I couldn't delete that task - looks like you don't have permission."`,
  Component: GenerativeUIRenderer,
  Header: undefined,
}

// Re-export individual UI components
export { ActionButton, type ActionButtonProps } from './ActionButton'
export { Badge, type BadgeProps } from './Badge'
export { Card, type CardProps } from './Card'
export { Divider, type DividerProps } from './Divider'
export { Grid, type GridProps } from './Grid'
export { List, type ListProps } from './List'
export { Metric, type MetricProps } from './Metric'
export { Progress, type ProgressProps } from './Progress'
export { Stack, type StackProps } from './Stack'
export { Table, type TableProps } from './Table'
export { Text, type TextProps } from './Text'
