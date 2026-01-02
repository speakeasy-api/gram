import { useContext } from 'react'
import { ElementsContext } from '@/contexts/elementsContextType'

/**
 * @private Internal hook to access the ElementsContext
 *
 */
export const useElements = () => {
  const context = useContext(ElementsContext)
  if (!context) {
    throw new Error('useElements must be used within a ElementsProvider')
  }
  return context
}
