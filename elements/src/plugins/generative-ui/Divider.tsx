'use client'

import { cn } from '@/lib/utils'
import type { FC } from 'react'

export interface DividerProps {
  /** Additional class names */
  className?: string
}

/**
 * Divider component - Horizontal line separator.
 * Use to visually separate content sections.
 */
export const Divider: FC<DividerProps> = ({ className }) => {
  return <hr className={cn('border-border my-4', className)} />
}
