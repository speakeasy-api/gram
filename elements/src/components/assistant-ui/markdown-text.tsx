'use client'

import {
  type CodeHeaderProps,
  MarkdownTextPrimitive,
  unstable_memoizeMarkdownComponents as memoizeMarkdownComponents,
  useIsMarkdownCodeBlock,
} from '@assistant-ui/react-markdown'
import { CheckIcon, CopyIcon } from 'lucide-react'
import { type FC, memo, useState, useCallback, useRef, useEffect } from 'react'
import remarkGfm from 'remark-gfm'

import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import { cn } from '@/lib/utils'
import { useElements } from '@/hooks/useElements'
import { useComponentsByLanguage } from '@/hooks/usePluginComponents'
import { useAssistantState } from '@assistant-ui/react'
import {
  useSubAgentOptional,
  parseAgentEventsFromContent,
  getEventKey,
} from '@/contexts/SubAgentContext'

const MarkdownTextImpl = () => {
  const { plugins } = useElements()
  const componentsByLanguage = useComponentsByLanguage(plugins)
  const subAgentContext = useSubAgentOptional()

  // Track which events we've already processed to avoid duplicate handling
  const processedEventsRef = useRef<Set<string>>(new Set())

  // Queue for pending events to process after render (avoids setState during render)
  const pendingEventsRef = useRef<import('@/types/agents').SubAgentEvent[]>([])

  // Process pending events after render to avoid "setState during render" error
  useEffect(() => {
    if (pendingEventsRef.current.length > 0 && subAgentContext) {
      const events = pendingEventsRef.current
      pendingEventsRef.current = []
      for (const event of events) {
        subAgentContext.handleEvent(event)
      }
    }
  })

  // Preprocess function that extracts agent events and returns cleaned content
  // Note: preprocess receives the FULL accumulated text each time, not deltas
  const preprocess = useCallback(
    (text: string): string => {
      // Parse the content for agent events
      // The function handles partial markers by stripping them from cleanContent
      const { cleanContent, events } = parseAgentEventsFromContent(text)

      // If no SubAgentContext, just strip markers without processing events
      if (!subAgentContext) {
        return cleanContent
      }

      // Queue new events for processing after render
      for (const event of events) {
        const eventKey = getEventKey(event)
        if (!processedEventsRef.current.has(eventKey)) {
          processedEventsRef.current.add(eventKey)
          pendingEventsRef.current.push(event)
        }
      }

      return cleanContent
    },
    [subAgentContext]
  )

  return (
    <MarkdownTextPrimitive
      remarkPlugins={[remarkGfm]}
      className="aui-md"
      components={defaultComponents}
      componentsByLanguage={componentsByLanguage}
      preprocess={preprocess}
    />
  )
}

export const MarkdownText = memo(MarkdownTextImpl)

const CodeHeader: FC<CodeHeaderProps> = ({ language, code }) => {
  const message = useAssistantState(({ message }) => message)
  const messageIsComplete = message.status?.type === 'complete'
  const { isCopied, copyToClipboard } = useCopyToClipboard()
  const onCopy = () => {
    if (!code || isCopied) return
    copyToClipboard(code)
  }

  if (!messageIsComplete) {
    return null
  }
  return (
    <div className="aui-code-header-root bg-muted-foreground/15 text-foreground dark:bg-muted-foreground/20 mt-4 flex items-center justify-between gap-4 rounded-t-lg px-4 py-2 text-sm font-semibold">
      <span className="aui-code-header-language lowercase [&>span]:text-xs">
        {language}
      </span>
      <TooltipIconButton tooltip="Copy" onClick={onCopy}>
        {!isCopied && <CopyIcon />}
        {isCopied && <CheckIcon />}
      </TooltipIconButton>
    </div>
  )
}

const useCopyToClipboard = ({
  copiedDuration = 3000,
}: {
  copiedDuration?: number
} = {}) => {
  const [isCopied, setIsCopied] = useState<boolean>(false)

  const copyToClipboard = (value: string) => {
    if (!value) return

    navigator.clipboard.writeText(value).then(() => {
      setIsCopied(true)
      setTimeout(() => setIsCopied(false), copiedDuration)
    })
  }

  return { isCopied, copyToClipboard }
}

const defaultComponents = memoizeMarkdownComponents({
  h1: ({ className, ...props }) => (
    <h1
      className={cn(
        'aui-md-h1 mb-8 scroll-m-20 text-4xl font-extrabold tracking-tight last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h2: ({ className, ...props }) => (
    <h2
      className={cn(
        'aui-md-h2 mt-8 mb-4 scroll-m-20 text-3xl font-semibold tracking-tight first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h3: ({ className, ...props }) => (
    <h3
      className={cn(
        'aui-md-h3 mt-6 mb-4 scroll-m-20 text-2xl font-semibold tracking-tight first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h4: ({ className, ...props }) => (
    <h4
      className={cn(
        'aui-md-h4 mt-6 mb-4 scroll-m-20 text-xl font-semibold tracking-tight first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h5: ({ className, ...props }) => (
    <h5
      className={cn(
        'aui-md-h5 my-4 text-lg font-semibold first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h6: ({ className, ...props }) => (
    <h6
      className={cn(
        'aui-md-h6 my-4 font-semibold first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  p: ({ className, ...props }) => (
    <p
      className={cn(
        'aui-md-p mt-5 mb-5 leading-7 first:mt-0 last:mb-0',
        className
      )}
      {...props}
    />
  ),
  a: ({ className, ...props }) => (
    <a
      className={cn(
        'aui-md-a text-primary font-medium underline underline-offset-4',
        className
      )}
      {...props}
    />
  ),
  blockquote: ({ className, ...props }) => (
    <blockquote
      className={cn('aui-md-blockquote border-l-2 pl-6 italic', className)}
      {...props}
    />
  ),
  ul: ({ className, ...props }) => (
    <ul
      className={cn('aui-md-ul my-5 ml-6 list-disc [&>li]:mt-2', className)}
      {...props}
    />
  ),
  ol: ({ className, ...props }) => (
    <ol
      className={cn('aui-md-ol my-5 ml-6 list-decimal [&>li]:mt-2', className)}
      {...props}
    />
  ),
  hr: ({ className, ...props }) => (
    <hr className={cn('aui-md-hr my-5 border-b', className)} {...props} />
  ),
  table: ({ className, ...props }) => (
    <table
      className={cn(
        'aui-md-table my-5 w-full border-separate border-spacing-0 overflow-y-auto',
        className
      )}
      {...props}
    />
  ),
  th: ({ className, ...props }) => (
    <th
      className={cn(
        'aui-md-th bg-muted px-4 py-2 text-left font-bold first:rounded-tl-lg last:rounded-tr-lg [[align=center]]:text-center [[align=right]]:text-right',
        className
      )}
      {...props}
    />
  ),
  td: ({ className, ...props }) => (
    <td
      className={cn(
        'aui-md-td border-b border-l px-4 py-2 text-left last:border-r [[align=center]]:text-center [[align=right]]:text-right',
        className
      )}
      {...props}
    />
  ),
  tr: ({ className, ...props }) => (
    <tr
      className={cn(
        'aui-md-tr m-0 border-b p-0 first:border-t [&:last-child>td:first-child]:rounded-bl-lg [&:last-child>td:last-child]:rounded-br-lg',
        className
      )}
      {...props}
    />
  ),
  sup: ({ className, ...props }) => (
    <sup
      className={cn('aui-md-sup [&>a]:text-xs [&>a]:no-underline', className)}
      {...props}
    />
  ),
  pre: ({ className, ...props }) => (
    <pre
      className={cn(
        'aui-md-pre text-foreground bg-muted overflow-x-auto rounded-t-none! rounded-b-lg border border-t-0 p-4',
        className
      )}
      {...props}
    />
  ),
  code: function Code({ className, ...props }) {
    const isCodeBlock = useIsMarkdownCodeBlock()
    return (
      <code
        className={cn(
          !isCodeBlock &&
            'aui-md-inline-code bg-muted rounded border font-semibold',
          className
        )}
        {...props}
      />
    )
  },
  CodeHeader,
})
