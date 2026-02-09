'use client'

import * as React from 'react'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from './select'

export interface SelectWrapperProps {
  placeholder?: string
  /** Path for form state management (future use) */
  valuePath: string
  options: Array<{ value: string; label: string }>
}

/**
 * Select wrapper that takes options as props and builds the dropdown.
 */
export function SelectWrapper({
  placeholder,
  valuePath,
  options,
}: SelectWrapperProps) {
  return (
    <Select name={valuePath}>
      <SelectTrigger data-value-path={valuePath}>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
