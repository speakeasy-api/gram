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
  const { isExpanded, setIsExpanded } = useExpanded()
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
        initial={{
          width: dimensions?.default?.width ?? '400px',
          height: dimensions?.default?.height ?? '100vh',
        }}
        animate={{
          width: isExpanded
            ? (dimensions?.expanded?.width ?? '800px')
            : (dimensions?.default?.width ?? '400px'),
          height: isExpanded
            ? (dimensions?.expanded?.height ?? '100%')
            : (dimensions?.default?.height ?? '100vh'),
        }}
        transition={{ duration: 0.3, ease: EASE_OUT_QUINT }}
        className={cn('aui-root aui-sidecar gramel:bg-popover gramel:text-popover-foreground gramel:fixed gramel:top-0 gramel:right-0 gramel:border-l',
          themeProps.className,
          className
        )}
      >
        {/* Header */}
        <div className="aui-sidecar-header gramel:flex gramel:h-14 gramel:items-center gramel:justify-between gramel:border-b gramel:px-4">
          <span
            className={cn('gramel:text-md gramel:flex gramel:items-center gramel:gap-2 gramel:font-medium',
              isGenerating && 'gramel:shimmer'
            )}
          >
            {title}

            {isGenerating && (
              <Loader
                className="gramel:text-muted-foreground gramel:size-4.5 gramel:animate-spin"
                strokeWidth={1.25}
              />
            )}
          </span>
          <div className="aui-sidecar-header-actions gramel:flex gramel:items-center gramel:gap-1">
            <TooltipIconButton
              tooltip={isExpanded ? 'Collapse' : 'Pop out'}
              variant="ghost"
              className="aui-sidecar-popout gramel:size-8"
              onClick={() => setIsExpanded((v) => !v)}
            >
              {!isExpanded ? (
                <PanelRightOpen className="gramel:size-4.5" />
              ) : (
                <PanelRightClose className="gramel:size-4.5" />
              )}
            </TooltipIconButton>
          </div>
        </div>

        {/* Main content area */}
        <div className="aui-sidecar-body gramel:flex gramel:h-[calc(100%-3.5rem)] gramel:overflow-hidden">
          {/* Thread list sidebar (when history enabled) */}
          {showThreadList && (
            <div className="aui-sidecar-thread-list gramel:w-56 gramel:shrink-0 gramel:overflow-y-auto gramel:border-r">
              <ThreadList />
            </div>
          )}

          {/* Thread content */}
          <div className="aui-sidecar-content gramel:flex-1 gramel:overflow-hidden">
            <ErrorBoundary>
              <Thread />
            </ErrorBoundary>
          </div>
        </div>
      </m.div>
    </LazyMotion>
  )
}
