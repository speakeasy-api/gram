import { JSONSchema7, ToolSet, type ToolCallOptions } from 'ai'
import {
  AssistantToolProps,
  Tool,
  makeAssistantTool,
} from '@assistant-ui/react'
import z from 'zod'
import { FC } from 'react'
import { ApprovalType } from '@/types'

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

/**
 * Helpers for requesting and tracking tool approval state.
 */
export interface ApprovalHelpers {
  requestApproval: (
    toolName: string,
    toolCallId: string,
    args: unknown
  ) => Promise<boolean>
  isToolApproved: (toolName: string) => boolean
  markToolApproved: (toolName: string) => void
}

/**
 * Wraps tools with approval logic based on the approval config.
 */
export function wrapToolsWithApproval(
  tools: ToolSet,
  approvalConfig: Record<string, ApprovalType> | undefined,
  approvalHelpers: ApprovalHelpers
): ToolSet {
  if (!approvalConfig) {
    return tools
  }

  return Object.fromEntries(
    Object.entries(tools).map(([name, tool]) => {
      const approvalType = approvalConfig[name]

      if (!approvalType || approvalType === 'never') {
        return [name, tool]
      }

      const originalExecute = tool.execute
      if (!originalExecute) {
        return [name, tool]
      }

      return [
        name,
        {
          ...tool,
          execute: async (args: unknown, options?: ToolCallOptions) => {
            // Use type assertion similar to useExecuteToolWithApproval
            const opts = (options ?? {}) as Parameters<
              typeof originalExecute
            >[1]
            // Extract toolCallId from options
            const toolCallId =
              (opts as { toolCallId?: string }).toolCallId ?? ''

            // Check if already approved (for "once" type)
            if (
              approvalType === 'once' &&
              approvalHelpers.isToolApproved(name)
            ) {
              return originalExecute(
                args,
                opts as Parameters<typeof originalExecute>[1]
              )
            }

            // Request approval using the actual toolCallId from the stream
            const approved = await approvalHelpers.requestApproval(
              name,
              toolCallId,
              args
            )

            if (!approved) {
              return {
                content: [
                  {
                    type: 'text',
                    text: `Tool "${name}" execution was denied by the user. Please acknowledge this and continue without using this tool's result.`,
                  },
                ],
                isError: true,
              }
            }

            // Mark as approved for "once" type
            if (approvalType === 'once') {
              approvalHelpers.markToolApproved(name)
            }

            return originalExecute(
              args,
              opts as Parameters<typeof originalExecute>[1]
            )
          },
        },
      ]
    })
  ) as ToolSet
}
