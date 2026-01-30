/**
 * `<Replay>` - A standalone provider that plays back a recorded cassette.
 *
 * Replaces `GramElementsProvider` entirely for demo/marketing use cases.
 * No auth, MCP, or network calls are made.
 *
 * @example
 * ```tsx
 * import { Replay, Chat } from '@gram-ai/elements'
 * import cassette from './demo.cassette.json'
 *
 * function MarketingDemo() {
 *   return (
 *     <Replay cassette={cassette}>
 *       <Chat />
 *     </Replay>
 *   )
 * }
 * ```
 */

import { ROOT_SELECTOR } from '@/constants/tailwind'
import { ElementsContext } from '@/contexts/contexts'
import { ReplayContext } from '@/contexts/ReplayContext'
import { ToolApprovalProvider } from '@/contexts/ToolApprovalContext'
import {
  createReplayTransport,
  type Cassette,
  type ReplayOptions,
} from '@/lib/cassette'
import { MODELS } from '@/lib/models'
import { cn } from '@/lib/utils'
import { recommended } from '@/plugins'
import type { ElementsConfig } from '@/types'
import { AssistantRuntimeProvider, useThreadRuntime } from '@assistant-ui/react'
import { useChatRuntime } from '@assistant-ui/react-ai-sdk'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactNode, useEffect, useMemo, useRef } from 'react'

// ---------------------------------------------------------------------------
// Replay component
// ---------------------------------------------------------------------------

export interface ReplayProps extends ReplayOptions {
  /** The recorded cassette to replay. */
  cassette: Cassette
  children: ReactNode
  /** Optional ElementsConfig for visual customization (theme, variant, etc.) */
  config?: Partial<ElementsConfig>
}

const replayQueryClient = new QueryClient()

export const Replay = ({
  cassette,
  children,
  config: partialConfig,
  typingSpeed,
  userMessageDelay,
  assistantStartDelay,
  onComplete,
}: ReplayProps) => {
  const replayOptions: ReplayOptions = {
    typingSpeed,
    userMessageDelay,
    assistantStartDelay,
    onComplete,
  }

  const transport = useMemo(
    () => createReplayTransport(cassette, replayOptions),
    [cassette]
  )

  const runtime = useChatRuntime({ transport })

  const plugins = partialConfig?.plugins ?? recommended

  // Build a minimal ElementsConfig for child components
  const config: ElementsConfig = useMemo(
    () => ({
      projectSlug: partialConfig?.projectSlug ?? 'replay',
      variant: partialConfig?.variant,
      theme: partialConfig?.theme,
      welcome: partialConfig?.welcome,
      components: partialConfig?.components,
      plugins,
      composer: partialConfig?.composer,
      tools: partialConfig?.tools,
    }),
    [partialConfig, plugins]
  )

  const contextValue = useMemo(
    () => ({
      config,
      setModel: () => {},
      model: MODELS[0],
      isExpanded: false,
      setIsExpanded: () => {},
      isOpen: true,
      setIsOpen: () => {},
      plugins,
      mcpTools: undefined,
    }),
    [config, plugins]
  )

  const replayCtx = useMemo(() => ({ isReplay: true }), [])

  return (
    <QueryClientProvider client={replayQueryClient}>
      <AssistantRuntimeProvider runtime={runtime}>
        <ReplayContext.Provider value={replayCtx}>
          <ElementsContext.Provider value={contextValue}>
            <ToolApprovalProvider>
              <div
                className={cn(
                  ROOT_SELECTOR,
                  (config.variant === 'standalone' ||
                    config.variant === 'sidecar') &&
                    'h-full min-h-0 flex-1'
                )}
              >
                {children}
              </div>
              <ReplayController cassette={cassette} options={replayOptions} />
            </ToolApprovalProvider>
          </ElementsContext.Provider>
        </ReplayContext.Provider>
      </AssistantRuntimeProvider>
    </QueryClientProvider>
  )
}

// ---------------------------------------------------------------------------
// ReplayController - auto-submits user messages to drive the replay
// ---------------------------------------------------------------------------

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/**
 * Wait for the runtime to complete a full run cycle (start → finish).
 * After `runtime.append()`, there's a brief window where `isRunning` is
 * still false. We must wait for it to become true first, then wait for
 * it to return to false — otherwise we resolve before the run begins.
 */
async function waitForRunComplete(
  runtime: NonNullable<ReturnType<typeof useThreadRuntime>>
): Promise<void> {
  // Phase 1: wait for the runtime to start running
  if (!runtime.getState().isRunning) {
    await new Promise<void>((resolve) => {
      const unsub = runtime.subscribe(() => {
        if (runtime.getState().isRunning) {
          unsub()
          resolve()
        }
      })
      // Re-check in case it started between getState and subscribe
      if (runtime.getState().isRunning) {
        unsub()
        resolve()
      }
    })
  }

  // Phase 2: wait for the runtime to stop running
  if (runtime.getState().isRunning) {
    await new Promise<void>((resolve) => {
      const unsub = runtime.subscribe(() => {
        if (!runtime.getState().isRunning) {
          unsub()
          resolve()
        }
      })
    })
  }
}

interface ReplayControllerProps {
  cassette: Cassette
  options: ReplayOptions
}

const ReplayController = ({ cassette, options }: ReplayControllerProps) => {
  const runtime = useThreadRuntime()
  const hasStarted = useRef(false)

  useEffect(() => {
    if (hasStarted.current) return
    hasStarted.current = true

    const userMessageDelay = options.userMessageDelay ?? 800
    let cancelled = false

    const runReplay = async () => {
      for (const msg of cassette.messages) {
        if (cancelled) return

        if (msg.role === 'user') {
          await sleep(userMessageDelay)
          if (cancelled) return

          // Extract text from user content parts
          const text = msg.content
            .filter((p) => p.type === 'text')
            .map((p) => p.text)
            .join('\n')

          // Append the user message — triggers transport.sendMessages
          runtime.append(text)

          // Wait for the assistant response to finish streaming
          await waitForRunComplete(runtime)
        }
        // Assistant messages are handled by the transport's sendMessages,
        // so we skip them here.
      }

      options.onComplete?.()
    }

    runReplay()

    return () => {
      cancelled = true
    }
  }, [])

  return null
}
