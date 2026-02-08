import { Plugin } from '@/types/plugins'
import { GenerativeUIRenderer } from './component'

/**
 * This plugin renders json-render UI trees as dynamic widgets.
 * Use the language identifier 'ui' or 'json-render' in code blocks.
 */
export const generativeUI: Plugin = {
  language: 'ui',
  prompt: `Render structured data as visual UI using \`\`\`ui code blocks with valid JSON.

Components:
- Card{title?} - container with border
- Grid{columns?} - multi-column layout
- Stack{direction?} - vertical/horizontal flex
- Metric{label,value,format?} - formatted number display (currency/percent/number)
- Table{headers[],rows[][]} - data table
- Text{content,variant?} - heading/body/caption/code
- Badge{content,variant?} - default/success/warning/error
- Progress{value,max?,label?} - progress bar
- List{items[],ordered?} - bullet/numbered list
- Divider - horizontal separator
- ActionButton{label,action,args?} - triggers tool call

Format: {"type":"Name","props":{...},"children":[...]}

Example: {"type":"Card","props":{"title":"Stats"},"children":[{"type":"Metric","props":{"label":"Revenue","value":50000,"format":"currency"}}]}

Use for dashboards, tables, metrics from tool results. Skip for simple text or errors.`,
  Component: GenerativeUIRenderer,
  Header: undefined,
}
