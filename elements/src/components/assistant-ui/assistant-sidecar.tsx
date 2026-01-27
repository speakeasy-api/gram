'use client'

import { type FC } from 'react'
import { Loader, PanelRightClose, PanelRightOpen } from 'lucide-react'
import { ErrorBoundary } from '@/components/assistant-ui/error-boundary'
import { Thread } from '@/components/assistant-ui/thread'
import { ThreadList } from '@/components/assistant-ui/thread-list'
import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import { useThemeProps } from '@/hooks/useThemeProps'
import { useElements } from '@/hooks/useElements'
import { cn } from '@/lib/utils'
import { useExpanded } from '@/hooks/useExpanded'
import { LazyMotion, domMax } from 'motion/react'
import * as m from 'motion/react-m'
import { EASE_OUT_QUINT } from '@/lib/easing'
import { useAssistantState } from '@assistant-ui/react'

interface AssistantSidecarProps {
  className?: string
}

export const AssistantSidecar: FC<AssistantSidecarProps> = ({ className }) => {
  const { config } = useElements()
  const themeProps = useThemeProps()
  const sidecarConfig = config.sidecar ?? {}
  const { title, dimensions } = sidecarConfig
  const { expandable, isExpanded, setIsExpanded } = useExpanded()
  const thread = useAssistantState(({ thread }) => thread)
  const isGenerating = thread.messages.some(
    (message) => message.status?.type === 'running'
  )

  // Check if thread list should be shown
  const showThreadList =
    config.history?.enabled && config.history?.showThreadList !== false

  return (
    <LazyMotion features={domMax}>
      <m.div
        initial={false}
        animate={{
          width: isExpanded
            ? (dimensions?.expanded?.width ?? '800px')
            : (dimensions?.default?.width ?? '400px'),
        }}
        transition={{ duration: 0.3, ease: EASE_OUT_QUINT }}
        className={cn(
          'aui-root aui-sidecar bg-popover text-popover-foreground fixed top-0 right-0 bottom-0 flex flex-col border-l',
          themeProps.className,
          className
        )}
      >
        {/* Header */}
        <div className="aui-sidecar-header flex h-14 shrink-0 items-center justify-between border-b px-4">
          <span
            className={cn(
              'text-foreground text-md flex items-center gap-2 font-medium',
              isGenerating && 'title-shimmer'
            )}
          >
            {title}

            {isGenerating && (
              <Loader
                className="text-muted-foreground size-4.5 animate-spin"
                strokeWidth={1.25}
              />
            )}
          </span>
          {expandable && (
            <div className="aui-sidecar-header-actions flex items-center gap-1">
              <TooltipIconButton
                tooltip={isExpanded ? 'Collapse' : 'Pop out'}
                variant="ghost"
                className="aui-sidecar-popout size-8"
                onClick={() => setIsExpanded((v) => !v)}
              >
                {!isExpanded ? (
                  <PanelRightOpen className="size-4.5" />
                ) : (
                  <PanelRightClose className="size-4.5" />
                )}
              </TooltipIconButton>
            </div>
          )}
        </div>

        {/* Main content area */}
        <div className="aui-sidecar-body flex min-h-0 flex-1 overflow-hidden">
          {/* Thread list sidebar (when history enabled) */}
          {showThreadList && (
            <div className="aui-sidecar-thread-list w-56 shrink-0 overflow-y-auto border-r">
              <ThreadList />
            </div>
          )}

          {/* Thread content */}
          <div className="aui-sidecar-content flex-1 overflow-hidden">
            <ErrorBoundary>
              <Thread />
            </ErrorBoundary>
          </div>
        </div>
      </m.div>
    </LazyMotion>
  )
}
