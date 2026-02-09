'use client'

import * as React from 'react'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from './table'
import { cn } from '@/lib/utils'

export interface DataTableProps extends React.ComponentProps<'div'> {
  headers?: string[]
  rows: (string | number)[][]
}

/**
 * DataTable adapts the compound Table component to a simple props-based API
 * for use with the json-render catalog.
 */
export function DataTable({
  headers,
  rows,
  className,
  ...props
}: DataTableProps) {
  return (
    <div className={cn(className)} {...props}>
      <Table>
        {headers && headers.length > 0 && (
          <TableHeader>
            <TableRow>
              {headers.map((header, i) => (
                <TableHead key={i}>{header}</TableHead>
              ))}
            </TableRow>
          </TableHeader>
        )}
        <TableBody>
          {rows.map((row, rowIndex) => (
            <TableRow key={rowIndex}>
              {row.map((cell, cellIndex) => (
                <TableCell key={cellIndex}>{cell}</TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
