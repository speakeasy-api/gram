import {
  ArrowDownIcon,
  ArrowUpIcon,
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CopyIcon,
  PencilIcon,
  RefreshCwIcon,
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
} from '@assistant-ui/react'

import { useState, useEffect, useRef, type FC } from 'react'
import { LazyMotion, MotionConfig, domAnimation } from 'motion/react'
import * as m from 'motion/react-m'

import { Button } from '@/components/ui/button'
import { MarkdownText } from '@/components/assistant-ui/markdown-text'
import { ToolFallback } from '@/components/assistant-ui/tool-fallback'
import { Reasoning, ReasoningGroup } from '@/components/assistant-ui/reasoning'
import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import {
  ComposerAddAttachment,
  ComposerAttachments,
  UserMessageAttachments,
} from '@/components/assistant-ui/attachment'

import { cn } from '@/lib/utils'
import { useElements } from '@/hooks/useElements'
import { useRadius } from '@/hooks/useRadius'
import { useDensity } from '@/hooks/useDensity'
import { useThemeProps } from '@/hooks/useThemeProps'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '../ui/tooltip'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover'
import { MODELS } from '@/lib/models'
import { EASE_OUT_QUINT } from '@/lib/easing'
import { ToolGroup } from './tool-group'

export const Thread: FC = () => {
  const themeProps = useThemeProps()
  const d = useDensity()
  const { config } = useElements()
  const components = config.components ?? {}
  return (
    <LazyMotion features={domAnimation}>
      <MotionConfig reducedMotion="user">
        <ThreadPrimitive.Root
          className={cn(
            'aui-root aui-thread-root bg-background @container flex h-full flex-col',
            themeProps.className
          )}
        >
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

            <ThreadPrimitive.Messages
              components={{
                UserMessage: components.UserMessage ?? UserMessage,
                EditComposer: components.EditComposer ?? EditComposer,
                AssistantMessage:
                  components.AssistantMessage ?? AssistantMessage,
              }}
            />

            <ThreadPrimitive.If empty={false}>
              <div className="aui-thread-viewport-spacer min-h-8 grow" />
            </ThreadPrimitive.If>

            <Composer />
          </ThreadPrimitive.Viewport>
        </ThreadPrimitive.Root>
      </MotionConfig>
    </LazyMotion>
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
              'aui-thread-welcome-message-motion-1 font-semibold',
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
            : 'grid @md:grid-cols-2'
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
          <ThreadPrimitive.Suggestion prompt={suggestion.action} send asChild>
            <Button
              variant="ghost"
              className={cn(
                'aui-thread-welcome-suggestion dark:hover:bg-accent/60 h-auto w-full border text-left whitespace-break-spaces',
                d('text-base'),
                isStandalone
                  ? `flex-row items-center ${d('gap-sm')} ${d('px-md')} ${d('py-sm')} ${r('full')}`
                  : `w-full flex-1 flex-col flex-wrap items-start justify-start ${d('gap-sm')} ${d('px-lg')} ${d('py-md')} ${r('xl')}`
              )}
              aria-label={suggestion.action}
            >
              <span className="aui-thread-welcome-suggestion-text-1 text-sm font-medium">
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

const Composer: FC = () => {
  const { config } = useElements()
  const r = useRadius()
  const d = useDensity()
  const composerConfig = config.composer ?? {
    placeholder: 'Send a message...',
    attachments: true,
  }
  const components = config.components ?? {}

  if (components.Composer) {
    return <components.Composer />
  }

  return (
    <div
      className={cn(
        'aui-composer-wrapper bg-background sticky bottom-0 flex w-full flex-col overflow-visible',
        d('gap-md'),
        d('py-md'),
        r('xl')
      )}
    >
      <ThreadScrollToBottom />
      <ComposerPrimitive.Root
        className={cn(
          'aui-composer-root group/input-group border-input bg-background has-[textarea:focus-visible]:border-ring has-[textarea:focus-visible]:ring-ring/5 dark:bg-background relative flex w-full flex-col border px-1 pt-2 shadow-xs transition-[color,box-shadow] outline-none has-[textarea:focus-visible]:ring-1',
          r('xl')
        )}
      >
        {composerConfig.attachments && <ComposerAttachments />}

        <ComposerPrimitive.Input
          placeholder={composerConfig.placeholder}
          className={cn(
            'aui-composer-input placeholder:text-muted-foreground mb-1 max-h-32 w-full resize-none bg-transparent px-3.5 pt-1.5 pb-3 outline-none focus-visible:ring-0',
            d('h-input'),
            d('text-base')
          )}
          rows={1}
          autoFocus
          aria-label="Message input"
        />
        <ComposerAction />
      </ComposerPrimitive.Root>
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

  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    savedScrollPosition.current = e.currentTarget.scrollTop
  }

  return (
    <TooltipProvider>
      <Tooltip open={tooltipOpen} onOpenChange={setTooltipOpen}>
        <Popover open={popoverOpen} onOpenChange={setPopoverOpen}>
          <TooltipTrigger asChild>
            <PopoverTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                data-state={popoverOpen ? 'open' : 'closed'}
                className="aui-composer-model-picker data-[state=open]:bg-muted-foreground/15 dark:border-muted-foreground/15 dark:hover:bg-muted-foreground/30 flex w-fit items-center gap-2 rounded-full px-2.5 py-1 text-xs font-semibold"
                aria-label="Model Settings"
                onPointerUp={(e) => e.stopPropagation()}
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

        {config.model?.showModelPicker && <ComposerModelPicker />}
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

const AssistantMessage: FC = () => {
  const { config } = useElements()
  const toolsConfig = config.tools ?? {}
  const components = config.components ?? {}
  return (
    <MessagePrimitive.Root asChild>
      <div
        className="aui-assistant-message-root animate-in fade-in slide-in-from-bottom-1 relative mx-auto w-full py-4 duration-150 ease-out last:mb-24"
        data-role="assistant"
      >
        <div className="aui-assistant-message-content text-foreground mx-2 leading-7 wrap-break-word">
          <MessagePrimitive.Parts
            components={{
              Text: components.Text ?? MarkdownText,
              Image: components.Image ?? Image,
              tools: {
                by_name: toolsConfig.components,
                Fallback: components.ToolFallback ?? ToolFallback,
              },
              Reasoning: components.Reasoning ?? Reasoning,
              ReasoningGroup: components.ReasoningGroup ?? ReasoningGroup,
              ToolGroup: components.ToolGroup ?? ToolGroup,
            }}
          />
          <MessageError />
        </div>

        <div className="aui-assistant-message-footer mt-2 ml-2 flex">
          <BranchPicker />
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
      <ActionBarPrimitive.Reload asChild>
        <TooltipIconButton tooltip="Refresh">
          <RefreshCwIcon />
        </TooltipIconButton>
      </ActionBarPrimitive.Reload>
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
