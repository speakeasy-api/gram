'use client'

import { cn } from '@/lib/utils'
import type { FC, ReactNode } from 'react'

export interface TextProps {
  /** Direct text content */
  content?: string
  /** Text style variant */
  variant?: 'heading' | 'body' | 'caption' | 'code'
  /** Child elements (alternative to content prop) */
  children?: ReactNode
  /** Additional class names */
  className?: string
}

const variantClasses: Record<string, string> = {
  heading: 'text-lg font-semibold',
  body: 'text-sm',
  caption: 'text-xs text-muted-foreground',
  code: 'font-mono text-sm bg-muted px-1 rounded',
}

/**
 * Text component - Styled text with variants.
 * Supports heading, body, caption, and code styles.
 */
export const Text: FC<TextProps> = ({
  content,
  variant = 'body',
  children,
  className,
}) => {
  return (
    <span className={cn(variantClasses[variant], className)}>
      {content ?? children}
    </span>
  )
}
