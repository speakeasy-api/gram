'use client'

import { type FC } from 'react'
import { PanelRightClose, PanelRightOpen } from 'lucide-react'
import { Thread } from '@/components/assistant-ui/thread'
import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import { useThemeProps } from '@/hooks/useThemeProps'
import { useElements } from '@/hooks/useElements'
import { cn } from '@/lib/utils'
import { useExpanded } from '@/hooks/useExpanded'
import { LazyMotion, domMax } from 'motion/react'
import * as m from 'motion/react-m'
import { EASE_OUT_QUINT } from '@/lib/easing'
import { useAssistantState } from '@assistant-ui/react'

export const AssistantSidecar: FC = () => {
  const { config } = useElements()
  const themeProps = useThemeProps()
  const sidecarConfig = config.sidecar ?? {}
  const { title, dimensions } = sidecarConfig
  const { isExpanded, setIsExpanded } = useExpanded()
  const thread = useAssistantState(({ thread }) => thread)
  const isGenerating = thread.messages.some(
    (message) => message.status?.type === 'running'
  )

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
        className={cn(
          'aui-root aui-sidecar bg-popover text-popover-foreground fixed top-0 right-0 border-l',
          themeProps.className
        )}
      >
        {/* Header */}
        <div className="aui-sidecar-header flex h-14 items-center justify-between border-b px-4">
          <span
            className={cn(
              'flex items-center gap-2 text-sm font-medium',
              isGenerating && 'shimmer'
            )}
          >
            {title}
          </span>
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
        </div>

        {/* Thread content */}
        <div className="aui-sidecar-content h-[calc(100%-3.5rem)] overflow-hidden">
          <Thread />
        </div>
      </m.div>
    </LazyMotion>
  )
}
