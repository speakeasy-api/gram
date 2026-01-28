'use client'

import { createContext, useContext, useCallback, ReactNode } from 'react'

// Define a minimal tool type for direct execution
// This avoids strict AI SDK type requirements while still being type-safe
interface ExecutableTool {
  execute?: (args: unknown, options?: unknown) => Promise<unknown>
}

type ExecutableToolSet = Record<string, ExecutableTool | undefined>

export interface ToolExecutionResult {
  success: boolean
  result?: unknown
  error?: string
}

interface ToolExecutionContextValue {
  executeTool: (
    toolName: string,
    args: Record<string, unknown>
  ) => Promise<ToolExecutionResult>
  isToolAvailable: (toolName: string) => boolean
}

const ToolExecutionContext = createContext<ToolExecutionContextValue | null>(
  null
)

interface ToolExecutionProviderProps {
  children: ReactNode
  tools: ExecutableToolSet | undefined
}

export function ToolExecutionProvider({
  children,
  tools,
}: ToolExecutionProviderProps) {
  const executeTool = useCallback(
    async (
      toolName: string,
      args: Record<string, unknown>
    ): Promise<ToolExecutionResult> => {
      if (!tools) {
        return { success: false, error: 'Tools not available' }
      }

      const tool = tools[toolName]
      if (!tool) {
        return { success: false, error: `Tool "${toolName}" not found` }
      }

      if (!tool.execute) {
        return {
          success: false,
          error: `Tool "${toolName}" has no execute function`,
        }
      }

      try {
        // Generate a unique toolCallId for this execution
        const toolCallId = `action-${Date.now()}-${Math.random().toString(36).slice(2)}`
        const result = await tool.execute(args, { toolCallId, messages: [] })
        return { success: true, result }
      } catch (err) {
        const errorMessage =
          err instanceof Error ? err.message : 'Unknown error'
        return { success: false, error: errorMessage }
      }
    },
    [tools]
  )

  const isToolAvailable = useCallback(
    (toolName: string): boolean => {
      return !!tools?.[toolName]?.execute
    },
    [tools]
  )

  return (
    <ToolExecutionContext.Provider value={{ executeTool, isToolAvailable }}>
      {children}
    </ToolExecutionContext.Provider>
  )
}

export function useToolExecution(): ToolExecutionContextValue {
  const context = useContext(ToolExecutionContext)
  if (!context) {
    return {
      executeTool: async (): Promise<ToolExecutionResult> => ({
        success: false,
        error: 'ToolExecutionProvider not found',
      }),
      isToolAvailable: () => false,
    }
  }
  return context
}
