'use client'

import * as React from 'react'
import { Avatar, AvatarImage, AvatarFallback } from './avatar'

export interface AvatarWrapperProps {
  src?: string
  alt?: string
  fallback: string
}

/**
 * Avatar wrapper that takes src, alt, and fallback as props.
 */
export function AvatarWrapper({ src, alt, fallback }: AvatarWrapperProps) {
  return (
    <Avatar>
      {src && <AvatarImage src={src} alt={alt} />}
      <AvatarFallback>{fallback}</AvatarFallback>
    </Avatar>
  )
}
