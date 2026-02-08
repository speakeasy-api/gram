import * as React from 'react'
import { cn } from '@/lib/utils'

export interface ListProps {
  items: string[]
  ordered?: boolean
  className?: string
}

export function List({ items, ordered = false, className }: ListProps) {
  const listClasses = cn(
    'space-y-1 pl-4',
    ordered ? 'list-decimal' : 'list-disc',
    className
  )

  if (ordered) {
    return (
      <ol data-slot="list" className={listClasses}>
        {items.map((item, index) => (
          <li key={index}>{item}</li>
        ))}
      </ol>
    )
  }

  return (
    <ul data-slot="list" className={listClasses}>
      {items.map((item, index) => (
        <li key={index}>{item}</li>
      ))}
    </ul>
  )
}
