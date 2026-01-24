'use client'

import { useDensity } from '@/hooks/useDensity'
import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import { isJsonRenderTree, type JsonRenderNode } from '@/lib/generative-ui'
import { useThreadRuntime } from '@assistant-ui/react'
import { AlertCircleIcon, Loader2Icon } from 'lucide-react'
import { FC, useMemo, useState, useCallback } from 'react'

interface GenerativeUIProps {
  /** The JSON content to render - can be a json-render tree or raw object */
  content: unknown
  /** Additional class names */
  className?: string
}

/**
 * Built-in components for rendering json-render trees.
 * These provide a default set of UI primitives for tool results.
 */
const components: Record<string, FC<Record<string, unknown>>> = {
  Card: ({ title, children, className }) => {
    const r = useRadius()
    const d = useDensity()
    const titleStr = title != null ? String(title) : null
    return (
      <div
        className={cn(
          'border-border bg-card border',
          r('lg'),
          d('p-lg'),
          className as string
        )}
      >
        {titleStr && <h3 className="mb-4 text-lg font-semibold">{titleStr}</h3>}
        {children as React.ReactNode}
      </div>
    )
  },

  Metric: ({ label, value, format, className }) => {
    const formattedValue = useMemo(() => {
      const numValue = Number(value)
      if (isNaN(numValue)) return String(value)

      switch (format) {
        case 'currency':
          return new Intl.NumberFormat('en-US', {
            style: 'currency',
            currency: 'USD',
          }).format(numValue)
        case 'percent':
          return new Intl.NumberFormat('en-US', {
            style: 'percent',
            minimumFractionDigits: 1,
          }).format(numValue)
        case 'number':
        default:
          return new Intl.NumberFormat('en-US').format(numValue)
      }
    }, [value, format])

    return (
      <div className={cn('flex flex-col gap-2', className as string)}>
        <span className="text-muted-foreground text-sm">{String(label)}</span>
        <span className="text-3xl font-bold">{formattedValue}</span>
      </div>
    )
  },

  Grid: ({ columns = 2, children, className }) => {
    const d = useDensity()
    return (
      <div
        className={cn('grid', d('gap-lg'), className as string)}
        style={{
          gridTemplateColumns: `repeat(${columns as number}, minmax(0, 1fr))`,
        }}
      >
        {children as React.ReactNode}
      </div>
    )
  },

  Stack: ({ direction = 'vertical', children, className }) => {
    const d = useDensity()
    return (
      <div
        className={cn(
          'flex',
          direction === 'horizontal' ? 'flex-row' : 'flex-col',
          d('gap-md'),
          className as string
        )}
      >
        {children as React.ReactNode}
      </div>
    )
  },

  Text: ({ children, content, variant = 'body', className }) => {
    const variantClasses: Record<string, string> = {
      heading: 'text-lg font-semibold',
      body: 'text-sm',
      caption: 'text-xs text-muted-foreground',
      code: 'font-mono text-sm bg-muted px-1 rounded',
    }
    // Support both content prop (for direct text) and children (for nested components)
    const textContent = content != null ? String(content) : null
    return (
      <span
        className={cn(variantClasses[variant as string], className as string)}
      >
        {textContent ?? (children as React.ReactNode)}
      </span>
    )
  },

  Badge: ({ children, content, variant = 'default', className }) => {
    const r = useRadius()
    const variantClasses: Record<string, string> = {
      default: 'bg-primary text-primary-foreground',
      secondary: 'bg-secondary text-secondary-foreground',
      success:
        'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100',
      warning:
        'bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-100',
      error: 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-100',
    }
    // Support both content prop (for direct text) and children (for nested components)
    const textContent = content != null ? String(content) : null
    return (
      <span
        className={cn(
          'inline-flex items-center px-2 py-0.5 text-xs font-medium',
          r('sm'),
          variantClasses[variant as string] ?? variantClasses.default,
          className as string
        )}
      >
        {textContent ?? (children as React.ReactNode)}
      </span>
    )
  },

  Table: ({ headers, rows, className }) => {
    const r = useRadius()
    const headerArray = Array.isArray(headers) ? (headers as string[]) : []
    const rowsArray = Array.isArray(rows) ? (rows as unknown[][]) : []
    return (
      <div className={cn('overflow-auto', className as string)}>
        <table className={cn('w-full border-collapse text-sm', r('lg'))}>
          {headerArray.length > 0 && (
            <thead>
              <tr className="border-border border-b">
                {headerArray.map((header, i) => (
                  <th
                    key={i}
                    className="text-muted-foreground px-4 py-3 text-left font-medium"
                  >
                    {header}
                  </th>
                ))}
              </tr>
            </thead>
          )}
          <tbody>
            {rowsArray.map((row, i) => (
              <tr key={i} className="border-border border-b last:border-0">
                {row.map((cell, j) => (
                  <td key={j} className="px-4 py-3">
                    {String(cell)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  },

  List: ({ items, ordered = false, className }) => {
    const Tag = ordered ? 'ol' : 'ul'
    const itemsArray = Array.isArray(items) ? (items as string[]) : []
    return (
      <Tag
        className={cn(
          'list-inside space-y-2 text-sm',
          ordered ? 'list-decimal' : 'list-disc',
          className as string
        )}
      >
        {itemsArray.map((item, i) => (
          <li key={i}>{item}</li>
        ))}
      </Tag>
    )
  },

  Divider: ({ className }) => (
    <hr className={cn('border-border my-4', className as string)} />
  ),

  Progress: ({ value, max = 100, label, className }) => {
    const r = useRadius()
    const numValue = Number(value)
    const numMax = Number(max)
    const percentage =
      isNaN(numValue) || isNaN(numMax) || numMax === 0
        ? 0
        : Math.min(100, Math.max(0, (numValue / numMax) * 100))
    const labelStr = label != null ? String(label) : null
    return (
      <div className={cn('w-full space-y-2', className as string)}>
        {labelStr && (
          <div className="flex justify-between text-sm">
            <span>{labelStr}</span>
            <span className="text-muted-foreground">
              {percentage.toFixed(0)}%
            </span>
          </div>
        )}
        <div className={cn('bg-muted h-3 overflow-hidden', r('sm'))}>
          <div
            className={cn('bg-primary h-full transition-all', r('sm'))}
            style={{ width: `${percentage}%` }}
          />
        </div>
      </div>
    )
  },

  ActionButton: ({ label, action, args, variant = 'default', className }) => {
    const r = useRadius()
    const runtime = useThreadRuntime({ optional: true })
    const [isLoading, setIsLoading] = useState(false)

    const handleClick = useCallback(async () => {
      if (!runtime || !action) return

      setIsLoading(true)
      try {
        // Send a structured message that instructs the LLM to call the tool
        const argsJson = args ? JSON.stringify(args) : '{}'
        runtime.append({
          role: 'user',
          content: [
            {
              type: 'text',
              text: `[Action: ${action}] ${argsJson}`,
            },
          ],
        })
      } finally {
        setIsLoading(false)
      }
    }, [runtime, action, args])

    const variantClasses: Record<string, string> = {
      default: 'bg-primary text-primary-foreground hover:bg-primary/90',
      secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
      outline:
        'border border-input bg-background hover:bg-accent hover:text-accent-foreground',
      destructive:
        'bg-destructive text-destructive-foreground hover:bg-destructive/90',
    }

    return (
      <button
        onClick={handleClick}
        disabled={isLoading || !runtime}
        className={cn(
          'inline-flex items-center justify-center gap-2 px-4 py-2 text-sm font-medium transition-colors',
          'disabled:pointer-events-none disabled:opacity-50',
          r('md'),
          variantClasses[variant as string] ?? variantClasses.default,
          className as string
        )}
      >
        {isLoading && <Loader2Icon className="size-4 animate-spin" />}
        {String(label)}
      </button>
    )
  },
}

/**
 * Recursively render a json-render tree node
 */
function renderNode(node: JsonRenderNode, key?: number): React.ReactNode {
  const Component = components[node.type]

  if (!Component) {
    // Unknown component type - render as debug info
    return (
      <div key={key} className="text-muted-foreground text-xs">
        Unknown component: {node.type}
      </div>
    )
  }

  // Recursively render children (ensure it's an array)
  const children = Array.isArray(node.children)
    ? node.children.map((child, i) => renderNode(child, i))
    : undefined

  return <Component key={key} {...(node.props ?? {})} children={children} />
}

/**
 * GenerativeUI component renders json-render compatible JSON as dynamic UI widgets.
 * This is used when tools.generativeUI.enabled is true in the Elements config.
 */
export const GenerativeUI: FC<GenerativeUIProps> = ({ content, className }) => {
  const d = useDensity()

  // Check if content is a valid json-render tree
  const tree = useMemo(() => {
    if (isJsonRenderTree(content)) {
      return content
    }
    return null
  }, [content])

  if (!tree) {
    return (
      <div
        className={cn(
          'text-muted-foreground flex items-center gap-2 text-sm',
          d('p-md'),
          className
        )}
      >
        <AlertCircleIcon className="size-4" />
        <span>Invalid generative UI structure</span>
      </div>
    )
  }

  return <div className={cn('w-full', className)}>{renderNode(tree)}</div>
}

export type { GenerativeUIProps }
