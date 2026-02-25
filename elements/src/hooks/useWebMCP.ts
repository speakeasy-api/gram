import { useEffect, useRef } from 'react'

interface WebMcpToolDescriptor {
  name: string
  description: string
  inputSchema: Record<string, unknown>
  execute: (
    args: Record<string, unknown>,
    agent?: unknown
  ) => unknown | Promise<unknown>
}

declare global {
  interface Navigator {
    modelContext?: {
      provideContext(context: { tools: WebMcpToolDescriptor[] }): void
      registerTool(tool: WebMcpToolDescriptor): void
      unregisterTool(name: string): void
    }
  }
}

interface UseWebMCPOptions {
  enabled: boolean
  mcpUrl: string | undefined
  // Mutable headers ref shared with the MCP transport — the same object is
  // mutated in-place by ElementsProvider, so the reference is stable across
  // renders and the execute callback always reads the latest headers.
  mcpHeaders: Record<string, string>
  tools: Record<string, unknown> | undefined
}

let warnedOnce = false

export function useWebMCP({
  enabled,
  mcpUrl,
  mcpHeaders,
  tools,
}: UseWebMCPOptions) {
  const registeredToolsRef = useRef<string[]>([])
  const idCounterRef = useRef(0)
  // Keep a stable ref to mcpHeaders so the execute closure always reads fresh
  // values without needing mcpHeaders as an effect dependency.
  const headersRef = useRef(mcpHeaders)
  headersRef.current = mcpHeaders

  useEffect(() => {
    if (!enabled || !tools || !mcpUrl) return

    if (!navigator.modelContext) {
      if (!warnedOnce) {
        console.warn(
          '[Gram WebMCP] navigator.modelContext is not available. Tool registration skipped.'
        )
        warnedOnce = true
      }
      return
    }

    const url = mcpUrl
    const toolNames: string[] = []

    for (const [name, tool] of Object.entries(tools)) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const t = tool as any
      const description = t?.description ?? ''
      const inputSchema = t?.parameters ?? {}

      const descriptor: WebMcpToolDescriptor = {
        name,
        description,
        inputSchema,
        execute: async (args: Record<string, unknown>) => {
          const id = ++idCounterRef.current
          const response = await fetch(url, {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              ...headersRef.current,
            },
            body: JSON.stringify({
              jsonrpc: '2.0',
              id,
              method: 'tools/call',
              params: { name, arguments: args },
            }),
          })

          if (!response.ok) {
            throw new Error(`MCP request failed: ${response.status}`)
          }

          const result = await response.json()
          if (result.error) {
            throw new Error(result.error.message || 'MCP tool call error')
          }
          return result.result
        },
      }

      navigator.modelContext.registerTool(descriptor)
      toolNames.push(name)
    }

    registeredToolsRef.current = toolNames

    return () => {
      if (!navigator.modelContext) return
      for (const toolName of registeredToolsRef.current) {
        navigator.modelContext.unregisterTool(toolName)
      }
      registeredToolsRef.current = []
    }
  }, [enabled, mcpUrl, tools])
}
