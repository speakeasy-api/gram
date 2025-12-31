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

import { Thread } from '@/components/assistant-ui/thread'
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

export const AssistantModal: FC = () => {
  const { config } = useElements()
  const themeProps = useThemeProps()
  const r = useRadius()
  const d = useDensity()
  const [isOpen, setIsOpen] = useState(config.modal?.defaultOpen ?? false)
  const { expandable, isExpanded, setIsExpanded } = useExpanded()
  const title = config.modal?.title
  const customIcon = config.modal?.icon

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
          className={cn(
            'aui-root aui-modal-anchor fixed z-[9999]',
            anchorPositionClass,
            themeProps.className,
            r('lg'),
            isOpen && 'shadow-xl'
          )}
        >
          <AnimatePresence mode="wait">
            {!isOpen ? (
              <m.button
                key="button"
                layout
                layoutId="chat-container"
                onClick={() => setIsOpen(true)}
                className={cn(
                  'aui-modal-button bg-primary text-primary-foreground flex size-12 cursor-pointer items-center justify-center border shadow-lg transition-shadow hover:shadow-xl',
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
                >
                  {customIcon ? (
                    customIcon('closed')
                  ) : (
                    <MessageCircleIcon className="size-6" />
                  )}
                </m.div>
              </m.button>
            ) : (
              <m.div
                key="chat"
                layout
                layoutId="chat-container"
                className={cn(
                  'aui-modal-content bg-popover text-popover-foreground flex flex-col overflow-hidden border [&>.aui-thread-root]:bg-inherit',
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
                  className={cn(
                    'aui-modal-header flex shrink-0 items-center justify-between border-b',
                    d('h-header'),
                    d('px-lg')
                  )}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  transition={{
                    duration: 0.2,
                    delay: 0.1,
                    ease: EASE_OUT_QUINT,
                  }}
                >
                  <div className={cn('flex min-w-0 items-center')}>
                    <span
                      className={cn(
                        'text-md flex items-center gap-2 truncate font-medium',
                        isGenerating && 'shimmer'
                      )}
                    >
                      <span className="truncate">{title}</span>

                      {isGenerating && (
                        <Loader
                          className="text-muted-foreground size-4.5 animate-spin"
                          strokeWidth={1.25}
                        />
                      )}
                    </span>
                  </div>

                  <div className="flex flex-row items-center justify-end gap-1">
                    {expandable ? (
                      <button
                        type="button"
                        onClick={() => setIsExpanded((v) => !v)}
                        className={cn(
                          'text-muted-foreground hover:text-foreground hover:bg-accent flex h-8 cursor-pointer items-center rounded-md px-2 text-xs transition-colors'
                        )}
                        aria-pressed={isExpanded}
                        aria-label={
                          isExpanded ? 'Collapse assistant' : 'Expand assistant'
                        }
                      >
                        {isExpanded ? (
                          <Minimize
                            strokeWidth={2}
                            className="size-3.5 rotate-90"
                          />
                        ) : (
                          <Maximize
                            strokeWidth={2}
                            className="size-3.5 rotate-90"
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
                      className="text-muted-foreground hover:text-foreground hover:bg-accent -mr-1 flex size-8 cursor-pointer items-center justify-center rounded-md transition-colors"
                      aria-label={`Close ${title}`}
                    >
                      <XIcon className="size-4.5" />
                    </button>
                  </div>
                </m.div>

                {/* Thread content */}
                <m.div
                  className="aui-modal-thread w-full flex-1 overflow-hidden"
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  transition={{
                    duration: 0.2,
                    delay: 0.05,
                    ease: EASE_OUT_QUINT,
                  }}
                >
                  <Thread />
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
      return 'left-4 bottom-4'
    case 'top-right':
      return 'right-4 top-4'
    case 'top-left':
      return 'left-4 top-4'
    case 'bottom-right':
      return 'right-4 bottom-4'
    default:
      assertNever(position)
  }
}
