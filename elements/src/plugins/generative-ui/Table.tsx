'use client'

import { useRadius } from '@/hooks/useRadius'
import { cn } from '@/lib/utils'
import type { FC } from 'react'

export interface TableProps {
  /** Column headers */
  headers: string[]
  /** Table rows as 2D array */
  rows: unknown[][]
  /** Additional class names */
  className?: string
}

/**
 * Table component - Data table with headers and rows.
 * Use for displaying tabular data.
 */
export const Table: FC<TableProps> = ({ headers, rows, className }) => {
  const r = useRadius()
  const headerArray = Array.isArray(headers) ? headers : []
  const rowsArray = Array.isArray(rows) ? rows : []

  return (
    <div className={cn('overflow-auto', className)}>
      <table className={cn('w-full border-collapse text-sm', r('lg'))}>
        {headerArray.length > 0 && (
          <thead>
            <tr className="border-border border-b">
              {headerArray.map((header, i) => (
                <th
                  key={i}
                  className="text-muted-foreground px-4 py-3 text-left font-medium"
                >
                  {header}
                </th>
              ))}
            </tr>
          </thead>
        )}
        <tbody>
          {rowsArray.map((row, i) => (
            <tr key={i} className="border-border border-b last:border-0">
              {(row as unknown[]).map((cell, j) => (
                <td key={j} className="px-4 py-3">
                  {String(cell)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
