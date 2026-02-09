'use client'

import * as React from 'react'
import {
  Accordion as AccordionPrimitive,
  AccordionItem as AccordionItemPrimitive,
  AccordionTrigger,
  AccordionContent,
} from './accordion'

export interface AccordionWrapperProps {
  type?: 'single' | 'multiple'
  children?: React.ReactNode
}

/**
 * Accordion wrapper that adapts the compound Accordion to the catalog's props-based API.
 */
export function AccordionWrapper({
  type = 'single',
  children,
}: AccordionWrapperProps) {
  // Type assertion needed because Radix types are complex
  const AccordionRoot = AccordionPrimitive as React.FC<{
    type: 'single' | 'multiple'
    collapsible?: boolean
    children?: React.ReactNode
  }>

  return (
    <AccordionRoot type={type} collapsible={type === 'single'}>
      {children}
    </AccordionRoot>
  )
}

export interface AccordionItemWrapperProps {
  value: string
  title: string
  children?: React.ReactNode
}

/**
 * AccordionItem wrapper that takes title as a prop and renders trigger + content.
 */
export function AccordionItemWrapper({
  value,
  title,
  children,
}: AccordionItemWrapperProps) {
  return (
    <AccordionItemPrimitive value={value}>
      <AccordionTrigger>{title}</AccordionTrigger>
      <AccordionContent>{children}</AccordionContent>
    </AccordionItemPrimitive>
  )
}
