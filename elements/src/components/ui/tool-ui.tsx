import * as React from 'react'
import { useState, useEffect } from 'react'
import { cva } from 'class-variance-authority'
import {
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  CopyIcon,
  LoaderIcon,
  XIcon,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { codeToHtml, BundledLanguage } from 'shiki'
import { Button } from './button'
import { Popover, PopoverContent, PopoverTrigger } from './popover'

/* -----------------------------------------------------------------------------
 * Status indicator styles
 * -------------------------------------------------------------------------- */

const statusVariants = cva(
  'flex size-5 items-center justify-center rounded-full',
  {
    variants: {
      status: {
        pending: 'border border-dashed border-muted-foreground/50',
        running: 'text-primary',
        complete: 'text-green-600 dark:text-green-500',
        error: 'text-destructive',
        approval: 'text-amber-500',
      },
    },
    defaultVariants: {
      status: 'pending',
    },
  }
)

/* -----------------------------------------------------------------------------
 * Types
 * -------------------------------------------------------------------------- */

type ToolStatus = 'pending' | 'running' | 'complete' | 'error' | 'approval'

type ContentItem =
  | { type: 'text'; text: string; _meta?: { 'getgram.ai/mime-type'?: string } }
  | { type: 'image'; data: string; _meta?: { 'getgram.ai/mime-type'?: string } }

interface ToolUIProps {
  /** Display name of the tool */
  name: string
  /** Optional icon to display (defaults to first letter of name) */
  icon?: React.ReactNode
  /** Provider/source name (e.g., "Notion", "GitHub") */
  provider?: string
  /** Current status of the tool execution */
  status?: ToolStatus
  /** Request/input data - can be string or object */
  request?: string | Record<string, unknown>
  /** Result/output data - can be string, object, or structured content array */
  result?: string | Record<string, unknown> | { content: ContentItem[] }
  /** Whether the tool card starts expanded */
  defaultExpanded?: boolean
  /** Additional class names */
  className?: string
  /** Approval callbacks */
  onApproveOnce?: () => void
  onApproveForSession?: () => void
  onDeny?: () => void
}

interface ToolUISectionProps {
  /** Section title */
  title: string
  /** Content to display - string or object (will be JSON stringified) */
  content: string | Record<string, unknown> | { content: ContentItem[] }
  /** Whether section starts expanded */
  defaultExpanded?: boolean
  /** Enable syntax highlighting */
  highlightSyntax?: boolean
  /** Language hint for syntax highlighting */
  language?: BundledLanguage
}

/* -----------------------------------------------------------------------------
 * Helper Functions
 * -------------------------------------------------------------------------- */

function getLanguageFromMimeType(
  mimeType: string
): BundledLanguage | undefined {
  switch (mimeType) {
    case 'text/markdown':
      return 'markdown'
    case 'text/html':
      return 'html'
    case 'text/css':
      return 'css'
    case 'application/json':
      return 'json'
    case 'text/javascript':
      return 'javascript'
    case 'text/typescript':
      return 'typescript'
    case 'text/python':
      return 'python'
    default:
      return undefined
  }
}

function formatTextForLanguage(
  text: string,
  language: BundledLanguage | undefined
): string {
  if (language === 'json') {
    try {
      return JSON.stringify(JSON.parse(text), null, 2)
    } catch {
      return text
    }
  }
  return text
}

function isStructuredContent(
  content: unknown
): content is { content: ContentItem[] } {
  return (
    typeof content === 'object' &&
    content !== null &&
    'content' in content &&
    Array.isArray((content as { content: unknown }).content)
  )
}

/* -----------------------------------------------------------------------------
 * Helper Components
 * -------------------------------------------------------------------------- */

function StatusIndicator({ status }: { status: ToolStatus }) {
  return (
    <div className={cn(statusVariants({ status }))}>
      {status === 'pending' && null}
      {status === 'running' && <LoaderIcon className="size-4 animate-spin" />}
      {status === 'complete' && <CheckIcon className="size-4" />}
      {status === 'error' && <XIcon className="size-4" />}
      {status === 'approval' && (
        <LoaderIcon className="text-muted-foreground size-4 animate-spin" />
      )}
    </div>
  )
}

function CopyButton({ content }: { content: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    await navigator.clipboard.writeText(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className="text-muted-foreground hover:bg-accent hover:text-foreground rounded p-1 transition-colors"
      aria-label="Copy to clipboard"
    >
      {copied ? (
        <CheckIcon className="size-4" />
      ) : (
        <CopyIcon className="size-4" />
      )}
    </button>
  )
}

/* -----------------------------------------------------------------------------
 * SyntaxHighlightedCode - Code block with shiki syntax highlighting
 * -------------------------------------------------------------------------- */

/** Max characters to send through shiki — above this we skip highlighting. */
const SHIKI_CHAR_LIMIT = 8_000
/** Max lines shown in the collapsed preview. */
const PREVIEW_LINE_LIMIT = 50

function truncateToLines(text: string, maxLines: number) {
  let pos = 0
  for (let i = 0; i < maxLines; i++) {
    const next = text.indexOf('\n', pos)
    if (next === -1) return { text, truncated: false, totalLines: i + 1 }
    pos = next + 1
  }
  const totalLines = text.split('\n').length
  return { text: text.slice(0, pos), truncated: true, totalLines }
}

function SyntaxHighlightedCode({
  text,
  language,
  className,
}: {
  text: string
  language?: BundledLanguage
  className?: string
}) {
  const [highlightedCode, setHighlightedCode] = useState<string | null>(null)
  const [expanded, setExpanded] = useState(false)

  const preview = React.useMemo(
    () => truncateToLines(text, PREVIEW_LINE_LIMIT),
    [text]
  )
  const displayText = expanded ? text : preview.text
  const canHighlight = displayText.length <= SHIKI_CHAR_LIMIT

  useEffect(() => {
    setHighlightedCode(null)
    if (!language || !canHighlight) return
    let cancelled = false
    codeToHtml(displayText, {
      lang: language,
      theme: 'github-dark-default',
      rootStyle: 'background-color: transparent;',
      transformers: [
        {
          pre(node) {
            node.properties.class =
              'w-full py-3 px-4 max-h-[300px] overflow-y-auto whitespace-pre-wrap text-left text-sm'
          },
        },
      ],
    }).then((html) => {
      if (!cancelled) setHighlightedCode(html)
    })
    return () => {
      cancelled = true
    }
  }, [displayText, language, canHighlight])

  const showMoreButton = preview.truncated && !expanded && (
    <button
      type="button"
      onClick={() => setExpanded(true)}
      className="w-full bg-slate-800/90 px-4 py-2 text-left text-xs text-slate-400 transition-colors hover:text-slate-200"
    >
      Show all {preview.totalLines} lines…
    </button>
  )

  if (!canHighlight || !highlightedCode) {
    return (
      <div className={cn('w-full', className)}>
        <pre className="max-h-[300px] w-full overflow-y-auto bg-slate-800/90 px-4 py-3 text-sm whitespace-pre-wrap text-slate-100">
          {displayText}
        </pre>
        {showMoreButton}
      </div>
    )
  }

  return (
    <div className={cn('w-full', className)}>
      <div
        className="w-full bg-slate-800/90"
        dangerouslySetInnerHTML={{ __html: highlightedCode }}
      />
      {showMoreButton}
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * ImageContent - Display base64 encoded images with checkerboard background
 * -------------------------------------------------------------------------- */

function ImageContent({ data }: { data: string }) {
  const image = `data:image/png;base64,${data}`
  return (
    <div
      className="flex items-center justify-center rounded-lg p-5"
      style={{
        backgroundImage: `linear-gradient(45deg, #ccc 25%, transparent 25%), 
                          linear-gradient(135deg, #ccc 25%, transparent 25%),
                          linear-gradient(45deg, transparent 75%, #ccc 75%),
                          linear-gradient(135deg, transparent 75%, #ccc 75%)`,
        backgroundSize: '25px 25px',
        backgroundPosition: '0 0, 12.5px 0, 12.5px -12.5px, 0px 12.5px',
      }}
    >
      <img src={image} className="max-h-[300px] max-w-full object-contain" />
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * StructuredResultContent - Renders structured content array
 * -------------------------------------------------------------------------- */

function StructuredResultContent({
  content,
}: {
  content: { content: ContentItem[] }
}) {
  return (
    <div className="w-full">
      {content.content.map((item, index) => {
        switch (item.type) {
          case 'text': {
            const language = getLanguageFromMimeType(
              item._meta?.['getgram.ai/mime-type'] ?? 'text/plain'
            )
            const formattedText = formatTextForLanguage(item.text, language)
            return (
              <SyntaxHighlightedCode
                key={index}
                text={formattedText}
                language={language}
              />
            )
          }
          case 'image': {
            return <ImageContent key={index} data={item.data} />
          }
          default:
            return (
              <pre
                key={index}
                className="px-4 py-3 text-sm whitespace-pre-wrap"
              >
                {JSON.stringify(item, null, 2)}
              </pre>
            )
        }
      })}
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * ToolUISection - Expandable section for Request/Result
 * -------------------------------------------------------------------------- */

function ToolUISection({
  title,
  content,
  defaultExpanded = false,
  highlightSyntax = true,
  language = 'json',
}: ToolUISectionProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded)

  // For structured content, we don't stringify it
  const isStructured = isStructuredContent(content)
  const contentString = isStructured
    ? JSON.stringify(content, null, 2)
    : typeof content === 'string'
      ? content
      : JSON.stringify(content, null, 2)

  return (
    <div data-slot="tool-ui-section" className="border-border border-t">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="hover:bg-accent/50 flex w-full cursor-pointer items-center justify-between px-5 py-2.5 text-left transition-colors"
      >
        <span className="text-muted-foreground text-sm">{title}</span>
        <div className="flex items-center gap-1">
          <CopyButton content={contentString} />
          <ChevronRightIcon
            className={cn(
              'text-muted-foreground size-4 transition-transform duration-200',
              isExpanded && 'rotate-90'
            )}
          />
        </div>
      </button>
      {isExpanded && (
        <div className="border-border border-t">
          {isStructured ? (
            <StructuredResultContent content={content} />
          ) : highlightSyntax ? (
            <SyntaxHighlightedCode text={contentString} language={language} />
          ) : (
            <pre className="text-foreground overflow-x-auto px-4 py-3 text-sm whitespace-pre-wrap">
              {contentString}
            </pre>
          )}
        </div>
      )}
    </div>
  )
}

type ApprovalMode = 'one-time' | 'for-session'

/* -----------------------------------------------------------------------------
 * ToolUI - Main component
 * -------------------------------------------------------------------------- */

function ToolUI({
  name,
  icon,
  provider,
  status = 'complete',
  request,
  result,
  defaultExpanded = false,
  className,
  onApproveOnce,
  onApproveForSession,
  onDeny,
}: ToolUIProps) {
  const isApprovalPending =
    status === 'approval' && onApproveOnce !== undefined && onDeny !== undefined
  // Auto-expand when approval is pending, collapse when approved
  const [isExpanded, setIsExpanded] = useState(defaultExpanded)
  const hasContent = request !== undefined || result !== undefined

  // Track approval mode: 'one-time' or 'for-session'
  const [approvalMode, setApprovalMode] = useState<ApprovalMode>('one-time')
  const [isDropdownOpen, setIsDropdownOpen] = useState(false)

  // Collapse when transitioning from approval to non-approval (i.e., when approved/denied)
  useEffect(() => {
    if (!isApprovalPending && isExpanded && !defaultExpanded) {
      setIsExpanded(false)
    }
  }, [isApprovalPending])

  // Handle approve based on selected mode
  const handleApprove = () => {
    if (approvalMode === 'for-session' && onApproveForSession) {
      onApproveForSession()
    } else if (onApproveOnce) {
      onApproveOnce()
    }
  }

  return (
    <div
      data-slot="tool-ui"
      className={cn(
        'border-border bg-card overflow-hidden rounded-lg border',
        className
      )}
    >
      {/* Header with provider */}
      {provider && (
        <div
          data-slot="tool-ui-provider"
          className={cn(
            'border-border flex items-center gap-2 border-b px-4 py-2.5'
          )}
        >
          {icon ? (
            <span className="flex size-5 items-center justify-center">
              {icon}
            </span>
          ) : (
            <span className="bg-muted flex size-5 items-center justify-center rounded text-xs font-medium">
              {provider.charAt(0).toUpperCase()}
            </span>
          )}
          <span className="text-sm font-medium">{provider}</span>
        </div>
      )}

      {/* Tool row */}
      <button
        onClick={() => hasContent && setIsExpanded(!isExpanded)}
        disabled={!hasContent}
        className={cn(
          'flex w-full items-center gap-2 px-4 py-3 text-left',
          hasContent && 'hover:bg-accent/50 cursor-pointer transition-colors'
        )}
      >
        <StatusIndicator status={status} />
        <span
          className={cn(
            'flex-1 text-sm',
            !provider && isApprovalPending && 'shimmer'
          )}
        >
          {name}
        </span>
        {hasContent && (
          <ChevronDownIcon
            className={cn(
              'text-muted-foreground size-4 transition-transform duration-200',
              isExpanded && 'rotate-180'
            )}
          />
        )}
      </button>

      {/* Expandable content */}
      {isExpanded && hasContent && (
        <div data-slot="tool-ui-content">
          {/* When not approval pending, use collapsible section */}
          {request !== undefined && (
            <ToolUISection
              title="Arguments"
              content={request}
              highlightSyntax
              language="json"
            />
          )}
          {/* Hide output when approval is pending */}
          {result !== undefined && (
            <ToolUISection
              title="Output"
              content={result}
              highlightSyntax
              language="json"
            />
          )}
        </div>
      )}

      {/* Approval actions */}
      {isApprovalPending && (
        <div
          data-slot="tool-ui-approval-actions"
          className="border-border flex items-center justify-end gap-2 border-t px-4 py-3"
        >
          <div>
            <span className="text-muted-foreground text-sm">
              This tool requires approval
            </span>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={onDeny}
              className="text-destructive hover:bg-destructive/10 dark:text-rose-400"
            >
              <XIcon className="mr-1 size-3" />
              Deny
            </Button>
            {/* Split button: main approve + dropdown for options */}
            <div className="flex items-center">
              <Button
                variant="default"
                size="sm"
                onClick={handleApprove}
                className="flex cursor-pointer justify-between gap-1 rounded-r-none bg-emerald-600 hover:bg-emerald-700"
              >
                <CheckIcon className="dark:text-foreground mr-1 size-3" />

                {/* The min-width is needed to prevent the button from shifting when the text changes */}
                <span className="dark:text-foreground min-w-[110px]">
                  {approvalMode === 'one-time'
                    ? 'Approve this time'
                    : 'Approve always'}
                </span>
              </Button>
              <Popover open={isDropdownOpen} onOpenChange={setIsDropdownOpen}>
                <PopoverTrigger asChild>
                  <Button
                    variant="default"
                    size="sm"
                    className="cursor-pointer rounded-l-none border-l border-emerald-700 bg-emerald-600 px-2 hover:bg-emerald-700"
                  >
                    <ChevronDownIcon className="dark:text-foreground size-3" />
                  </Button>
                </PopoverTrigger>
                <PopoverContent align="end" className="w-64 p-1" sideOffset={4}>
                  <button
                    onClick={() => {
                      setApprovalMode('one-time')
                      setIsDropdownOpen(false)
                    }}
                    className="hover:bg-accent relative flex w-full items-start gap-2 rounded-sm px-2 py-2 text-left"
                  >
                    <CheckIcon
                      className={cn(
                        'relative top-1 mt-0.5 size-3 shrink-0',
                        approvalMode !== 'one-time' && 'invisible'
                      )}
                    />
                    <div className="flex flex-col gap-0.5">
                      <span className="text-sm">Approve only once</span>
                      <span className="text-muted-foreground text-xs">
                        You'll be asked again next time
                      </span>
                    </div>
                  </button>
                  {onApproveForSession && (
                    <button
                      onClick={() => {
                        setApprovalMode('for-session')
                        setIsDropdownOpen(false)
                      }}
                      className="hover:bg-accent relative flex w-full items-start gap-2 rounded-sm px-2 py-2 text-left"
                    >
                      <CheckIcon
                        className={cn(
                          'relative top-1 mt-0.5 size-3 shrink-0',
                          approvalMode !== 'for-session' && 'invisible'
                        )}
                      />
                      <div className="flex flex-col gap-0.5">
                        <span className="text-sm">Approve always</span>
                        <span className="text-muted-foreground text-xs">
                          Trust this tool for the session
                        </span>
                      </div>
                    </button>
                  )}
                </PopoverContent>
              </Popover>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * ToolUIGroup - Container for multiple tool calls
 * -------------------------------------------------------------------------- */

interface ToolUIGroupProps {
  /** Title for the group header */
  title: string
  /** Optional icon */
  icon?: React.ReactNode
  /** Overall status of the group */
  status?: 'running' | 'complete'
  /** Whether the group starts expanded */
  defaultExpanded?: boolean
  /** Child tool UI components */
  children: React.ReactNode
  /** Additional class names */
  className?: string
}

function ToolUIGroup({
  title,
  icon,
  status = 'complete',
  defaultExpanded = false,
  children,
  className,
}: ToolUIGroupProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded)

  return (
    <div
      data-slot="tool-ui-group"
      className={cn(
        'border-border bg-card overflow-hidden rounded-lg border',
        className
      )}
    >
      {/* Group header */}
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="hover:bg-accent/50 flex w-full items-center gap-2 px-4 py-3 text-left transition-colors"
      >
        {icon || (
          <StatusIndicator
            status={status === 'running' ? 'running' : 'complete'}
          />
        )}
        <span
          className={cn(
            'flex-1 text-sm font-medium',
            status === 'running' && 'shimmer'
          )}
        >
          {title}
        </span>
        <ChevronDownIcon
          className={cn(
            'text-muted-foreground size-4 transition-transform duration-200',
            isExpanded && 'rotate-180'
          )}
        />
      </button>

      {/* Expandable children */}
      {isExpanded && (
        <div
          data-slot="tool-ui-group-content"
          className="border-border border-t"
        >
          {children}
        </div>
      )}
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * Exports
 * -------------------------------------------------------------------------- */

export {
  ToolUI,
  ToolUISection,
  ToolUIGroup,
  SyntaxHighlightedCode,
  StatusIndicator,
  CopyButton,
}
export type {
  ToolUIProps,
  ToolUISectionProps,
  ToolUIGroupProps,
  ToolStatus,
  ContentItem,
}
