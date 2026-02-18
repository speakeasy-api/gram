import * as React from 'react'
import { cn } from '@/lib/utils'

export interface StackProps extends React.ComponentProps<'div'> {
  direction?: 'horizontal' | 'vertical'
  gap?: 'sm' | 'md' | 'lg'
  align?: 'start' | 'center' | 'end' | 'stretch'
  justify?: 'start' | 'center' | 'end' | 'between' | 'around'
}

const gapClasses = {
  sm: 'gap-2',
  md: 'gap-4',
  lg: 'gap-6',
}

const alignClasses = {
  start: 'items-start',
  center: 'items-center',
  end: 'items-end',
  stretch: 'items-stretch',
}

const justifyClasses = {
  start: 'justify-start',
  center: 'justify-center',
  end: 'justify-end',
  between: 'justify-between',
  around: 'justify-around',
}

export function Stack({
  direction = 'vertical',
  gap = 'md',
  align,
  justify,
  className,
  ...props
}: StackProps) {
  return (
    <div
      data-slot="stack"
      className={cn(
        'flex',
        direction === 'horizontal' ? 'flex-row' : 'flex-col',
        gapClasses[gap],
        align && alignClasses[align],
        justify && justifyClasses[justify],
        className
      )}
      {...props}
    />
  )
}
