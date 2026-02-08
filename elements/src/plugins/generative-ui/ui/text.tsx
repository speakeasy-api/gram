import * as React from 'react'
import { cn } from '@/lib/utils'

export interface TextProps extends React.ComponentProps<'span'> {
  content?: string
  variant?: 'default' | 'muted' | 'small' | 'large'
}

const variantClasses = {
  default: '',
  muted: 'text-muted-foreground',
  small: 'text-sm text-muted-foreground',
  large: 'text-lg font-medium',
}

export function Text({
  content,
  variant = 'default',
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
