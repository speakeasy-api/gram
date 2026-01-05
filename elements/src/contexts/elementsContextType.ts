import { createContext } from 'react'
import type { ElementsContextType } from '@/types'

export const ElementsContext = createContext<ElementsContextType | undefined>(
  undefined
)
