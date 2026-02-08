'use client'

import { cn } from '@/lib/utils'
import type { FC } from 'react'

export interface ListProps {
  /** List items as strings */
  items: string[]
  /** Whether to use ordered (numbered) list */
  ordered?: boolean
  /** Additional class names */
  className?: string
}

/**
 * List component - Bullet or numbered list.
 * Use for displaying lists of items.
 */
export const List: FC<ListProps> = ({ items, ordered = false, className }) => {
  const Tag = ordered ? 'ol' : 'ul'
  const itemsArray = Array.isArray(items) ? items : []

  return (
    <Tag
      className={cn(
        'list-inside space-y-2 text-sm',
        ordered ? 'list-decimal' : 'list-disc',
        className
      )}
    >
      {itemsArray.map((item, i) => (
        <li key={i}>{item}</li>
      ))}
    </Tag>
  )
}
