'use client'

import * as React from 'react'
import { Input } from './input'
import { Label } from './label'

export interface InputWrapperProps {
  label?: string
  placeholder?: string
  type?: 'text' | 'email' | 'password' | 'number' | 'tel'
  /** Path for form state management (future use) */
  valuePath: string
}

/**
 * Input wrapper that adds label support.
 */
export function InputWrapper({
  label,
  placeholder,
  type = 'text',
  valuePath,
}: InputWrapperProps) {
  const id = React.useId()

  return (
    <div className="flex flex-col gap-1.5">
      {label && <Label htmlFor={id}>{label}</Label>}
      <Input
        id={id}
        type={type}
        placeholder={placeholder}
        name={valuePath}
        data-value-path={valuePath}
      />
    </div>
  )
}
