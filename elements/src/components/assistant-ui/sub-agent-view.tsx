import * as React from 'react'
import { cva } from 'class-variance-authority'
import {
  CheckIcon,
  ChevronDownIcon,
  LoaderIcon,
  XIcon,
  BotIcon,
  WrenchIcon,
} from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { cn } from '@/lib/utils'
import type { SubAgentState, SubAgentStatus, SubAgentMessage } from '@/types'
import { useSubAgent, useSubAgentOptional } from '@/contexts/SubAgentContext'

/* -----------------------------------------------------------------------------
 * Status indicator styles
 * -------------------------------------------------------------------------- */

const agentStatusVariants = cva(
  'flex size-5 items-center justify-center rounded-full',
  {
    variants: {
      status: {
        pending: 'border border-dashed border-muted-foreground/50',
        running: 'text-primary',
        completed: 'text-green-600 dark:text-green-500',
        failed: 'text-destructive',
      },
    },
    defaultVariants: {
      status: 'pending',
    },
  }
)

/* -----------------------------------------------------------------------------
 * Helper Components
 * -------------------------------------------------------------------------- */

function AgentStatusIndicator({ status }: { status: SubAgentStatus }) {
  return (
    <div className={cn(agentStatusVariants({ status }))}>
      {status === 'pending' && null}
      {status === 'running' && <LoaderIcon className="size-4 animate-spin" />}
      {status === 'completed' && <CheckIcon className="size-4" />}
      {status === 'failed' && <XIcon className="size-4" />}
    </div>
  )
}

function AgentIcon() {
  return (
    <div className="bg-primary/10 text-primary flex size-6 items-center justify-center rounded">
      <BotIcon className="size-4" />
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * Simple Markdown renderer for sub-agent content
 * Uses the same styles as the main MarkdownText component
 * -------------------------------------------------------------------------- */

interface SimpleMarkdownProps {
  content: string
  className?: string
}

function SimpleMarkdown({ content, className }: SimpleMarkdownProps) {
  return (
    <div className={cn('aui-md text-sm', className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
        h1: ({ className: cls, ...props }) => (
          <h1
            className={cn(
              'aui-md-h1 mb-4 scroll-m-20 text-2xl font-extrabold tracking-tight last:mb-0',
              cls
            )}
            {...props}
          />
        ),
        h2: ({ className: cls, ...props }) => (
          <h2
            className={cn(
              'aui-md-h2 mt-4 mb-2 scroll-m-20 text-xl font-semibold tracking-tight first:mt-0 last:mb-0',
              cls
            )}
            {...props}
          />
        ),
        h3: ({ className: cls, ...props }) => (
          <h3
            className={cn(
              'aui-md-h3 mt-3 mb-2 scroll-m-20 text-lg font-semibold tracking-tight first:mt-0 last:mb-0',
              cls
            )}
            {...props}
          />
        ),
        p: ({ className: cls, ...props }) => (
          <p
            className={cn(
              'aui-md-p mt-2 mb-2 leading-6 first:mt-0 last:mb-0',
              cls
            )}
            {...props}
          />
        ),
        a: ({ className: cls, ...props }) => (
          <a
            className={cn(
              'aui-md-a text-primary font-medium underline underline-offset-4',
              cls
            )}
            {...props}
          />
        ),
        ul: ({ className: cls, ...props }) => (
          <ul
            className={cn(
              'aui-md-ul my-2 ml-4 list-disc [&>li]:mt-1',
              cls
            )}
            {...props}
          />
        ),
        ol: ({ className: cls, ...props }) => (
          <ol
            className={cn(
              'aui-md-ol my-2 ml-4 list-decimal [&>li]:mt-1',
              cls
            )}
            {...props}
          />
        ),
        blockquote: ({ className: cls, ...props }) => (
          <blockquote
            className={cn('aui-md-blockquote border-l-2 pl-4 italic', cls)}
            {...props}
          />
        ),
        code: ({ className: cls, ...props }) => (
          <code
            className={cn(
              'aui-md-inline-code bg-muted rounded border px-1 py-0.5 font-mono text-xs',
              cls
            )}
            {...props}
          />
        ),
        pre: ({ className: cls, ...props }) => (
          <pre
            className={cn(
              'aui-md-pre bg-muted overflow-x-auto rounded-lg border p-3 text-xs',
              cls
            )}
            {...props}
          />
        ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * SubAgentMessage - Renders individual messages within an agent's thread
 * -------------------------------------------------------------------------- */

interface SubAgentMessageProps {
  message: SubAgentMessage
}

function SubAgentMessageView({ message }: SubAgentMessageProps) {
  if (message.role === 'tool') {
    return (
      <div className="border-border flex items-start gap-2 border-b px-4 py-2.5 last:border-b-0">
        <div className="bg-muted flex size-5 shrink-0 items-center justify-center rounded">
          <WrenchIcon className="text-muted-foreground size-3" />
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          {message.toolName && (
            <span className="text-muted-foreground text-xs font-medium">
              {message.toolName}
            </span>
          )}
          <p className="text-foreground text-sm break-words">
            {message.content}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="border-border border-b px-4 py-2.5 last:border-b-0">
      <SimpleMarkdown content={message.content} />
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * SubAgentHeader - Collapsed view header
 * -------------------------------------------------------------------------- */

interface SubAgentHeaderProps {
  agent: SubAgentState
  isExpanded: boolean
  onToggle: () => void
  depth?: number
}

function SubAgentHeader({
  agent,
  isExpanded,
  onToggle,
  depth = 0,
}: SubAgentHeaderProps) {
  const statusText =
    agent.status === 'running'
      ? 'Running...'
      : agent.status === 'completed'
        ? 'Completed'
        : agent.status === 'failed'
          ? 'Failed'
          : 'Pending'

  return (
    <button
      onClick={onToggle}
      className={cn(
        'hover:bg-accent/50 flex w-full items-center gap-2 px-4 py-3 text-left transition-colors',
        depth > 0 && 'bg-muted/30'
      )}
      style={{ paddingLeft: `${16 + depth * 12}px` }}
    >
      <AgentIcon />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex items-center gap-2">
          <span
            className={cn(
              'text-sm font-medium',
              agent.status === 'running' && 'shimmer'
            )}
          >
            {agent.name}
          </span>
          <span className="text-muted-foreground text-xs">{statusText}</span>
        </div>
        <span className="text-muted-foreground truncate text-xs">
          {agent.task}
        </span>
      </div>
      <AgentStatusIndicator status={agent.status} />
      <ChevronDownIcon
        className={cn(
          'text-muted-foreground size-4 shrink-0 transition-transform duration-200',
          isExpanded && 'rotate-180'
        )}
      />
    </button>
  )
}

/* -----------------------------------------------------------------------------
 * SubAgentContent - Expanded view showing messages
 * -------------------------------------------------------------------------- */

interface SubAgentContentProps {
  agent: SubAgentState
  depth?: number
}

function SubAgentContent({ agent, depth = 0 }: SubAgentContentProps) {
  const { getChildAgents } = useSubAgent()
  const childAgents = getChildAgents(agent.id)

  return (
    <div
      className="border-border border-t"
      style={{ marginLeft: `${depth * 12}px` }}
    >
      {/* Messages */}
      {agent.messages.length > 0 && (
        <div className="bg-muted/20">
          {agent.messages.map((msg) => (
            <SubAgentMessageView key={msg.id} message={msg} />
          ))}
        </div>
      )}

      {/* Result or Error */}
      {agent.status === 'completed' && agent.result && (
        <div className="border-border border-t bg-green-50 px-4 py-2.5 dark:bg-green-950/20">
          <span className="text-xs font-medium text-green-700 dark:text-green-400">
            Result:
          </span>
          <div className="text-foreground mt-1">
            <SimpleMarkdown content={agent.result} />
          </div>
        </div>
      )}

      {agent.status === 'failed' && agent.error && (
        <div className="border-border bg-destructive/10 border-t px-4 py-2.5">
          <span className="text-destructive text-xs font-medium">Error:</span>
          <p className="text-destructive mt-1 text-sm">{agent.error}</p>
        </div>
      )}

      {/* Nested child agents */}
      {childAgents.length > 0 && (
        <div className="border-border border-t">
          {childAgents.map((child) => (
            <SubAgentView key={child.id} agentId={child.id} depth={depth + 1} />
          ))}
        </div>
      )}
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * SubAgentView - Main component
 * -------------------------------------------------------------------------- */

interface SubAgentViewProps {
  /** The agent ID to render */
  agentId: string
  /** Nesting depth for indentation */
  depth?: number
  /** Additional class names */
  className?: string
}

export function SubAgentView({
  agentId,
  depth = 0,
  className,
}: SubAgentViewProps) {
  const { getAgent, isAgentExpanded, toggleAgentExpanded } = useSubAgent()
  const agent = getAgent(agentId)

  if (!agent) {
    return null
  }

  const isExpanded = isAgentExpanded(agentId)

  return (
    <div
      data-slot="sub-agent-view"
      className={cn(
        'border-border overflow-hidden',
        depth === 0 && 'bg-card rounded-lg border',
        className
      )}
    >
      <SubAgentHeader
        agent={agent}
        isExpanded={isExpanded}
        onToggle={() => toggleAgentExpanded(agentId)}
        depth={depth}
      />
      {isExpanded && <SubAgentContent agent={agent} depth={depth} />}
    </div>
  )
}

/* -----------------------------------------------------------------------------
 * SubAgentToolView - Component to render spawn_agent tool calls
 * -------------------------------------------------------------------------- */

interface SubAgentToolViewProps {
  /** The agent ID associated with this tool call */
  agentId: string
  /** Additional class names */
  className?: string
}

export function SubAgentToolView({
  agentId,
  className,
}: SubAgentToolViewProps) {
  const { getAgent } = useSubAgent()
  const agent = getAgent(agentId)

  // Show a loading placeholder while waiting for the agent spawn event
  // This ensures the component stays mounted and will re-render when agent appears
  if (!agent) {
    return (
      <div
        data-slot="sub-agent-view"
        className={cn(
          'bg-card border-border overflow-hidden rounded-lg border',
          className
        )}
      >
        <div className="flex items-center gap-2 px-4 py-3">
          <div className="bg-primary/10 text-primary flex size-6 items-center justify-center rounded">
            <BotIcon className="size-4" />
          </div>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="shimmer text-sm font-medium">Starting agent...</span>
          </div>
          <LoaderIcon className="text-primary size-4 animate-spin" />
        </div>
      </div>
    )
  }

  return <SubAgentView agentId={agentId} depth={0} className={className} />
}

/* -----------------------------------------------------------------------------
 * SubAgentList - Renders all root-level agents from context
 * -------------------------------------------------------------------------- */

interface SubAgentListProps {
  /** Additional class names */
  className?: string
}

function SubAgentListInner({ className }: SubAgentListProps) {
  const { getRootAgents } = useSubAgent()
  const rootAgents = getRootAgents()

  if (rootAgents.length === 0) {
    return null
  }

  return (
    <div className={cn('space-y-2 my-4', className)}>
      {rootAgents.map((agent) => (
        <SubAgentView key={agent.id} agentId={agent.id} depth={0} />
      ))}
    </div>
  )
}

/**
 * SubAgentList - Renders all root-level agents from context.
 * Safe to use outside SubAgentProvider (returns null).
 */
export function SubAgentList({ className }: SubAgentListProps) {
  const context = useSubAgentOptional()

  if (!context) {
    return null
  }

  return <SubAgentListInner className={className} />
}

/* -----------------------------------------------------------------------------
 * Exports
 * -------------------------------------------------------------------------- */

export {
  SubAgentHeader,
  SubAgentContent,
  SubAgentMessageView,
  AgentStatusIndicator,
  AgentIcon,
}
export type { SubAgentViewProps, SubAgentToolViewProps, SubAgentHeaderProps, SubAgentListProps }
