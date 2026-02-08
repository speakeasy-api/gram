import * as React from 'react'
import { cn } from '@/lib/utils'

export interface TextProps extends React.ComponentProps<'span'> {
  content?: string
  /** Matches LLM prompt variants: heading, body, caption, code */
  variant?: 'heading' | 'body' | 'caption' | 'code'
}

const variantClasses = {
  heading: 'text-lg font-semibold',
  body: 'text-sm',
  caption: 'text-xs text-muted-foreground',
  code: 'font-mono text-sm bg-muted px-1 rounded',
}

export function Text({
  content,
  variant = 'body',
  className,
  children,
  ...props
}: TextProps) {
  return (
    <span
      data-slot="text"
      className={cn(variantClasses[variant], className)}
      {...props}
    >
      {content ?? children}
    </span>
  )
}
