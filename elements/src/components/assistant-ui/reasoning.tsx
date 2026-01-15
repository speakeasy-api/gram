'use client'

import { BrainIcon, ChevronDownIcon } from 'lucide-react'
import {
  memo,
  useCallback,
  useRef,
  useState,
  type FC,
  type PropsWithChildren,
} from 'react'

import {
  useAssistantState,
  useScrollLock,
  type ReasoningGroupComponent,
  type ReasoningMessagePartComponent,
} from '@assistant-ui/react'

import { MarkdownText } from '@/components/assistant-ui/markdown-text'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'

const ANIMATION_DURATION = 200

/**
 * Root collapsible container that manages open/closed state and scroll lock.
 * Provides animation timing via CSS variable and prevents scroll jumps on collapse.
 */
const ReasoningRoot: FC<
  PropsWithChildren<{
    className?: string
  }>
> = ({ className, children }) => {
  const collapsibleRef = useRef<HTMLDivElement>(null)
  const [isOpen, setIsOpen] = useState(false)
  const lockScroll = useScrollLock(collapsibleRef, ANIMATION_DURATION)

  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (!open) {
        lockScroll()
      }
      setIsOpen(open)
    },
    [lockScroll]
  )

  return (
    <Collapsible
      ref={collapsibleRef}
      open={isOpen}
      onOpenChange={handleOpenChange}
      className={cn('aui-reasoning-root gramel:mb-4 gramel:w-full', className)}
      style={
        {
          '--animation-duration': `${ANIMATION_DURATION}ms`,
        } as React.CSSProperties
      }
    >
      {children}
    </Collapsible>
  )
}

ReasoningRoot.displayName = 'ReasoningRoot'

/**
 * Gradient overlay that softens the bottom edge during expand/collapse animations.
 * Animation: Fades out with delay when opening and fades back in when closing.
 */
const GradientFade: FC<{ className?: string }> = ({ className }) => (
  <div
    className={cn('aui-reasoning-fade gramel:pointer-events-none gramel:absolute gramel:inset-x-0 gramel:bottom-0 gramel:z-10 gramel:h-16',
      'gramel:bg-[linear-gradient(to_top,var(--color-background),transparent)]',
      'gramel:fade-in-0 gramel:animate-in',
      'gramel:group-data-[state=open]/collapsible-content:animate-out',
      'gramel:group-data-[state=open]/collapsible-content:fade-out-0',
      'gramel:group-data-[state=open]/collapsible-content:delay-[calc(var(--animation-duration)*0.75)]', // calc for timing the delay
      'gramel:group-data-[state=open]/collapsible-content:fill-mode-forwards',
      'gramel:duration-(--animation-duration)',
      'gramel:group-data-[state=open]/collapsible-content:duration-(--animation-duration)',
      className
    )}
  />
)

/**
 * Trigger button for the Reasoning collapsible.
 * Composed of icons, label, and text shimmer animation when reasoning is being streamed.
 */
const ReasoningTrigger: FC<{ active: boolean; className?: string }> = ({
  active,
  className,
}) => (
  <CollapsibleTrigger
    className={cn('aui-reasoning-trigger group/trigger gramel:text-muted-foreground gramel:hover:text-foreground gramel:-mb-2 gramel:flex gramel:max-w-[75%] gramel:items-center gramel:gap-2 gramel:py-2 gramel:text-sm gramel:transition-colors',
      className,
      active && 'gramel:shimmer'
    )}
  >
    <BrainIcon className="aui-reasoning-trigger-icon gramel:size-4 gramel:shrink-0" />
    <span className="aui-reasoning-trigger-label-wrapper gramel:relative gramel:inline-block gramel:leading-none">
      <span>Reasoning</span>
      {active ? (
        <span
          aria-hidden
          className="aui-reasoning-trigger-shimmer gramel:shimmer gramel:pointer-events-none gramel:absolute gramel:inset-0 gramel:motion-reduce:animate-none"
        >
          Reasoning
        </span>
      ) : null}
    </span>
    <ChevronDownIcon
      className={cn('aui-reasoning-trigger-chevron gramel:mt-0.5 gramel:size-4 gramel:shrink-0',
        'gramel:transition-transform gramel:duration-(--animation-duration) gramel:ease-out',
        'gramel:group-data-[state=closed]/trigger:-rotate-90',
        'gramel:group-data-[state=open]/trigger:rotate-0'
      )}
    />
  </CollapsibleTrigger>
)

/**
 * Collapsible content wrapper that handles height expand/collapse animation.
 * Animation: Height animates up (collapse) and down (expand).
 * Also provides group context for child animations via data-state attributes.
 */
const ReasoningContent: FC<
  PropsWithChildren<{
    className?: string
    'aria-busy'?: boolean
  }>
> = ({ className, children, 'aria-busy': ariaBusy }) => (
  <CollapsibleContent
    className={cn('aui-reasoning-content gramel:text-muted-foreground gramel:relative gramel:overflow-hidden gramel:text-sm gramel:outline-none',
      'gramel:group/collapsible-content gramel:ease-out',
      'gramel:data-[state=closed]:animate-collapsible-up',
      'gramel:data-[state=open]:animate-collapsible-down',
      'gramel:data-[state=closed]:fill-mode-forwards',
      'gramel:data-[state=closed]:pointer-events-none',
      'gramel:data-[state=open]:duration-(--animation-duration)',
      'gramel:data-[state=closed]:duration-(--animation-duration)',
      className
    )}
    aria-busy={ariaBusy}
  >
    {children}
    <GradientFade />
  </CollapsibleContent>
)

ReasoningContent.displayName = 'ReasoningContent'

/**
 * Text content wrapper that animates the reasoning text visibility.
 * Animation: Slides in from top + fades in when opening, reverses when closing.
 * Reacts to parent ReasoningContent's data-state via Radix group selectors.
 */
const ReasoningText: FC<
  PropsWithChildren<{
    className?: string
  }>
> = ({ className, children }) => (
  <div
    className={cn('aui-reasoning-text gramel:relative gramel:z-0 gramel:space-y-4 gramel:pt-4 gramel:pl-6 gramel:leading-relaxed',
      'gramel:transform-gpu gramel:transition-[transform,opacity]',
      'gramel:group-data-[state=open]/collapsible-content:animate-in',
      'gramel:group-data-[state=closed]/collapsible-content:animate-out',
      'gramel:group-data-[state=open]/collapsible-content:fade-in-0',
      'gramel:group-data-[state=closed]/collapsible-content:fade-out-0',
      'gramel:group-data-[state=open]/collapsible-content:slide-in-from-top-4',
      'gramel:group-data-[state=closed]/collapsible-content:slide-out-to-top-4',
      'gramel:group-data-[state=open]/collapsible-content:duration-(--animation-duration)',
      'gramel:group-data-[state=closed]/collapsible-content:duration-(--animation-duration)',
      'gramel:[&_p]:-mb-2',
      className
    )}
  >
    {children}
  </div>
)

ReasoningText.displayName = 'ReasoningText'

/**
 * Renders a single reasoning part's text with markdown support.
 * Consecutive reasoning parts are automatically grouped by ReasoningGroup.
 *
 * Pass Reasoning to MessagePrimitive.Parts in thread.tsx
 *
 * @example:
 * ```tsx
 * <MessagePrimitive.Parts
 *   components={{
 *     Reasoning: Reasoning,
 *     ReasoningGroup: ReasoningGroup,
 *   }}
 * />
 * ```
 */
const ReasoningImpl: ReasoningMessagePartComponent = () => <MarkdownText />

/**
 * Collapsible wrapper that groups consecutive reasoning parts together.
 *  Includes scroll lock to prevent page jumps during collapse animation.
 *
 *  Pass ReasoningGroup to MessagePrimitive.Parts in thread.tsx
 *
 * @example:
 * ```tsx
 * <MessagePrimitive.Parts
 *   components={{
 *     Reasoning: Reasoning,
 *     ReasoningGroup: ReasoningGroup,
 *   }}
 * />
 * ```
 */
const ReasoningGroupImpl: ReasoningGroupComponent = ({
  children,
  startIndex,
  endIndex,
}) => {
  /**
   * Detects if reasoning is currently streaming within this group's range.
   */
  const isReasoningStreaming = useAssistantState(({ message }) => {
    if (message.status?.type !== 'running') return false
    const lastIndex = message.parts.length - 1
    if (lastIndex < 0) return false
    const lastType = message.parts[lastIndex]?.type
    if (lastType !== 'reasoning') return false
    return lastIndex >= startIndex && lastIndex <= endIndex
  })

  return (
    <ReasoningRoot>
      <ReasoningTrigger active={isReasoningStreaming} />

      <ReasoningContent aria-busy={isReasoningStreaming}>
        <ReasoningText>{children}</ReasoningText>
      </ReasoningContent>
    </ReasoningRoot>
  )
}

export const Reasoning = memo(ReasoningImpl)
Reasoning.displayName = 'Reasoning'

export const ReasoningGroup = memo(ReasoningGroupImpl)
ReasoningGroup.displayName = 'ReasoningGroup'
