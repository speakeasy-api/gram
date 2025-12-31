import { Dispatch, SetStateAction } from 'react'
import { useElements } from './useElements'

interface UseExpandedAPI {
  expandable: boolean
  isExpanded: boolean
  defaultExpanded: boolean
  setIsExpanded: Dispatch<SetStateAction<boolean>>
}

export const useExpanded = (): UseExpandedAPI => {
  const { config, isExpanded, setIsExpanded } = useElements()
  const defaultExpanded = config.modal?.defaultExpanded ?? false
  return {
    expandable: config.modal?.expandable ?? false,
    isExpanded,
    setIsExpanded,
    defaultExpanded,
  }
}
