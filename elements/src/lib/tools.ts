import type { ToolsFilter } from '@/types'
import {
  AssistantToolProps,
  Tool,
  makeAssistantTool,
} from '@assistant-ui/react'
import { JSONSchema7, ToolSet, type ToolCallOptions } from 'ai'
import { FC } from 'react'
import z from 'zod'

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
 * Module-level approval config that gets set by ElementsProvider at runtime.
 * This allows defineFrontendTool to check approval status during execute.
 */
let approvalConfig: {
  helpers: ApprovalHelpers
  requiresApproval: (toolName: string) => boolean
} | null = null

/**
 * Sets the approval configuration. Called by ElementsProvider.
 */
export function setFrontendToolApprovalConfig(
  helpers: ApprovalHelpers,
  toolsRequiringApproval: ToolsFilter
): void {
  const requiresApproval = createRequiresApprovalFn(toolsRequiringApproval)
  approvalConfig = {
    helpers,
    requiresApproval,
  }
}

/**
 * Clears the approval configuration. Called when ElementsProvider unmounts.
 */
export function clearFrontendToolApprovalConfig(): void {
  approvalConfig = null
}

/**
 * Creates a function that checks if a tool requires approval.
 * Handles both array and function-based configurations.
 */
function createRequiresApprovalFn(
  toolsRequiringApproval: ToolsFilter | undefined
): (toolName: string) => boolean {
  if (!toolsRequiringApproval) {
    return () => false
  }

  if (typeof toolsRequiringApproval === 'function') {
    return (toolName: string) => toolsRequiringApproval({ toolName })
  }

  const approvalSet = new Set(toolsRequiringApproval)
  return (toolName: string) => approvalSet.has(toolName)
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
  type ToolExecutionContext = Parameters<
    NonNullable<Tool<Record<string, unknown>, void>['execute']>
  >[1]
  return makeAssistantTool({
    ...tool,
    execute: async (args: TArgs, context: ToolExecutionContext) => {
      // Check if this tool requires approval at runtime
      if (approvalConfig?.requiresApproval(name)) {
        const { helpers } = approvalConfig
        const toolCallId = context.toolCallId ?? ''

        // Check if already approved (user chose "Approve always" previously)
        if (!helpers.isToolApproved(name)) {
          const approved = await helpers.requestApproval(name, toolCallId, args)

          if (!approved) {
            return {
              content: [
                {
                  type: 'text',
                  text: `Tool "${name}" execution was denied by the user. Please acknowledge this and continue without using this tool's result.`,
                },
              ],
              isError: true,
            } as TResult
          }
        }
      }

      return tool.execute?.(args, context)
    },
    toolName: name,
  } as AssistantToolProps<TArgs, TResult>)
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
  whitelistTool: (toolName: string) => void
}

/**
 * Wraps tools with approval logic based on the approval config.
 */
export function wrapToolsWithApproval(
  tools: ToolSet,
  toolsRequiringApproval: ToolsFilter | undefined,
  approvalHelpers: ApprovalHelpers
): ToolSet {
  if (!toolsRequiringApproval) {
    return tools
  }

  // Handle empty array case
  if (
    Array.isArray(toolsRequiringApproval) &&
    toolsRequiringApproval.length === 0
  ) {
    return tools
  }

  const requiresApproval = createRequiresApprovalFn(toolsRequiringApproval)

  return Object.fromEntries(
    Object.entries(tools).map(([name, tool]) => {
      if (!requiresApproval(name)) {
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
            const opts = (options ?? {}) as Parameters<
              typeof originalExecute
            >[1]
            // Extract toolCallId from options
            const toolCallId =
              (opts as { toolCallId?: string }).toolCallId ?? ''

            // Check if already approved (user chose "Approve always" previously)
            if (approvalHelpers.isToolApproved(name)) {
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

            // Note: Tool is marked as approved via the UI when user clicks "Approve always"
            // (handled in tool-fallback.tsx via markToolApproved)

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
