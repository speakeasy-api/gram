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

import { LazyMotion, MotionConfig, domAnimation } from 'motion/react'
import * as m from 'motion/react-m'
import { useEffect, useRef, useState, type FC } from 'react'

import {
  ComposerAddAttachment,
  ComposerAttachments,
  UserMessageAttachments,
} from '@/components/assistant-ui/attachment'
import { MarkdownText } from '@/components/assistant-ui/markdown-text'
import { Reasoning, ReasoningGroup } from '@/components/assistant-ui/reasoning'
import { ToolFallback } from '@/components/assistant-ui/tool-fallback'
import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import { Button } from '@/components/ui/button'

import { useDensity } from '@/hooks/useDensity'
import { useElements } from '@/hooks/useElements'
import { useRadius } from '@/hooks/useRadius'
import { useThemeProps } from '@/hooks/useThemeProps'
import { EASE_OUT_QUINT } from '@/lib/easing'
import { MODELS } from '@/lib/models'
import { cn } from '@/lib/utils'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '../ui/tooltip'
import { ToolGroup } from './tool-group'

const ApiKeyWarning = () => (
  <div className="gramel:m-2 gramel:rounded-md gramel:border gramel:border-amber-500 gramel:bg-amber-100 gramel:px-4 gramel:py-3 gramel:text-sm gramel:text-amber-800 gramel:dark:border-amber-600 gramel:dark:bg-amber-900/30 gramel:dark:text-amber-200">
    <strong>Warning:</strong> You are using an API key directly in the client.
    Please{' '}
    <a
      href="https://github.com/speakeasy-api/gram/tree/main/elements#setting-up-your-backend"
      target="_blank"
      rel="noopener noreferrer"
      className="gramel:text-amber-700 gramel:underline gramel:hover:text-amber-800 gramel:dark:text-amber-300 gramel:dark:hover:text-amber-200"
    >
      set up a session endpoint
    </a>{' '}
    before deploying to production.
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
  const showApiKeyWarning = config.api && 'UNSAFE_apiKey' in config.api

  return (
    <LazyMotion features={domAnimation}>
      <MotionConfig reducedMotion="user">
        <ThreadPrimitive.Root
          className={cn('aui-root aui-thread-root gramel:bg-background @container gramel:flex gramel:h-full gramel:flex-col',
            themeProps.className,
            className
          )}
        >
          <ThreadPrimitive.Viewport
            className={cn('aui-thread-viewport gramel:relative gramel:mx-auto gramel:flex gramel:w-full gramel:flex-1 gramel:flex-col gramel:overflow-x-auto gramel:overflow-y-scroll gramel:pb-0!',
              d('gramel:p-lg')
            )}
          >
            <ThreadPrimitive.If empty>
              {components.ThreadWelcome ? (
                <components.ThreadWelcome />
              ) : (
                <ThreadWelcome />
              )}
            </ThreadPrimitive.If>

            {showApiKeyWarning && <ApiKeyWarning />}

            <ThreadPrimitive.Messages
              components={{
                UserMessage: components.UserMessage ?? UserMessage,
                EditComposer: components.EditComposer ?? EditComposer,
                AssistantMessage:
                  components.AssistantMessage ?? AssistantMessage,
              }}
            />

            <ThreadPrimitive.If empty={false}>
              <div className="aui-thread-viewport-spacer gramel:min-h-8 gramel:grow" />
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
        className="aui-thread-scroll-to-bottom gramel:dark:bg-background gramel:dark:hover:bg-accent gramel:absolute gramel:-top-12 gramel:z-10 gramel:self-center gramel:rounded-full gramel:p-4 gramel:disabled:invisible"
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
      className={cn('aui-thread-welcome-root gramel:my-auto gramel:flex gramel:w-full gramel:grow gramel:flex-col',
        isStandalone ? 'gramel:items-center gramel:justify-center' : '',
        d('gramel:gap-lg')
      )}
    >
      <div
        className={cn('aui-thread-welcome-center gramel:flex gramel:w-full gramel:grow gramel:flex-col gramel:items-center gramel:justify-start'
        )}
      >
        <div
          className={cn('aui-thread-welcome-message gramel:flex gramel:flex-col',
            isStandalone
              ? 'gramel:items-center gramel:text-center'
              : 'gramel:size-full gramel:justify-start',
            d('gramel:gap-sm'),
            !isStandalone && d('gramel:py-md')
          )}
        >
          <m.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 10 }}
            transition={{ duration: 0.25, ease: EASE_OUT_QUINT }}
            className={cn('aui-thread-welcome-message-motion-1 gramel:text-foreground gramel:font-semibold',
              d('gramel:text-title')
            )}
          >
            {title}
          </m.div>
          <m.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 10 }}
            transition={{ duration: 0.25, delay: 0.05, ease: EASE_OUT_QUINT }}
            className={cn('aui-thread-welcome-message-motion-2 gramel:text-muted-foreground/65',
              d('gramel:text-subtitle')
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
      className={cn('aui-thread-welcome-suggestions gramel:w-full',
        d('gramel:gap-md'),
        d('gramel:py-lg'),
        isStandalone
          ? 'gramel:flex gramel:flex-wrap gramel:items-center gramel:justify-center'
          : suggestions.length === 1
            ? 'gramel:flex'
            : 'gramel:grid gramel:max-w-fit @md:gramel:grid-cols-2'
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
          className={cn('aui-thread-welcome-suggestion-display',
            !isStandalone && 'gramel:nth-[n+3]:hidden @md:gramel:nth-[n+3]:block'
          )}
        >
          <ThreadPrimitive.Suggestion prompt={suggestion.action} send asChild>
            <Button
              variant="ghost"
              className={cn('aui-thread-welcome-suggestion gramel:dark:hover:bg-accent/60 gramel:h-auto gramel:w-full gramel:border gramel:text-left gramel:whitespace-break-spaces',
                d('gramel:text-base'),
                isStandalone
                  ? `gramel:flex-row gramel:items-center ${d('gramel:gap-sm')} ${d('gramel:px-md')} ${d('gramel:py-sm')} ${r('full')}`
                  : `gramel:w-full gramel:flex-1 gramel:flex-col gramel:flex-wrap gramel:items-start gramel:justify-start ${d('gramel:gap-sm')} ${d('gramel:px-lg')} ${d('gramel:py-md')} ${r('xl')}`
              )}
              aria-label={suggestion.action}
            >
              <span className="aui-thread-welcome-suggestion-text-1 gramel:text-foreground gramel:text-sm gramel:font-medium">
                {suggestion.title}
              </span>
              <span className="aui-thread-welcome-suggestion-text-2 gramel:text-muted-foreground gramel:text-sm">
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
      className={cn('aui-composer-wrapper gramel:bg-background gramel:sticky gramel:bottom-0 gramel:flex gramel:w-full gramel:flex-col gramel:overflow-visible',
        d('gramel:gap-md'),
        d('gramel:py-md'),
        r('xl')
      )}
    >
      <ThreadScrollToBottom />
      <ComposerPrimitive.Root
        className={cn('aui-composer-root gramel:group/input-group gramel:border-input gramel:bg-background gramel:has-[textarea:focus-visible]:border-ring gramel:has-[textarea:focus-visible]:ring-ring/5 gramel:dark:bg-background gramel:relative gramel:flex gramel:w-full gramel:flex-col gramel:border gramel:px-1 gramel:pt-2 gramel:shadow-xs gramel:transition-[color,box-shadow] gramel:outline-none gramel:has-[textarea:focus-visible]:ring-1',
          r('xl')
        )}
      >
        {composerConfig.attachments && <ComposerAttachments />}

        <ComposerPrimitive.Input
          placeholder={composerConfig.placeholder}
          className={cn('aui-composer-input placeholder:text-muted-foreground gramel:mb-1 gramel:max-h-32 gramel:w-full gramel:resize-none gramel:bg-transparent gramel:px-3.5 gramel:pt-1.5 gramel:pb-3 gramel:outline-none gramel:focus-visible:ring-0',
            d('gramel:h-input'),
            d('gramel:text-base')
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
                className="aui-composer-model-picker data-[state=open]:bg-muted-foreground/15 gramel:dark:border-muted-foreground/15 gramel:dark:hover:bg-muted-foreground/30 gramel:flex gramel:w-fit gramel:items-center gramel:gap-2 gramel:rounded-full gramel:px-2.5 gramel:py-1 gramel:text-xs gramel:font-semibold"
                aria-label="Model Settings"
              >
                <Settings2 className="aui-attachment-add-icon gramel:size-5 gramel:stroke-[1.5px]" />
              </Button>
            </PopoverTrigger>
          </TooltipTrigger>
          <PopoverContent
            side="top"
            align="start"
            className="gramel:w-min gramel:p-0 gramel:shadow-none"
          >
            <div
              ref={scrollContainerRef}
              className="gramel:max-h-48 gramel:overflow-y-auto"
              onScroll={handleScroll}
            >
              {MODELS.map((m) => (
                <Button
                  key={m}
                  onClick={() => {
                    setModel(m)
                  }}
                  variant="ghost"
                  className="gramel:w-full gramel:justify-start gramel:gap-2 gramel:rounded-none gramel:px-2"
                >
                  {m === model ? (
                    <div>
                      <CheckIcon className="gramel:size-4 gramel:text-emerald-500" />
                    </div>
                  ) : (
                    <div className="gramel:size-4">&nbsp;</div>
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
    <div className="aui-composer-action-wrapper gramel:relative gramel:mx-1 gramel:mt-2 gramel:mb-2 gramel:flex gramel:items-center gramel:justify-between">
      <div className="aui-composer-action-wrapper-inner gramel:flex gramel:items-center">
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
            className={cn('aui-composer-send gramel:size-[34px] gramel:p-1', r('full'))}
            aria-label="Send message"
          >
            <ArrowUpIcon className="aui-composer-send-icon gramel:size-5" />
          </TooltipIconButton>
        </ComposerPrimitive.Send>
      </ThreadPrimitive.If>

      <ThreadPrimitive.If running>
        <ComposerPrimitive.Cancel asChild>
          <Button
            type="button"
            variant="default"
            size="icon"
            className={cn('aui-composer-cancel gramel:border-muted-foreground/60 gramel:hover:bg-primary/75 gramel:dark:border-muted-foreground/90 gramel:size-[34px] gramel:border',
              r('full')
            )}
            aria-label="Stop generating"
          >
            <Square className="aui-composer-cancel-icon gramel:size-3.5 gramel:fill-white gramel:dark:fill-black" />
          </Button>
        </ComposerPrimitive.Cancel>
      </ThreadPrimitive.If>
    </div>
  )
}

const MessageError: FC = () => {
  return (
    <MessagePrimitive.Error>
      <ErrorPrimitive.Root className="aui-message-error-root gramel:border-destructive gramel:bg-destructive/10 gramel:text-destructive gramel:dark:bg-destructive/5 gramel:mt-2 gramel:rounded-md gramel:border gramel:p-3 gramel:text-sm gramel:dark:text-red-200">
        <ErrorPrimitive.Message className="aui-message-error-message gramel:line-clamp-2" />
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
        className="aui-assistant-message-root gramel:animate-in gramel:fade-in gramel:slide-in-from-bottom-1 gramel:relative gramel:mx-auto gramel:w-full gramel:py-4 gramel:duration-150 gramel:ease-out gramel:last:mb-24"
        data-role="assistant"
      >
        <div className="aui-assistant-message-content gramel:text-foreground gramel:mx-2 gramel:leading-7 gramel:wrap-break-word">
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

        <div className="aui-assistant-message-footer gramel:mt-2 gramel:ml-2 gramel:flex">
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
      className="aui-assistant-action-bar-root gramel:text-muted-foreground gramel:data-floating:bg-background gramel:col-start-3 gramel:row-start-2 gramel:-ml-1 gramel:flex gramel:gap-1 gramel:data-floating:absolute gramel:data-floating:rounded-md gramel:data-floating:border gramel:data-floating:p-1 gramel:data-floating:shadow-sm"
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
        className="aui-user-message-root gramel:animate-in gramel:fade-in gramel:slide-in-from-bottom-1 gramel:mx-auto gramel:grid gramel:w-full gramel:auto-rows-auto gramel:grid-cols-[minmax(72px,1fr)_auto] gramel:gap-y-2 gramel:px-2 gramel:py-4 gramel:duration-150 gramel:ease-out gramel:first:mt-3 gramel:last:mb-5 gramel:[&:where(>*)]:col-start-2"
        data-role="user"
      >
        <UserMessageAttachments />

        <div className="aui-user-message-content-wrapper gramel:relative gramel:col-start-2 gramel:min-w-0">
          <div
            className={cn('aui-user-message-content gramel:bg-muted gramel:text-foreground gramel:px-5 gramel:py-2.5 gramel:wrap-break-word',
              r('xl')
            )}
          >
            <MessagePrimitive.Parts />
          </div>
          <div className="aui-user-action-bar-wrapper gramel:absolute gramel:top-1/2 gramel:left-0 gramel:-translate-x-full gramel:-translate-y-1/2 gramel:pr-2">
            <UserActionBar />
          </div>
        </div>

        <BranchPicker className="aui-user-branch-picker gramel:col-span-full gramel:col-start-1 gramel:row-start-3 gramel:-mr-1 gramel:justify-end" />
      </div>
    </MessagePrimitive.Root>
  )
}

const UserActionBar: FC = () => {
  return (
    <ActionBarPrimitive.Root
      hideWhenRunning
      autohide="not-last"
      className="aui-user-action-bar-root gramel:flex gramel:flex-col gramel:items-end"
    >
      <ActionBarPrimitive.Edit asChild>
        <TooltipIconButton tooltip="Edit" className="aui-user-action-edit gramel:p-4">
          <PencilIcon />
        </TooltipIconButton>
      </ActionBarPrimitive.Edit>
    </ActionBarPrimitive.Root>
  )
}

const EditComposer: FC = () => {
  return (
    <div className="aui-edit-composer-wrapper gramel:mx-auto gramel:flex gramel:w-full gramel:flex-col gramel:gap-4 gramel:px-2 gramel:first:mt-4">
      <ComposerPrimitive.Root className="aui-edit-composer-root gramel:bg-muted gramel:ml-auto gramel:flex gramel:w-full gramel:max-w-7/8 gramel:flex-col gramel:rounded-xl">
        <ComposerPrimitive.Input
          className="aui-edit-composer-input gramel:text-foreground gramel:flex gramel:min-h-[60px] gramel:w-full gramel:resize-none gramel:bg-transparent gramel:p-4 gramel:outline-none"
          autoFocus
        />

        <div className="aui-edit-composer-footer gramel:mx-3 gramel:mb-3 gramel:flex gramel:items-center gramel:justify-center gramel:gap-2 gramel:self-end">
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
      className={cn('aui-branch-picker-root gramel:text-muted-foreground gramel:mr-2 gramel:-ml-2 gramel:inline-flex gramel:items-center gramel:text-xs',
        className
      )}
      {...rest}
    >
      <BranchPickerPrimitive.Previous asChild>
        <TooltipIconButton tooltip="Previous">
          <ChevronLeftIcon />
        </TooltipIconButton>
      </BranchPickerPrimitive.Previous>
      <span className="aui-branch-picker-state gramel:font-medium">
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
