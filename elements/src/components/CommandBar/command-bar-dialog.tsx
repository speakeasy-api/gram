'use client'

import * as React from 'react'
import { useCallback, useRef, useState } from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { Command as CommandPrimitive } from 'cmdk'
import { cn } from '@/lib/utils'
import { usePortalContainer } from '@/hooks/usePortalContainer'
import { useCommandBar } from '@/contexts/CommandBarContext'
import { ROOT_SELECTOR } from '@/constants/tailwind'
import { CommandBarInput } from './command-bar-input'
import { CommandBarList } from './command-bar-list'
import { CommandBarAIFallback } from './command-bar-ai-fallback'
import { CommandBarToolPrompt } from './command-bar-tool-prompt'
import type { CommandBarAction, CommandBarToolMeta } from '@/types'

export function CommandBarDialog() {
  const { isOpen, close, query, setQuery, actions, config } = useCommandBar()

  const portalContainer = usePortalContainer()
  const [isExecuting, setIsExecuting] = useState(false)
  const [executionError, setExecutionError] = useState<string | null>(null)
  const fireAndForget = config.fireAndForget ?? true
  const aiSubmitRef = useRef<(() => void) | null>(null)

  // Tool prompt mode state
  const [selectedTool, setSelectedTool] = useState<CommandBarToolMeta | null>(
    null
  )

  // Reset tool state when dialog closes
  React.useEffect(() => {
    if (!isOpen) {
      setSelectedTool(null)
    }
  }, [isOpen])

  const handleToolBack = useCallback(() => {
    setSelectedTool(null)
  }, [])

  const handleActionSelect = useCallback(
    (action: CommandBarAction) => {
      if (action.disabled) return

      // Tool with metadata → enter prompt mode
      if (action.toolMeta) {
        setSelectedTool(action.toolMeta)
        return
      }

      // String onSelect → route to AI fallback
      if (typeof action.onSelect === 'string') {
        setQuery(action.onSelect)
        return
      }

      const result = action.onSelect()

      if (fireAndForget) {
        close()
        if (result instanceof Promise) {
          result
            .then((res) => config.onAction?.({ action, result: res }))
            .catch((err: Error) =>
              config.onAction?.({ action, error: err.message })
            )
        } else {
          config.onAction?.({ action })
        }
      } else {
        if (result instanceof Promise) {
          setIsExecuting(true)
          setExecutionError(null)
          result
            .then((res) => {
              config.onAction?.({ action, result: res })
              close()
            })
            .catch((err: Error) => {
              config.onAction?.({ action, error: err.message })
              setExecutionError(err.message)
            })
            .finally(() => setIsExecuting(false))
        } else {
          config.onAction?.({ action })
          close()
        }
      }
    },
    [fireAndForget, close, config, setQuery]
  )

  // Handle Enter → submit to AI when cmdk has no matching items
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      // In tool prompt mode, let the child component handle keys
      if (selectedTool) {
        if (e.key === 'Escape') {
          e.preventDefault()
          handleToolBack()
        }
        return
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        close()
      }
      if (e.key === 'Enter') {
        // cmdk handles Enter for item selection. If the empty state is
        // visible (no matching items), forward Enter to the AI fallback.
        const root = e.currentTarget
        const emptyEl = root.querySelector('[cmdk-empty]:not([hidden])')
        if (emptyEl && aiSubmitRef.current) {
          e.preventDefault()
          aiSubmitRef.current()
        }
      }
    },
    [close, selectedTool, handleToolBack]
  )

  const placeholder = config.placeholder ?? 'Type a command or ask anything...'
  const maxVisible = config.maxVisible ?? 8

  // When there's no portal container (no ShadowRoot), the dialog portals to
  // document.body which is outside .gram-elements. Wrap in ROOT_SELECTOR so
  // scoped CSS variables and Tailwind utilities still apply.
  const needsScopeWrapper = !portalContainer

  return (
    <DialogPrimitive.Root
      open={isOpen}
      onOpenChange={(open) => !open && close()}
    >
      <DialogPrimitive.Portal container={portalContainer}>
        <CommandBarPortalScope enabled={needsScopeWrapper}>
          <DialogPrimitive.Overlay
            data-slot="command-bar-overlay"
            className="data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 fixed inset-0 z-50 bg-black/50"
          />
          <DialogPrimitive.Content
            data-slot="command-bar-content"
            className={cn(
              'data-[state=open]:animate-in data-[state=closed]:animate-out',
              'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
              'data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
              'data-[state=closed]:slide-out-to-top-[2%] data-[state=open]:slide-in-from-top-[2%]',
              'fixed top-[20%] left-[50%] z-50 w-full max-w-lg translate-x-[-50%]',
              'bg-popover text-popover-foreground rounded-lg border shadow-2xl',
              'duration-200'
            )}
            onKeyDown={handleKeyDown}
          >
            {/* Visually hidden title for accessibility */}
            <DialogPrimitive.Title className="sr-only">
              Command Bar
            </DialogPrimitive.Title>
            <DialogPrimitive.Description className="sr-only">
              Search for commands or ask AI
            </DialogPrimitive.Description>

            {selectedTool ? (
              // Tool prompt mode - natural language input
              <CommandBarToolPrompt
                toolMeta={selectedTool}
                onBack={handleToolBack}
                onToolCall={config.onToolCall}
              />
            ) : (
              // Standard command list mode
              <CommandPrimitive shouldFilter loop>
                <CommandBarInput
                  placeholder={placeholder}
                  value={query}
                  onValueChange={setQuery}
                />

                <CommandBarList
                  actions={actions}
                  onSelect={handleActionSelect}
                  maxVisible={maxVisible}
                />

                {/* Empty state: AI fallback */}
                <CommandPrimitive.Empty className="py-0">
                  <CommandBarAIFallback
                    query={query}
                    onToolCall={config.onToolCall}
                    onMessage={config.onMessage}
                    submitRef={aiSubmitRef}
                  />
                </CommandPrimitive.Empty>

                {/* Execution states */}
                {isExecuting && (
                  <div className="text-muted-foreground border-t px-3 py-2 text-xs">
                    Running...
                  </div>
                )}
                {executionError && (
                  <div className="text-destructive border-t px-3 py-2 text-xs">
                    {executionError}
                  </div>
                )}
              </CommandPrimitive>
            )}

            {/* Footer hint */}
            <div className="border-t px-3 py-2">
              <div className="text-muted-foreground flex items-center justify-between text-[10px]">
                <div className="flex items-center gap-3">
                  {selectedTool ? (
                    <>
                      <span>
                        <kbd className="bg-muted rounded px-1 py-0.5 font-medium">
                          Esc
                        </kbd>{' '}
                        Back
                      </span>
                      <span>
                        <kbd className="bg-muted rounded px-1 py-0.5 font-medium">
                          ↵
                        </kbd>{' '}
                        Send
                      </span>
                    </>
                  ) : (
                    <>
                      <span>
                        <kbd className="bg-muted rounded px-1 py-0.5 font-medium">
                          ↑↓
                        </kbd>{' '}
                        Navigate
                      </span>
                      <span>
                        <kbd className="bg-muted rounded px-1 py-0.5 font-medium">
                          ↵
                        </kbd>{' '}
                        Select
                      </span>
                      <span>
                        <kbd className="bg-muted rounded px-1 py-0.5 font-medium">
                          Esc
                        </kbd>{' '}
                        Close
                      </span>
                    </>
                  )}
                </div>
              </div>
            </div>
          </DialogPrimitive.Content>
        </CommandBarPortalScope>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}

/**
 * Wraps portal content in a .gram-elements scope div when the portal
 * lands outside the existing scope (i.e. no ShadowRoot portal container).
 */
function CommandBarPortalScope({
  enabled,
  children,
}: {
  enabled: boolean
  children: React.ReactNode
}) {
  if (!enabled) return <>{children}</>
  return <div className={ROOT_SELECTOR}>{children}</div>
}
