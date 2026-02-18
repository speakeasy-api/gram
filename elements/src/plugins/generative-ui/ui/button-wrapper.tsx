'use client'

import * as React from 'react'
import { Button } from './button'

export interface ButtonWrapperProps {
  label: string
  variant?: 'default' | 'secondary' | 'destructive' | 'outline' | 'ghost'
  size?: 'default' | 'sm' | 'lg' | 'icon'
  disabled?: boolean
  /** Backend action to trigger (future use) */
  action?: string
  /** Parameters for the action (future use) */
  actionParams?: Record<string, unknown>
}

/**
 * Button wrapper that takes label as a prop.
 * The action/actionParams props are for future backend integration.
 */
export function ButtonWrapper({
  label,
  variant = 'default',
  size = 'default',
  disabled = false,
}: ButtonWrapperProps) {
  return (
    <Button variant={variant} size={size} disabled={disabled}>
      {label}
    </Button>
  )
}
