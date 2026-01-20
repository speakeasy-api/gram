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

  // Use sidecar config for sidecar variant, modal config for widget variant
  const expandableConfig =
    config.variant === 'sidecar' ? config.sidecar : config.modal

  const expandable = expandableConfig?.expandable ?? false
  const defaultExpanded = expandableConfig?.defaultExpanded ?? false

  return {
    expandable,
    isExpanded,
    setIsExpanded,
    defaultExpanded,
  }
}
