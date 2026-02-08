'use client'

import { useDensity } from '@/hooks/useDensity'
import { cn } from '@/lib/utils'
import { isJsonRenderTree, type JsonRenderNode } from '@/lib/generative-ui'
import { AlertCircleIcon } from 'lucide-react'
import { FC, useMemo } from 'react'

// Import all components from the generative-ui plugin ui directory
import {
  ActionButton,
  Badge,
  CardWrapper,
  DataTable,
  Grid,
  List,
  Metric,
  Progress,
  Separator,
  Stack,
  Text,
} from '@/plugins/generative-ui/ui'

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
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const components: Record<string, FC<any>> = {
  Card: CardWrapper,
  Metric,
  Grid,
  Stack,
  Text,
  Badge,
  Table: DataTable,
  List,
  Divider: Separator,
  Separator,
  Progress,
  ActionButton,
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
 * This is used by the generativeUI plugin to render `ui` code blocks.
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
