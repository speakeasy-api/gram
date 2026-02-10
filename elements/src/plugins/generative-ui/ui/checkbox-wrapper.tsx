'use client'

import * as React from 'react'
import { Checkbox } from './checkbox'
import { Label } from './label'

export interface CheckboxWrapperProps {
  label?: string
  /** Path for form state management (future use) */
  valuePath: string
  defaultChecked?: boolean
}

/**
 * Checkbox wrapper that adds label support.
 */
export function CheckboxWrapper({
  label,
  valuePath,
  defaultChecked,
}: CheckboxWrapperProps) {
  const id = React.useId()

  return (
    <div className="flex items-center gap-2">
      <Checkbox
        id={id}
        name={valuePath}
        defaultChecked={defaultChecked}
        data-value-path={valuePath}
      />
      {label && (
        <Label htmlFor={id} className="cursor-pointer">
          {label}
        </Label>
      )}
    </div>
  )
}
