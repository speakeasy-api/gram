import {
  CodeHeaderProps,
  SyntaxHighlighterProps,
} from '@assistant-ui/react-markdown'
import { ComponentType } from 'react'

/**
 * A plugin enables addition of custom rendering capabilities to the Elements library.
 * For example, a plugin could provide a custom renderer for a specific language such as
 * D3.js or Mermaid.
 *
 * The general flow of a plugin is:
 * 1. Plugin extends the system prompt with a custom prompt instructing the LLM to return code fences marked with the specified language / format
 * 2. The LLM returns a code fence marked with the specified language / format
 * 3. The code fence is rendered using the custom renderer
 */
export interface Plugin {
  /**
   * Any prompt that the plugin may need to add to the system prompt.
   * Will be appended to the built-in system prompt.
   *
   * @example
   * ```
   * If the user asks for a chart, use D3 to render it.
   * Return only a d3 code block. The code will execute in a sandboxed environment where:
   * - \`d3\` is the D3 library
   * - \`container\` is the DOM element to render into (use \`d3.select(container)\` NOT \`d3.select('body')\`)
   * The code should be wrapped in a \`\`\`d3
   * \`\`\` block.
   * ```
   */
  prompt: string

  /**
   * The language identifier for the syntax highlighter
   * e.g mermaid or d3
   *
   * Does not need to be an official language identifier, can be any string. The important part is that the
   * prompt adequately instructs the LLM to return code fences marked with the specified language / format
   *
   * @example
   * ```
   * d3
   * ```
   */
  language: string

  /**
   * The component to use for the syntax highlighter.
   */
  Component: ComponentType<SyntaxHighlighterProps>

  /**
   * The component to use for the code header.
   * Will be rendered above the code block.
   * @default () => null
   */
  Header?: ComponentType<CodeHeaderProps> | undefined

  /**
   * Whether to override existing plugins with the same language.
   * @default false
   */
  overrideExisting?: boolean
}
