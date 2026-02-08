'use client'

import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import type { FC, ReactNode } from 'react'

export interface BadgeProps {
  /** Direct text content */
  content?: string
  /** Badge style variant */
  variant?: 'default' | 'secondary' | 'success' | 'warning' | 'error'
  /** Child elements (alternative to content prop) */
  children?: ReactNode
  /** Additional class names */
  className?: string
}

const variantClasses: Record<string, string> = {
  default: 'bg-primary text-primary-foreground',
  secondary: 'bg-secondary text-secondary-foreground',
  success: 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100',
  warning: 'bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-100',
  error: 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-100',
}

/**
 * Badge component - Status badges with color variants.
 * Use for labels, tags, and status indicators.
 */
export const Badge: FC<BadgeProps> = ({
  content,
  variant = 'default',
  children,
  className,
}) => {
  const r = useRadius()

  return (
    <span
      className={cn(
        'inline-flex items-center px-2 py-0.5 text-xs font-medium',
        r('sm'),
        variantClasses[variant] ?? variantClasses.default,
        className
      )}
    >
      {content ?? children}
    </span>
  )
}
