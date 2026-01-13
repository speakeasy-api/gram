import { createContext } from 'react'
import type { ElementsContextType } from '@/types'
import { ToolApprovalContextType } from './ToolApprovalContext'

export const ElementsContext = createContext<ElementsContextType | undefined>(
  undefined
)

export const ToolApprovalContext =
  createContext<ToolApprovalContextType | null>(null)
