import { Plugin } from '@/types/plugins'
import { GenerativeUIRenderer } from './component'

/**
 * This plugin renders json-render UI trees as dynamic widgets.
 * Use the language identifier 'ui' or 'json-render' in code blocks.
 */
export const generativeUI: Plugin = {
  language: 'ui',
  prompt: `When a user requests data visualization, dashboards, or structured information display, respond with a json-render UI specification in a code block annotated with the language identifier 'ui'.

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

ACTION HANDLING:
When you receive a message in the format "[Action: tool_name] {args}", immediately call the specified tool with the provided arguments. Do not ask for confirmation - the user has already clicked the button to initiate this action. After the tool executes, provide a brief confirmation of what happened.`,
  Component: GenerativeUIRenderer,
  Header: undefined,
}
