'use client'

import * as React from 'react'
import { Alert, AlertTitle, AlertDescription } from './alert'

export interface AlertWrapperProps {
  title: string
  description?: string
  variant?: 'default' | 'destructive'
}

/**
 * Alert wrapper that takes title and description as props.
 */
export function AlertWrapper({
  title,
  description,
  variant = 'default',
}: AlertWrapperProps) {
  return (
    <Alert variant={variant}>
      <AlertTitle>{title}</AlertTitle>
      {description && <AlertDescription>{description}</AlertDescription>}
    </Alert>
  )
}
