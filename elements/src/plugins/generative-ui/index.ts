import { Plugin } from '@/types/plugins'
import { GenerativeUIRenderer } from './component'

/**
 * This plugin renders json-render UI trees as dynamic widgets.
 * Use the language identifier 'ui' or 'json-render' in code blocks.
 */
export const generativeUI: Plugin = {
  language: 'ui',
  prompt: `You can render rich UI for structured data using \`\`\`ui code blocks containing valid JSON (no comments, no trailing commas).

Components: Card{title?}, Grid{columns?}, Stack{direction?:"vertical"|"horizontal"}, Metric{label,value,format?:"currency"|"percent"|"number"}, Table{headers[],rows[][]}, Text{content,variant?:"heading"|"body"|"caption"|"code"}, Badge{content,variant?:"default"|"success"|"warning"|"error"}, Progress{value,max?,label?}, List{items[],ordered?}, Divider, ActionButton{label,action,args?,variant?}

Structure: {"type":"ComponentName","props":{...},"children":[...]}

Example:
\`\`\`ui
{"type":"Card","props":{"title":"Overview"},"children":[{"type":"Grid","props":{"columns":2},"children":[{"type":"Metric","props":{"label":"Sales","value":50000,"format":"currency"}},{"type":"Metric","props":{"label":"Growth","value":0.12,"format":"percent"}}]}]}
\`\`\`

Use for: tool results, dashboards, tables, metrics. Skip for: simple text, errors, single values.`,
  Component: GenerativeUIRenderer,
  Header: undefined,
}
