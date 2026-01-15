'use client'

import { useMemo, useState, type FC } from 'react'
import {
  Loader,
  Maximize,
  MessageCircleIcon,
  Minimize,
  XIcon,
} from 'lucide-react'
import { LazyMotion, domMax, AnimatePresence, MotionConfig } from 'motion/react'
import * as m from 'motion/react-m'

import { ErrorBoundary } from '@/components/assistant-ui/error-boundary'
import { Thread } from '@/components/assistant-ui/thread'
import { ThreadList } from '@/components/assistant-ui/thread-list'
import { useThemeProps } from '@/hooks/useThemeProps'
import { useRadius } from '@/hooks/useRadius'
import { useDensity } from '@/hooks/useDensity'
import { assertNever, cn } from '@/lib/utils'
import { useElements } from '@/hooks/useElements'
import { useExpanded } from '@/hooks/useExpanded'
import { EASE_OUT_QUINT } from '@/lib/easing'
import { useAssistantState } from '@assistant-ui/react'

const LAYOUT_TRANSITION = {
  layout: {
    duration: 0.25,
    ease: EASE_OUT_QUINT,
  },
} as const

type Dimensions = {
  width?: string | number | `${number}%`
  height?: string | number | `${number}%`
  maxHeight?: string | number | `${number}%`
}

interface AssistantModalProps {
  className?: string
}

export const AssistantModal: FC<AssistantModalProps> = ({ className }) => {
  const { config } = useElements()
  const themeProps = useThemeProps()
  const r = useRadius()
  const d = useDensity()
  const [isOpen, setIsOpen] = useState(config.modal?.defaultOpen ?? false)
  const { expandable, isExpanded, setIsExpanded } = useExpanded()
  const title = config.modal?.title
  const customIcon = config.modal?.icon

  // Check if thread list should be shown
  const showThreadList =
    config.history?.enabled && config.history?.showThreadList !== false

  const position = config.modal?.position ?? 'bottom-right'
  const anchorPositionClass = positionClassname(position)

  const defaultDimensions: Dimensions = useMemo(
    () =>
      config.modal?.dimensions?.default ?? {
        width: '500px',
        height: '600px',
        maxHeight: '95vh',
      },
    [config.modal?.dimensions?.default]
  )

  const expandedDimensions: Dimensions = useMemo(
    () =>
      config.modal?.dimensions?.expanded ?? {
        width: '70vw',
        height: '90vh',
      },
    [config.modal?.dimensions?.expanded]
  )
  const thread = useAssistantState(({ thread }) => thread)
  const isGenerating = thread.messages.some(
    (message) => message.status?.type === 'running'
  )

  const effectiveWidth = isExpanded
    ? expandedDimensions.width
    : defaultDimensions.width
  const effectiveHeight = isExpanded
    ? expandedDimensions.height
    : defaultDimensions.height
  const effectiveMaxHeight = defaultDimensions.maxHeight

  return (
    <LazyMotion features={domMax}>
      {/* reducedMotion="user" respects prefers-reduced-motion */}
      <MotionConfig reducedMotion="user" transition={LAYOUT_TRANSITION}>
        <div
          className={cn('aui-root aui-modal-anchor gramel:fixed gramel:z-10',
            anchorPositionClass,
            themeProps.className,
            r('lg'),
            isOpen && 'gramel:shadow-xl',
            className
          )}
        >
          <AnimatePresence mode="wait">
            {!isOpen ? (
              <m.button
                key="button"
                layout
                layoutId="chat-container"
                onClick={() => setIsOpen(true)}
                className={cn('aui-modal-button gramel:bg-primary gramel:text-primary-foreground gramel:flex gramel:size-12 gramel:cursor-pointer gramel:items-center gramel:justify-center gramel:border gramel:shadow-lg gramel:transition-shadow gramel:hover:shadow-xl',
                  r('full')
                )}
                initial={false}
                aria-label={`Open ${title}`}
                style={{ originX: 1, originY: 1 }}
              >
                <m.div
                  initial={{ opacity: 0, scale: 0.8 }}
                  animate={{ opacity: 1, scale: 1 }}
                  exit={{ opacity: 0, scale: 0.8 }}
                  transition={{ duration: 0.2, ease: EASE_OUT_QUINT }}
                  className="gramel:flex gramel:size-full gramel:items-center gramel:justify-center"
                >
                  {customIcon ? (
                    customIcon('closed')
                  ) : (
                    <MessageCircleIcon className="gramel:size-6" />
                  )}
                </m.div>
              </m.button>
            ) : (
              <m.div
                key="chat"
                layout
                layoutId="chat-container"
                className={cn('aui-modal-content gramel:bg-popover gramel:text-popover-foreground gramel:flex gramel:flex-col gramel:overflow-hidden gramel:border gramel:[&>.aui-thread-root]:bg-inherit',
                  r('lg')
                )}
                initial={false}
                style={{
                  originX: position.includes('left') ? 0 : 1,
                  originY: position.includes('top') ? 0 : 1,
                  width: effectiveWidth,
                  height: effectiveHeight,
                  maxHeight: effectiveMaxHeight,
                }}
              >
                <m.div
                  className={cn('aui-modal-header gramel:flex gramel:shrink-0 gramel:items-center gramel:justify-between gramel:border-b',
                    d('gramel:h-header'),
                    d('gramel:px-lg')
                  )}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  transition={{
                    duration: 0.2,
                    delay: 0.1,
                    ease: EASE_OUT_QUINT,
                  }}
                >
                  <div className={cn('gramel:flex gramel:min-w-0 gramel:items-center')}>
                    <span
                      className={cn('gramel:text-md gramel:flex gramel:items-center gramel:gap-2 gramel:truncate gramel:font-medium',
                        isGenerating && 'gramel:shimmer'
                      )}
                    >
                      <span className="gramel:truncate">{title}</span>

                      {isGenerating && (
                        <Loader
                          className="gramel:text-muted-foreground gramel:size-4.5 gramel:animate-spin"
                          strokeWidth={1.25}
                        />
                      )}
                    </span>
                  </div>

                  <div className="gramel:flex gramel:flex-row gramel:items-center gramel:justify-end gramel:gap-1">
                    {expandable ? (
                      <button
                        type="button"
                        onClick={() => setIsExpanded((v) => !v)}
                        className={cn('gramel:text-muted-foreground gramel:hover:text-foreground gramel:hover:bg-accent gramel:flex gramel:h-8 gramel:cursor-pointer gramel:items-center gramel:rounded-md gramel:px-2 gramel:text-xs gramel:transition-colors'
                        )}
                        aria-pressed={isExpanded}
                        aria-label={
                          isExpanded ? 'Collapse assistant' : 'Expand assistant'
                        }
                      >
                        {isExpanded ? (
                          <Minimize
                            strokeWidth={2}
                            className="gramel:size-3.5 gramel:rotate-90"
                          />
                        ) : (
                          <Maximize
                            strokeWidth={2}
                            className="gramel:size-3.5 gramel:rotate-90"
                          />
                        )}
                      </button>
                    ) : null}
                    <button
                      onClick={() => {
                        setIsOpen(false)
                        // Optional: reset expansion when closing
                        setIsExpanded(false)
                      }}
                      className="gramel:text-muted-foreground gramel:hover:text-foreground gramel:hover:bg-accent gramel:-mr-1 gramel:flex gramel:size-8 gramel:cursor-pointer gramel:items-center gramel:justify-center gramel:rounded-md gramel:transition-colors"
                      aria-label={`Close ${title}`}
                    >
                      <XIcon className="gramel:size-4.5" />
                    </button>
                  </div>
                </m.div>

                {/* Main content area */}
                <m.div
                  className="aui-modal-body gramel:flex gramel:flex-1 gramel:overflow-hidden"
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  transition={{
                    duration: 0.2,
                    delay: 0.05,
                    ease: EASE_OUT_QUINT,
                  }}
                >
                  {/* Thread list sidebar (when history enabled) */}
                  {showThreadList && (
                    <div className="aui-modal-thread-list gramel:w-56 gramel:shrink-0 gramel:overflow-y-auto gramel:border-r">
                      <ThreadList />
                    </div>
                  )}

                  {/* Thread content */}
                  <div className="aui-modal-thread gramel:w-full gramel:flex-1 gramel:overflow-hidden">
                    <ErrorBoundary>
                      <Thread />
                    </ErrorBoundary>
                  </div>
                </m.div>
              </m.div>
            )}
          </AnimatePresence>
        </div>
      </MotionConfig>
    </LazyMotion>
  )
}

function positionClassname(
  position:
    | 'bottom-right'
    | 'bottom-left'
    | 'top-right'
    | 'top-left'
    | undefined
): string {
  switch (position) {
    case 'bottom-left':
      return 'gramel:left-4 gramel:bottom-4'
    case 'top-right':
      return 'gramel:right-4 gramel:top-4'
    case 'top-left':
      return 'gramel:left-4 gramel:top-4'
    case 'bottom-right':
      return 'gramel:right-4 gramel:bottom-4'
    default:
      assertNever(position)
  }
}
