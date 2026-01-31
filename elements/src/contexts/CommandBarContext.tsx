'use client'

import * as React from 'react'
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { WrenchIcon } from 'lucide-react'
import type {
  CommandBarAction,
  CommandBarConfig,
  CommandBarContextType,
  ElementsContextType,
} from '@/types'
import { useCommandBarShortcut } from '@/hooks/useCommandBarShortcut'
import { ElementsContext } from './contexts'

const CommandBarContext = createContext<CommandBarContextType | undefined>(
  undefined
)

export interface CommandBarProviderProps {
  children: ReactNode
  config?: CommandBarConfig
}

/**
 * Convert a snake_case or camelCase tool name into a human-readable label.
 * e.g. "get_user_profile" → "Get User Profile"
 *      "listAllDeployments" → "List All Deployments"
 */
function humanizeToolName(name: string): string {
  return name
    .replace(/_/g, ' ')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/\b\w/g, (c) => c.toUpperCase())
}

export function CommandBarProvider({
  children,
  config = {},
}: CommandBarProviderProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [dynamicActions, setDynamicActions] = useState<CommandBarAction[]>([])
  const nextIdRef = useRef(0)

  // Safely read ElementsContext — may not exist if used standalone
  let elementsContext: ElementsContextType | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    elementsContext = useContext(ElementsContext)
  } catch {
    elementsContext = undefined
  }

  const open = useCallback(() => {
    setIsOpen(true)
    setQuery('')
  }, [])

  const close = useCallback(() => {
    setIsOpen(false)
    setQuery('')
  }, [])

  const toggle = useCallback(() => {
    setIsOpen((prev) => {
      if (!prev) setQuery('')
      return !prev
    })
  }, [])

  // Keyboard shortcut
  const shortcut = config.shortcut ?? 'meta+k'
  useCommandBarShortcut(shortcut, toggle)

  // Register/unregister dynamic actions
  const registerActions = useCallback(
    (actions: CommandBarAction[]): (() => void) => {
      const batchId = nextIdRef.current++
      const taggedActions = actions.map((a) => ({
        ...a,
        _batchId: batchId,
      }))

      setDynamicActions((prev) => {
        const existing = new Map(prev.map((a) => [a.id, a]))
        taggedActions.forEach((a) => existing.set(a.id, a))
        return Array.from(existing.values())
      })

      return () => {
        const ids = new Set(actions.map((a) => a.id))
        setDynamicActions((prev) => prev.filter((a) => !ids.has(a.id)))
      }
    },
    []
  )

  const surfaceTools = config.surfaceMCPTools ?? true

  // Surface MCP tools as actions (enabled by default)
  const mcpToolActions: CommandBarAction[] = useMemo(() => {
    if (!surfaceTools || !elementsContext?.mcpTools) return []

    return Object.entries(elementsContext.mcpTools).map(([name, tool]) => {
      const typedTool = tool as {
        description?: string
        inputSchema?: Record<string, unknown>
        parameters?: Record<string, unknown>
        execute?: (
          args: Record<string, unknown>,
          options?: unknown
        ) => Promise<unknown>
      }

      // MCP tools use `inputSchema`, AI SDK tools use `parameters`
      const schema = typedTool.inputSchema ?? typedTool.parameters

      return {
        id: `mcp-tool-${name}`,
        label: humanizeToolName(name),
        description: typedTool.description ?? undefined,
        group: 'Tools',
        icon: React.createElement(WrenchIcon, { className: 'size-4' }),
        keywords: [name],
        onSelect: `Use the ${name} tool`,
        priority: -1,
        toolMeta: typedTool.execute
          ? {
              toolName: name,
              parameters: (schema ?? {
                type: 'object',
                properties: {},
              }) as Record<string, unknown>,
              description: typedTool.description,
              execute: (args: Record<string, unknown>) =>
                typedTool.execute!(args, {
                  toolCallId: crypto.randomUUID(),
                  abortSignal: new AbortController().signal,
                }),
              source: 'mcp' as const,
            }
          : undefined,
      }
    })
  }, [surfaceTools, elementsContext?.mcpTools])

  // Surface frontend tools as actions
  const frontendToolActions: CommandBarAction[] = useMemo(() => {
    if (!surfaceTools) return []
    const frontendTools = elementsContext?.config.tools?.frontendTools
    if (!frontendTools) return []

    return Object.entries(frontendTools).map(([name, tool]) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const unstable = (tool as any).unstable_tool as
        | {
            description?: string
            parameters?: unknown
            execute?: (
              args: Record<string, unknown>,
              context: unknown
            ) => Promise<unknown>
          }
        | undefined

      return {
        id: `frontend-tool-${name}`,
        label: humanizeToolName(name),
        description: unstable?.description ?? undefined,
        group: 'Tools',
        icon: React.createElement(WrenchIcon, { className: 'size-4' }),
        keywords: [name],
        onSelect: `Use the ${name} tool`,
        priority: -1,
        toolMeta: unstable?.execute
          ? {
              toolName: name,
              parameters: (unstable.parameters ?? {
                type: 'object',
                properties: {},
              }) as Record<string, unknown>,
              description: unstable.description,
              execute: (args: Record<string, unknown>) =>
                unstable.execute!(args, {
                  toolCallId: crypto.randomUUID(),
                }),
              source: 'frontend' as const,
            }
          : undefined,
      }
    })
  }, [surfaceTools, elementsContext?.config.tools?.frontendTools])

  // Merge all actions: static + dynamic + tools, sorted by priority
  const allActions = useMemo(() => {
    const merged = [
      ...(config.actions ?? []),
      ...dynamicActions,
      ...mcpToolActions,
      ...frontendToolActions,
    ]
    return merged.sort((a, b) => (b.priority ?? 0) - (a.priority ?? 0))
  }, [config.actions, dynamicActions, mcpToolActions, frontendToolActions])

  const value = useMemo<CommandBarContextType>(
    () => ({
      isOpen,
      open,
      close,
      toggle,
      query,
      setQuery,
      actions: allActions,
      registerActions,
      config,
    }),
    [isOpen, open, close, toggle, query, allActions, registerActions, config]
  )

  return (
    <CommandBarContext.Provider value={value}>
      {children}
    </CommandBarContext.Provider>
  )
}

export function useCommandBar(): CommandBarContextType {
  const context = useContext(CommandBarContext)
  if (!context) {
    throw new Error('useCommandBar must be used within a CommandBarProvider')
  }
  return context
}
