import { useContext } from 'react'
import { ToolApprovalContext } from '@/contexts/ToolApprovalContext'

/**
 * Hook to access the tool approval context for managing human-in-the-loop
 * tool execution approval.
 */
export const useToolApproval = () => {
  const context = useContext(ToolApprovalContext)
  if (!context) {
    throw new Error(
      'useToolApproval must be used within a ToolApprovalProvider'
    )
  }
  return context
}
