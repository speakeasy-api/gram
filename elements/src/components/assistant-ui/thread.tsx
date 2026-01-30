import {
  ArrowDownIcon,
  ArrowUpIcon,
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CopyIcon,
  PencilIcon,
  Settings2,
  Square,
} from 'lucide-react'

import {
  ActionBarPrimitive,
  BranchPickerPrimitive,
  ComposerPrimitive,
  ErrorPrimitive,
  ImageMessagePartProps,
  MessagePrimitive,
  ThreadPrimitive,
  useAssistantState,
} from '@assistant-ui/react'

import { LazyMotion, MotionConfig, domAnimation } from 'motion/react'
import * as m from 'motion/react-m'
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FC,
} from 'react'
import { AnimatePresence } from 'motion/react'

import {
  ComposerAddAttachment,
  ComposerAttachments,
  UserMessageAttachments,
} from '@/components/assistant-ui/attachment'
import { FollowOnSuggestions } from '@/components/assistant-ui/follow-on-suggestions'
import { MarkdownText } from '@/components/assistant-ui/markdown-text'
import { MessageFeedback } from '@/components/assistant-ui/message-feedback'
import { Reasoning, ReasoningGroup } from '@/components/assistant-ui/reasoning'
import { ToolFallback } from '@/components/assistant-ui/tool-fallback'
import { ToolMentionAutocomplete } from '@/components/assistant-ui/tool-mention-autocomplete'
import { MentionedToolsBadges } from '@/components/assistant-ui/mentioned-tools-badges'
import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import { Button } from '@/components/ui/button'
import { useToolMentions } from '@/hooks/useToolMentions'

import { useDensity } from '@/hooks/useDensity'
import { useElements } from '@/hooks/useElements'
import { useRadius } from '@/hooks/useRadius'
import { useThemeProps } from '@/hooks/useThemeProps'
import { EASE_OUT_QUINT } from '@/lib/easing'
import { MODELS } from '@/lib/models'
import { cn } from '@/lib/utils'
import { ConnectionStatusIndicatorSafe } from './connection-status-indicator'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '../ui/tooltip'
import { ToolGroup } from './tool-group'

// Context for chat resolution state
const ChatResolutionContext = createContext<{
  isResolved: boolean
  feedbackHidden: boolean
  setResolved: () => void
  setUnresolved: () => void
  resetFeedbackHidden: () => void
}>({
  isResolved: false,
  feedbackHidden: false,
  setResolved: () => {},
  setUnresolved: () => {},
  resetFeedbackHidden: () => {},
})

const useChatResolution = () => useContext(ChatResolutionContext)

const StaticSessionWarning = () => (
  <div className="m-2 rounded-md border border-amber-500 bg-amber-100 px-4 py-3 text-sm text-amber-800 dark:border-amber-600 dark:bg-amber-900/30 dark:text-amber-200">
    <strong>Warning:</strong> You are using a static session token in the
    client. It will expire shortly. Please{' '}
    <a
      href="https://github.com/speakeasy-api/gram/tree/main/elements#setting-up-your-backend"
      target="_blank"
      rel="noopener noreferrer"
      className="text-amber-700 underline hover:text-amber-800 dark:text-amber-300 dark:hover:text-amber-200"
    >
      set up a session endpoint to avoid this warning.
    </a>
  </div>
)

interface ThreadProps {
  className?: string
}

export const Thread: FC<ThreadProps> = ({ className }) => {
  const themeProps = useThemeProps()
  const d = useDensity()
  const { config } = useElements()
  const components = config.components ?? {}
  const showStaticSessionWarning = config.api && 'sessionToken' in config.api
  const showFeedback = config.thread?.experimental_showFeedback ?? false
  const [isResolved, setIsResolved] = useState(false)
  const [feedbackHidden, setFeedbackHidden] = useState(false)

  const setResolved = () => setIsResolved(true)
  const setUnresolved = () => {
    setIsResolved(false)
    setFeedbackHidden(true)
  }
  const resetFeedbackHidden = () => setFeedbackHidden(false)

  return (
    <ChatResolutionContext.Provider
      value={{
        isResolved: showFeedback && isResolved,
        feedbackHidden,
        setResolved,
        setUnresolved,
        resetFeedbackHidden,
      }}
    >
      <LazyMotion features={domAnimation}>
        <MotionConfig reducedMotion="user">
          <ThreadPrimitive.Root
            className={cn(
              'aui-root aui-thread-root bg-background @container relative flex h-full flex-col',
              themeProps.className,
              className
            )}
          >
            <ConnectionStatusIndicatorSafe />
            <ThreadPrimitive.Viewport
              className={cn(
                'aui-thread-viewport relative mx-auto flex w-full flex-1 flex-col overflow-x-auto overflow-y-scroll pb-0!',
                d('p-lg')
              )}
            >
              <ThreadPrimitive.If empty>
                {components.ThreadWelcome ? (
                  <components.ThreadWelcome />
                ) : (
                  <ThreadWelcome />
                )}
              </ThreadPrimitive.If>

              {showStaticSessionWarning && <StaticSessionWarning />}

              <ThreadPrimitive.Messages
                components={{
                  UserMessage: components.UserMessage ?? UserMessage,
                  EditComposer: components.EditComposer ?? EditComposer,
                  AssistantMessage:
                    components.AssistantMessage ?? AssistantMessage,
                }}
              />

              <ThreadPrimitive.If empty={false} running={false}>
                <FollowOnSuggestions />
              </ThreadPrimitive.If>

              <ThreadPrimitive.If empty={false}>
                <div className="aui-thread-viewport-spacer min-h-8 grow" />
              </ThreadPrimitive.If>

              <Composer showFeedback={showFeedback} />
            </ThreadPrimitive.Viewport>

            {/* Resolution overlay - subtle readonly effect */}
            <AnimatePresence>
              {showFeedback && isResolved && (
                <m.div
                  className="bg-background/40 pointer-events-none absolute inset-0 z-50"
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  exit={{ opacity: 0 }}
                  transition={{ duration: 0.3, ease: EASE_OUT_QUINT }}
                />
              )}
            </AnimatePresence>
          </ThreadPrimitive.Root>
        </MotionConfig>
      </LazyMotion>
    </ChatResolutionContext.Provider>
  )
}

const ThreadScrollToBottom: FC = () => {
  return (
    <ThreadPrimitive.ScrollToBottom asChild>
      <TooltipIconButton
        tooltip="Scroll to bottom"
        variant="outline"
        className="aui-thread-scroll-to-bottom dark:bg-background dark:hover:bg-accent absolute -top-12 z-10 self-center rounded-full p-4 disabled:invisible"
      >
        <ArrowDownIcon />
      </TooltipIconButton>
    </ThreadPrimitive.ScrollToBottom>
  )
}

const ThreadWelcome: FC = () => {
  const { config } = useElements()
  const d = useDensity()
  const { title, subtitle } = config.welcome ?? {}
  const isStandalone = config.variant === 'standalone'

  return (
    <div
      className={cn(
        'aui-thread-welcome-root my-auto flex w-full grow flex-col',
        isStandalone ? 'items-center justify-center' : '',
        d('gap-lg')
      )}
    >
      <div
        className={cn(
          'aui-thread-welcome-center flex w-full grow flex-col items-center justify-start'
        )}
      >
        <div
          className={cn(
            'aui-thread-welcome-message flex flex-col',
            isStandalone
              ? 'items-center text-center'
              : 'size-full justify-start',
            d('gap-sm'),
            !isStandalone && d('py-md')
          )}
        >
          <m.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 10 }}
            transition={{ duration: 0.25, ease: EASE_OUT_QUINT }}
            className={cn(
              'aui-thread-welcome-message-motion-1 text-foreground font-semibold',
              d('text-title')
            )}
          >
            {title}
          </m.div>
          <m.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 10 }}
            transition={{ duration: 0.25, delay: 0.05, ease: EASE_OUT_QUINT }}
            className={cn(
              'aui-thread-welcome-message-motion-2 text-muted-foreground/65',
              d('text-subtitle')
            )}
          >
            {subtitle}
          </m.div>
        </div>
      </div>
      <ThreadSuggestions />
    </div>
  )
}

const ThreadSuggestions: FC = () => {
  const { config } = useElements()
  const r = useRadius()
  const d = useDensity()
  const suggestions = config.welcome?.suggestions ?? []
  const isStandalone = config.variant === 'standalone'

  if (suggestions.length === 0) return null

  return (
    <div
      className={cn(
        'aui-thread-welcome-suggestions w-full',
        d('gap-md'),
        d('py-lg'),
        isStandalone
          ? 'flex flex-wrap items-center justify-center'
          : suggestions.length === 1
            ? 'flex'
            : 'grid max-w-fit @md:grid-cols-2'
      )}
    >
      {suggestions.map((suggestion, index) => (
        <m.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 20 }}
          transition={{
            duration: 0.25,
            delay: 0.03 * index,
            ease: EASE_OUT_QUINT,
          }}
          key={`suggested-action-${suggestion.title}-${index}`}
          className={cn(
            'aui-thread-welcome-suggestion-display',
            !isStandalone && 'nth-[n+3]:hidden @md:nth-[n+3]:block'
          )}
        >
          <ThreadPrimitive.Suggestion prompt={suggestion.prompt} send asChild>
            <Button
              variant="ghost"
              className={cn(
                'aui-thread-welcome-suggestion dark:hover:bg-accent/60 h-auto w-full border text-left whitespace-break-spaces',
                d('text-base'),
                isStandalone
                  ? `flex-row items-center ${d('gap-sm')} ${d('px-md')} ${d('py-sm')} ${r('full')}`
                  : `w-full flex-1 flex-col flex-wrap items-start justify-start ${d('gap-sm')} ${d('px-lg')} ${d('py-md')} ${r('xl')}`
              )}
              aria-label={suggestion.prompt}
            >
              <span className="aui-thread-welcome-suggestion-text-1 text-foreground text-sm font-medium">
                {suggestion.title}
              </span>
              <span className="aui-thread-welcome-suggestion-text-2 text-muted-foreground text-sm">
                {suggestion.label}
              </span>
            </Button>
          </ThreadPrimitive.Suggestion>
        </m.div>
      ))}
    </div>
  )
}

/**
 * Component that handles tool mentions (@tool) in the composer.
 * Shows autocomplete dropdown and badges for mentioned tools.
 */
const ComposerToolMentions: FC<{
  tools: Record<string, unknown> | undefined
}> = ({ tools }) => {
  const containerRef = useRef<HTMLDivElement>(null)

  const {
    mentionableTools,
    mentionedToolIds,
    value,
    cursorPosition,
    textareaRef,
    updateCursorPosition,
    handleAutocompleteChange,
    removeMention,
    isActive,
  } = useToolMentions({ tools })

  // Find and attach to the textarea within the composer
  useEffect(() => {
    if (!isActive) return

    const findTextarea = () => {
      const textarea = document.querySelector(
        '.aui-composer-input'
      ) as HTMLTextAreaElement | null
      if (textarea && textareaRef.current !== textarea) {
        textareaRef.current = textarea

        const handleSelectionChange = () => updateCursorPosition()
        textarea.addEventListener('click', handleSelectionChange)
        textarea.addEventListener('keyup', handleSelectionChange)
        textarea.addEventListener('input', handleSelectionChange)

        return () => {
          textarea.removeEventListener('click', handleSelectionChange)
          textarea.removeEventListener('keyup', handleSelectionChange)
          textarea.removeEventListener('input', handleSelectionChange)
        }
      }
    }

    const cleanup = findTextarea()

    const observer = new MutationObserver(() => {
      findTextarea()
    })

    observer.observe(document.body, {
      childList: true,
      subtree: true,
    })

    return () => {
      cleanup?.()
      observer.disconnect()
    }
  }, [isActive, textareaRef, updateCursorPosition])

  if (!isActive) {
    return null
  }

  return (
    <div ref={containerRef} className="aui-composer-tool-mentions relative">
      {/* Badges showing mentioned tools */}
      <MentionedToolsBadges
        mentionedToolIds={mentionedToolIds}
        tools={mentionableTools}
        onRemove={removeMention}
      />

      {/* Autocomplete dropdown */}
      <AnimatePresence>
        <ToolMentionAutocomplete
          tools={mentionableTools}
          value={value}
          cursorPosition={cursorPosition}
          onValueChange={handleAutocompleteChange}
          textareaRef={textareaRef}
        />
      </AnimatePresence>
    </div>
  )
}

// Resets feedbackHidden when a new message starts generating
const FeedbackHiddenResetter: FC = () => {
  const { resetFeedbackHidden } = useChatResolution()

  useEffect(() => {
    resetFeedbackHidden()
  }, [resetFeedbackHidden])

  return null
}

const ComposerFeedback: FC = () => {
  const { isResolved, feedbackHidden, setResolved } = useChatResolution()

  return (
    <ThreadPrimitive.If empty={false}>
      {/* Reset feedbackHidden when a new message starts generating */}
      <ThreadPrimitive.If running>
        <FeedbackHiddenResetter />
      </ThreadPrimitive.If>
      <ThreadPrimitive.If running={false}>
        <AnimatePresence>
          {!isResolved && !feedbackHidden && (
            <m.div
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: 10 }}
              transition={{ duration: 0.2, ease: EASE_OUT_QUINT }}
              className="mb-3"
            >
              <MessageFeedback className="mx-auto" onResolved={setResolved} />
            </m.div>
          )}
        </AnimatePresence>
      </ThreadPrimitive.If>
    </ThreadPrimitive.If>
  )
}

interface ComposerProps {
  showFeedback?: boolean
}

const Composer: FC<ComposerProps> = ({ showFeedback = false }) => {
  const { config, mcpTools } = useElements()
  const { isResolved, setUnresolved } = useChatResolution()
  const r = useRadius()
  const d = useDensity()
  const composerConfig = config.composer ?? {
    placeholder: 'Send a message...',
    attachments: true,
  }
  const components = config.components ?? {}

  // Determine if tool mentions are enabled (default: true)
  const toolMentionsEnabled =
    composerConfig.toolMentions === undefined ||
    composerConfig.toolMentions === true ||
    (typeof composerConfig.toolMentions === 'object' &&
      composerConfig.toolMentions.enabled !== false)

  const composerRootRef = useRef<HTMLFormElement>(null)

  if (components.Composer) {
    return <components.Composer />
  }

  return (
    <div
      className={cn(
        'aui-composer-wrapper bg-background sticky bottom-0 z-[60] flex w-full flex-col overflow-visible',
        d('gap-md'),
        d('py-md'),
        r('xl')
      )}
    >
      {showFeedback && <ComposerFeedback />}
      <ThreadScrollToBottom />
      {showFeedback && isResolved ? (
        <m.div
          className="aui-composer-resolved border-input flex min-h-[118px] flex-col items-center justify-center gap-2 border-t px-1"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.2, ease: EASE_OUT_QUINT }}
        >
          <span className="text-muted-foreground text-sm">
            This conversation has been resolved
          </span>
          <Button
            variant="outline"
            size="sm"
            className="text-foreground"
            onClick={setUnresolved}
          >
            Reopen conversation
          </Button>
        </m.div>
      ) : (
        <ComposerPrimitive.Root
          ref={composerRootRef}
          className={cn(
            'aui-composer-root group/input-group border-input bg-background has-[textarea:focus-visible]:border-ring has-[textarea:focus-visible]:ring-ring/5 dark:bg-background relative flex min-h-[118px] w-full flex-col border px-1 pt-2 shadow-xs transition-[color,box-shadow] outline-none has-[textarea:focus-visible]:ring-1',
            r('xl')
          )}
        >
          {composerConfig.attachments && <ComposerAttachments />}

          {toolMentionsEnabled && <ComposerToolMentions tools={mcpTools} />}

          <ComposerPrimitive.Input
            placeholder={composerConfig.placeholder}
            className={cn(
              'aui-composer-input text-foreground placeholder:text-muted-foreground mb-1 max-h-32 w-full resize-none bg-transparent px-3.5 pt-1.5 pb-3 outline-none focus-visible:ring-0',
              d('h-input'),
              d('text-base')
            )}
            rows={1}
            autoFocus
            aria-label="Message input"
          />
          <ComposerAction />
        </ComposerPrimitive.Root>
      )}
    </div>
  )
}

const ComposerModelPicker: FC = () => {
  const { model, setModel } = useElements()
  const [popoverOpen, setPopoverOpen] = useState(false)
  const [tooltipOpen, setTooltipOpen] = useState(false)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const savedScrollPosition = useRef(0)
  const previousOpenRef = useRef(false)

  useEffect(() => {
    // Restore scroll position when opening
    if (popoverOpen && !previousOpenRef.current) {
      requestAnimationFrame(() => {
        const container = scrollContainerRef.current
        if (container && container.scrollHeight > 0) {
          container.scrollTop = savedScrollPosition.current
        }
      })
    }

    previousOpenRef.current = popoverOpen
  }, [popoverOpen])

  // Close tooltip when popover opens
  useEffect(() => {
    if (popoverOpen) {
      setTooltipOpen(false)
    }
  }, [popoverOpen])

  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    savedScrollPosition.current = e.currentTarget.scrollTop
  }

  return (
    <TooltipProvider>
      <Tooltip open={tooltipOpen && !popoverOpen} onOpenChange={setTooltipOpen}>
        <Popover open={popoverOpen} onOpenChange={setPopoverOpen}>
          <TooltipTrigger asChild>
            <PopoverTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                data-state={popoverOpen ? 'open' : 'closed'}
                className="aui-composer-model-picker data-[state=open]:bg-muted-foreground/15 dark:border-muted-foreground/15 dark:hover:bg-muted-foreground/30 flex w-fit items-center gap-2 rounded-full px-2.5 py-1 text-xs font-semibold"
                aria-label="Model Settings"
              >
                <Settings2 className="aui-attachment-add-icon size-5 stroke-[1.5px]" />
              </Button>
            </PopoverTrigger>
          </TooltipTrigger>
          <PopoverContent
            side="top"
            align="start"
            className="w-min p-0 shadow-none"
          >
            <div
              ref={scrollContainerRef}
              className="max-h-48 overflow-y-auto"
              onScroll={handleScroll}
            >
              {MODELS.map((m) => (
                <Button
                  key={m}
                  onClick={() => {
                    setModel(m)
                  }}
                  variant="ghost"
                  className="w-full justify-start gap-2 rounded-none px-2"
                >
                  {m === model ? (
                    <div>
                      <CheckIcon className="size-4 text-emerald-500" />
                    </div>
                  ) : (
                    <div className="size-4">&nbsp;</div>
                  )}
                  {m}
                </Button>
              ))}
            </div>
          </PopoverContent>
        </Popover>
        <TooltipContent side="bottom" align="start">
          Model Settings
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

const ComposerAction: FC = () => {
  const { config } = useElements()
  const r = useRadius()
  const composerConfig = config.composer ?? { attachments: true }
  return (
    <div className="aui-composer-action-wrapper relative mx-1 mt-2 mb-2 flex items-center justify-between">
      <div className="aui-composer-action-wrapper-inner flex items-center">
        {composerConfig.attachments ? (
          <ComposerAddAttachment />
        ) : (
          <div className="aui-composer-add-attachment-placeholder" />
        )}

        {config.model?.showModelPicker && !config.languageModel && (
          <ComposerModelPicker />
        )}
      </div>

      <ThreadPrimitive.If running={false}>
        <ComposerPrimitive.Send asChild>
          <TooltipIconButton
            tooltip="Send message"
            side="bottom"
            type="submit"
            variant="default"
            size="icon"
            className={cn('aui-composer-send size-[34px] p-1', r('full'))}
            aria-label="Send message"
          >
            <ArrowUpIcon className="aui-composer-send-icon size-5" />
          </TooltipIconButton>
        </ComposerPrimitive.Send>
      </ThreadPrimitive.If>

      <ThreadPrimitive.If running>
        <ComposerPrimitive.Cancel asChild>
          <Button
            type="button"
            variant="default"
            size="icon"
            className={cn(
              'aui-composer-cancel border-muted-foreground/60 hover:bg-primary/75 dark:border-muted-foreground/90 size-[34px] border',
              r('full')
            )}
            aria-label="Stop generating"
          >
            <Square className="aui-composer-cancel-icon size-3.5 fill-white dark:fill-black" />
          </Button>
        </ComposerPrimitive.Cancel>
      </ThreadPrimitive.If>
    </div>
  )
}

const MessageError: FC = () => {
  return (
    <MessagePrimitive.Error>
      <ErrorPrimitive.Root className="aui-message-error-root border-destructive bg-destructive/10 text-destructive dark:bg-destructive/5 mt-2 rounded-md border p-3 text-sm dark:text-red-200">
        <ErrorPrimitive.Message className="aui-message-error-message line-clamp-2" />
      </ErrorPrimitive.Root>
    </MessagePrimitive.Error>
  )
}

/**
 * Shows the pulsing dot indicator when the message is still running but the
 * last rendered part is a tool call (not text). Without this, there's no
 * visual feedback that the model is still working after a tool call.
 */
const ToolCallStreamingIndicator: FC = () => {
  const show = useAssistantState(({ message }) => {
    if (message.status?.type !== 'running') return false
    const lastPart = message.parts[message.parts.length - 1]
    return lastPart?.type === 'tool-call'
  })
  if (!show) return null
  return <div className="aui-md mt-2" data-status="running" />
}

const AssistantMessage: FC = () => {
  const { config } = useElements()
  const toolsConfig = config.tools ?? {}
  const components = config.components ?? {}

  const partsComponents = useMemo(
    () => ({
      Text: components.Text ?? MarkdownText,
      Image: components.Image ?? Image,
      tools: {
        by_name: toolsConfig.components,
        Fallback: components.ToolFallback ?? ToolFallback,
      },
      Reasoning: components.Reasoning ?? Reasoning,
      ReasoningGroup: components.ReasoningGroup ?? ReasoningGroup,
      ToolGroup: components.ToolGroup ?? ToolGroup,
    }),
    [components, toolsConfig.components]
  )

  return (
    <MessagePrimitive.Root asChild>
      <div
        className="aui-assistant-message-root animate-in fade-in slide-in-from-bottom-1 relative mx-auto w-full py-4 duration-150 ease-out last:mb-24"
        data-role="assistant"
      >
        <div className="aui-assistant-message-content text-foreground mx-2 leading-7 wrap-break-word">
          <MessagePrimitive.Parts components={partsComponents} />
          <ToolCallStreamingIndicator />
          <MessageError />
        </div>

        <div className="aui-assistant-message-footer mt-2 ml-2 flex items-center gap-3">
          {/* <BranchPicker /> */}
          <AssistantActionBar />
        </div>
      </div>
    </MessagePrimitive.Root>
  )
}

const Image: FC<ImageMessagePartProps> = (props) => {
  return <img src={props.image} />
}

const AssistantActionBar: FC = () => {
  return (
    <ActionBarPrimitive.Root
      hideWhenRunning
      autohide="not-last"
      autohideFloat="single-branch"
      className="aui-assistant-action-bar-root text-muted-foreground data-floating:bg-background col-start-3 row-start-2 -ml-1 flex gap-1 data-floating:absolute data-floating:rounded-md data-floating:border data-floating:p-1 data-floating:shadow-sm"
    >
      <ActionBarPrimitive.Copy asChild>
        <TooltipIconButton tooltip="Copy">
          <MessagePrimitive.If copied>
            <CheckIcon />
          </MessagePrimitive.If>
          <MessagePrimitive.If copied={false}>
            <CopyIcon />
          </MessagePrimitive.If>
        </TooltipIconButton>
      </ActionBarPrimitive.Copy>
      {/* <ActionBarPrimitive.Reload asChild>
        <TooltipIconButton tooltip="Refresh">
          <RefreshCwIcon />
        </TooltipIconButton>
      </ActionBarPrimitive.Reload> */}
    </ActionBarPrimitive.Root>
  )
}

const UserMessage: FC = () => {
  const r = useRadius()
  return (
    <MessagePrimitive.Root asChild>
      <div
        className="aui-user-message-root animate-in fade-in slide-in-from-bottom-1 mx-auto grid w-full auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] gap-y-2 px-2 py-4 duration-150 ease-out first:mt-3 last:mb-5 [&:where(>*)]:col-start-2"
        data-role="user"
      >
        <UserMessageAttachments />

        <div className="aui-user-message-content-wrapper relative col-start-2 min-w-0">
          <div
            className={cn(
              'aui-user-message-content bg-muted text-foreground px-5 py-2.5 wrap-break-word',
              r('xl')
            )}
          >
            <MessagePrimitive.Parts />
          </div>
          <div className="aui-user-action-bar-wrapper absolute top-1/2 left-0 -translate-x-full -translate-y-1/2 pr-2">
            <UserActionBar />
          </div>
        </div>

        <BranchPicker className="aui-user-branch-picker col-span-full col-start-1 row-start-3 -mr-1 justify-end" />
      </div>
    </MessagePrimitive.Root>
  )
}

const UserActionBar: FC = () => {
  return (
    <ActionBarPrimitive.Root
      hideWhenRunning
      autohide="not-last"
      className="aui-user-action-bar-root flex flex-col items-end"
    >
      <ActionBarPrimitive.Edit asChild>
        <TooltipIconButton tooltip="Edit" className="aui-user-action-edit p-4">
          <PencilIcon />
        </TooltipIconButton>
      </ActionBarPrimitive.Edit>
    </ActionBarPrimitive.Root>
  )
}

const EditComposer: FC = () => {
  return (
    <div className="aui-edit-composer-wrapper mx-auto flex w-full flex-col gap-4 px-2 first:mt-4">
      <ComposerPrimitive.Root className="aui-edit-composer-root bg-muted ml-auto flex w-full max-w-7/8 flex-col rounded-xl">
        <ComposerPrimitive.Input
          className="aui-edit-composer-input text-foreground flex min-h-[60px] w-full resize-none bg-transparent p-4 outline-none"
          autoFocus
        />

        <div className="aui-edit-composer-footer mx-3 mb-3 flex items-center justify-center gap-2 self-end">
          <ComposerPrimitive.Cancel asChild>
            <Button variant="ghost" size="sm" aria-label="Cancel edit">
              Cancel
            </Button>
          </ComposerPrimitive.Cancel>
          <ComposerPrimitive.Send asChild>
            <Button size="sm" aria-label="Update message">
              Update
            </Button>
          </ComposerPrimitive.Send>
        </div>
      </ComposerPrimitive.Root>
    </div>
  )
}

const BranchPicker: FC<BranchPickerPrimitive.Root.Props> = ({
  className,
  ...rest
}) => {
  return (
    <BranchPickerPrimitive.Root
      hideWhenSingleBranch
      className={cn(
        'aui-branch-picker-root text-muted-foreground mr-2 -ml-2 inline-flex items-center text-xs',
        className
      )}
      {...rest}
    >
      <BranchPickerPrimitive.Previous asChild>
        <TooltipIconButton tooltip="Previous">
          <ChevronLeftIcon />
        </TooltipIconButton>
      </BranchPickerPrimitive.Previous>
      <span className="aui-branch-picker-state font-medium">
        <BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />
      </span>
      <BranchPickerPrimitive.Next asChild>
        <TooltipIconButton tooltip="Next">
          <ChevronRightIcon />
        </TooltipIconButton>
      </BranchPickerPrimitive.Next>
    </BranchPickerPrimitive.Root>
  )
}
