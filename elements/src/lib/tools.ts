import { JSONSchema7 } from 'ai'
import {
  AssistantToolProps,
  Tool,
  makeAssistantTool,
} from '@assistant-ui/react'
import z from 'zod'
import { FC } from 'react'

/**
 * Converts from assistant-ui tool format to the AI SDK tool shape
 */
export const toAISDKTools = (tools: Record<string, Tool>) => {
  return Object.fromEntries(
    Object.entries(tools).map(([name, tool]) => [
      name,
      {
        ...(tool.description ? { description: tool.description } : undefined),
        parameters: (tool.parameters instanceof z.ZodType
          ? z.toJSONSchema(tool.parameters)
          : tool.parameters) as JSONSchema7,
      },
    ])
  )
}

/**
 * Returns only frontend tools that are enabled
 */
export const getEnabledTools = (tools: Record<string, Tool>) => {
  return Object.fromEntries(
    Object.entries(tools).filter(
      ([, tool]) => !tool.disabled && tool.type !== 'backend'
    )
  )
}

/**
 * A frontend tool is a tool that is defined by the user and can be used in the chat.
 */
export type FrontendTool<TArgs extends Record<string, unknown>, TResult> = FC<
  AssistantToolProps<TArgs, TResult>
> & {
  unstable_tool: AssistantToolProps<TArgs, TResult>
}
/**
 * Make a frontend tool
 */
export const defineFrontendTool = <
  TArgs extends Record<string, unknown>,
  TResult,
>(
  tool: Tool,
  name: string
): FrontendTool<TArgs, TResult> => {
  return makeAssistantTool({
    ...tool,
    toolName: name,
  })
}
