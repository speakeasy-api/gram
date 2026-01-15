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

const statusVariants = cva('gramel:flex gramel:size-5 gramel:items-center gramel:justify-center gramel:rounded-full',
  {
    variants: {
      status: {
        pending: 'gramel:border gramel:border-dashed gramel:border-muted-foreground/50',
        running: 'gramel:text-primary',
        complete: 'gramel:text-green-600 gramel:dark:text-green-500',
        error: 'gramel:text-destructive',
        approval: 'gramel:text-amber-500',
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
      {status === 'running' && <LoaderIcon className="gramel:size-4 gramel:animate-spin" />}
      {status === 'complete' && <CheckIcon className="gramel:size-4" />}
      {status === 'error' && <XIcon className="gramel:size-4" />}
      {status === 'approval' && (
        <LoaderIcon className="gramel:text-muted-foreground gramel:size-4 gramel:animate-spin" />
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
      className="gramel:text-muted-foreground gramel:hover:bg-accent gramel:hover:text-foreground gramel:rounded gramel:p-1 gramel:transition-colors"
      aria-label="Copy to clipboard"
    >
      {copied ? (
        <CheckIcon className="gramel:size-4" />
      ) : (
        <CopyIcon className="gramel:size-4" />
      )}
    </button>
  )
}

/* -----------------------------------------------------------------------------
 * SyntaxHighlightedCode - Code block with shiki syntax highlighting
 * -------------------------------------------------------------------------- */

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

  useEffect(() => {
    if (!language) return
    codeToHtml(text, {
      lang: language,
      theme: 'github-dark-default',
      rootStyle: 'background-color: transparent;',
      transformers: [
        {
          pre(node) {
            node.properties.class =
              'gramel:w-full gramel:py-3 gramel:px-4 gramel:max-h-[300px] gramel:overflow-y-auto gramel:whitespace-pre-wrap gramel:text-left gramel:text-sm'
          },
        },
      ],
    }).then(setHighlightedCode)
  }, [text, language])

  if (!highlightedCode) {
    return (
      <pre
        className={cn('gramel:w-full gramel:bg-slate-800/90 gramel:px-4 gramel:py-3 gramel:text-sm gramel:whitespace-pre-wrap gramel:text-slate-100',
          className
        )}
      >
        {text}
      </pre>
    )
  }

  return (
    <div
      className={cn('gramel:w-full gramel:bg-slate-800/90', className)}
      dangerouslySetInnerHTML={{ __html: highlightedCode }}
    />
  )
}

/* -----------------------------------------------------------------------------
 * ImageContent - Display base64 encoded images with checkerboard background
 * -------------------------------------------------------------------------- */

function ImageContent({ data }: { data: string }) {
  const image = `data:image/png;base64,${data}`
  return (
    <div
      className="gramel:flex gramel:items-center gramel:justify-center gramel:rounded-lg gramel:p-5"
      style={{
        backgroundImage: `linear-gradient(45deg, #ccc 25%, transparent 25%), 
                          linear-gradient(135deg, #ccc 25%, transparent 25%),
                          linear-gradient(45deg, transparent 75%, #ccc 75%),
                          linear-gradient(135deg, transparent 75%, #ccc 75%)`,
        backgroundSize: '25px 25px',
        backgroundPosition: '0 0, 12.5px 0, 12.5px -12.5px, 0px 12.5px',
      }}
    >
      <img src={image} className="gramel:max-h-[300px] gramel:max-w-full gramel:object-contain" />
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
    <div className="gramel:w-full">
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
                className="gramel:px-4 gramel:py-3 gramel:text-sm gramel:whitespace-pre-wrap"
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
    <div data-slot="tool-ui-section" className="gramel:border-border gramel:border-t">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="gramel:hover:bg-accent/50 gramel:flex gramel:w-full gramel:cursor-pointer gramel:items-center gramel:justify-between gramel:px-4 gramel:py-2.5 gramel:text-left gramel:transition-colors"
      >
        <span className="gramel:text-muted-foreground gramel:text-sm">{title}</span>
        <div className="gramel:flex gramel:items-center gramel:gap-1">
          <CopyButton content={contentString} />
          <ChevronRightIcon
            className={cn('gramel:text-muted-foreground gramel:size-4 gramel:transition-transform gramel:duration-200',
              isExpanded && 'gramel:rotate-90'
            )}
          />
        </div>
      </button>
      {isExpanded && (
        <div className="gramel:border-border gramel:border-t">
          {isStructured ? (
            <StructuredResultContent content={content} />
          ) : highlightSyntax ? (
            <SyntaxHighlightedCode text={contentString} language={language} />
          ) : (
            <pre className="gramel:text-foreground gramel:overflow-x-auto gramel:px-4 gramel:py-3 gramel:text-sm gramel:whitespace-pre-wrap">
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
      className={cn('gramel:border-border gramel:bg-card gramel:overflow-hidden gramel:rounded-lg gramel:border',
        className
      )}
    >
      {/* Header with provider */}
      {provider && (
        <div
          data-slot="tool-ui-provider"
          className={cn('gramel:border-border gramel:flex gramel:items-center gramel:gap-2 gramel:border-b gramel:px-4 gramel:py-2.5'
          )}
        >
          {icon ? (
            <span className="gramel:flex gramel:size-5 gramel:items-center gramel:justify-center">
              {icon}
            </span>
          ) : (
            <span className="gramel:bg-muted gramel:flex gramel:size-5 gramel:items-center gramel:justify-center gramel:rounded gramel:text-xs gramel:font-medium">
              {provider.charAt(0).toUpperCase()}
            </span>
          )}
          <span className="gramel:text-sm gramel:font-medium">{provider}</span>
        </div>
      )}

      {/* Tool row */}
      <button
        onClick={() => hasContent && setIsExpanded(!isExpanded)}
        disabled={!hasContent}
        className={cn('gramel:flex gramel:w-full gramel:items-center gramel:gap-2 gramel:px-4 gramel:py-3 gramel:text-left',
          hasContent && 'gramel:hover:bg-accent/50 gramel:cursor-pointer gramel:transition-colors'
        )}
      >
        <StatusIndicator status={status} />
        <span
          className={cn('gramel:flex-1 gramel:text-sm',
            !provider && isApprovalPending && 'gramel:shimmer'
          )}
        >
          {name}
        </span>
        {hasContent && (
          <ChevronDownIcon
            className={cn('gramel:text-muted-foreground gramel:size-4 gramel:transition-transform gramel:duration-200',
              isExpanded && 'gramel:rotate-180'
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
          className="gramel:border-border gramel:flex gramel:items-center gramel:justify-end gramel:gap-2 gramel:border-t gramel:px-4 gramel:py-3"
        >
          <div>
            <span className="gramel:text-muted-foreground gramel:text-sm">
              This tool requires approval
            </span>
          </div>
          <div className="gramel:ml-auto gramel:flex gramel:items-center gramel:gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={onDeny}
              className="gramel:text-destructive gramel:hover:bg-destructive/10"
            >
              <XIcon className="gramel:mr-1 gramel:size-3" />
              Deny
            </Button>
            {/* Split button: main approve + dropdown for options */}
            <div className="gramel:flex gramel:items-center">
              <Button
                variant="default"
                size="sm"
                onClick={handleApprove}
                className="gramel:flex gramel:cursor-pointer gramel:justify-between gramel:gap-1 gramel:rounded-r-none gramel:bg-emerald-600 gramel:hover:bg-emerald-700"
              >
                <CheckIcon className="gramel:mr-1 gramel:size-3" />

                {/* The gramel:min-width is needed to prevent the button from shifting when the text changes */}
                <span className="gramel:min-w-[110px]">
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
                    className="gramel:cursor-pointer gramel:rounded-l-none gramel:border-l gramel:border-emerald-700 gramel:bg-emerald-600 gramel:px-2 gramel:hover:bg-emerald-700"
                  >
                    <ChevronDownIcon className="gramel:size-3" />
                  </Button>
                </PopoverTrigger>
                <PopoverContent align="end" className="gramel:w-64 gramel:p-1" sideOffset={4}>
                  <button
                    onClick={() => {
                      setApprovalMode('one-time')
                      setIsDropdownOpen(false)
                    }}
                    className="gramel:hover:bg-accent gramel:relative gramel:flex gramel:w-full gramel:items-start gramel:gap-2 gramel:rounded-sm gramel:px-2 gramel:py-2 gramel:text-left"
                  >
                    <CheckIcon
                      className={cn('gramel:relative gramel:top-1 gramel:mt-0.5 gramel:size-3 gramel:shrink-0',
                        approvalMode !== 'one-time' && 'gramel:invisible'
                      )}
                    />
                    <div className="gramel:flex gramel:flex-col gramel:gap-0.5">
                      <span className="gramel:text-sm">Approve only once</span>
                      <span className="gramel:text-muted-foreground gramel:text-xs">
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
                      className="gramel:hover:bg-accent gramel:relative gramel:flex gramel:w-full gramel:items-start gramel:gap-2 gramel:rounded-sm gramel:px-2 gramel:py-2 gramel:text-left"
                    >
                      <CheckIcon
                        className={cn('gramel:relative gramel:top-1 gramel:mt-0.5 gramel:size-3 gramel:shrink-0',
                          approvalMode !== 'for-session' && 'gramel:invisible'
                        )}
                      />
                      <div className="gramel:flex gramel:flex-col gramel:gap-0.5">
                        <span className="gramel:text-sm">Approve always</span>
                        <span className="gramel:text-muted-foreground gramel:text-xs">
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
      className={cn('gramel:border-border gramel:bg-card gramel:overflow-hidden gramel:rounded-lg gramel:border',
        className
      )}
    >
      {/* Group header */}
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="gramel:hover:bg-accent/50 gramel:flex gramel:w-full gramel:items-center gramel:gap-2 gramel:px-4 gramel:py-3 gramel:text-left gramel:transition-colors"
      >
        {icon || (
          <StatusIndicator
            status={status === 'running' ? 'running' : 'complete'}
          />
        )}
        <span
          className={cn('gramel:flex-1 gramel:text-sm gramel:font-medium',
            status === 'running' && 'gramel:shimmer'
          )}
        >
          {title}
        </span>
        <ChevronDownIcon
          className={cn('gramel:text-muted-foreground gramel:size-4 gramel:transition-transform gramel:duration-200',
            isExpanded && 'gramel:rotate-180'
          )}
        />
      </button>

      {/* Expandable children */}
      {isExpanded && (
        <div
          data-slot="tool-ui-group-content"
          className="gramel:border-border gramel:border-t"
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
