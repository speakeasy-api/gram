'use client'

import * as React from 'react'
import { Skeleton } from './skeleton'
import { cn } from '@/lib/utils'

export interface SkeletonWrapperProps {
  width?: string
  height?: string
  className?: string
}

/**
 * Skeleton wrapper that takes width and height as props.
 */
export function SkeletonWrapper({
  width,
  height,
  className,
}: SkeletonWrapperProps) {
  return (
    <Skeleton
      className={cn(className)}
      style={{
        width: width ?? '100%',
        height: height ?? '1rem',
      }}
    />
  )
}
