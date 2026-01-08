import { useState, useCallback, type ReactNode } from 'react'
import { ToolApprovalContext } from './contexts'

interface PendingApproval {
  toolCallId: string
  toolName: string
  args: unknown
  resolve: (approved: boolean) => void
}

interface ToolApprovalContextType {
  pendingApprovals: Map<string, PendingApproval>
  approvedTools: Set<string>
  requestApproval: (
    toolName: string,
    toolCallId: string,
    args: unknown
  ) => Promise<boolean>
  approveToolCall: (toolCallId: string) => void
  denyToolCall: (toolCallId: string) => void
  isToolApproved: (toolName: string) => boolean
  markToolApproved: (toolName: string) => void
  getPendingApproval: (toolCallId: string) => PendingApproval | undefined
}

export function ToolApprovalProvider({ children }: { children: ReactNode }) {
  const [pendingApprovals, setPendingApprovals] = useState<
    Map<string, PendingApproval>
  >(new Map())
  const [approvedTools, setApprovedTools] = useState<Set<string>>(new Set())

  const requestApproval = useCallback(
    (toolName: string, toolCallId: string, args: unknown): Promise<boolean> => {
      return new Promise((resolve) => {
        const pending: PendingApproval = {
          toolCallId,
          toolName,
          args,
          resolve,
        }

        setPendingApprovals((prev) => {
          const next = new Map(prev)
          next.set(toolCallId, pending)
          return next
        })
      })
    },
    []
  )

  const approveToolCall = useCallback((toolCallId: string) => {
    setPendingApprovals((prev) => {
      const pending = prev.get(toolCallId)
      if (pending) {
        pending.resolve(true)
        const next = new Map(prev)
        next.delete(toolCallId)
        return next
      }
      return prev
    })
  }, [])

  const denyToolCall = useCallback((toolCallId: string) => {
    setPendingApprovals((prev) => {
      const pending = prev.get(toolCallId)
      if (pending) {
        pending.resolve(false)
        const next = new Map(prev)
        next.delete(toolCallId)
        return next
      }
      return prev
    })
  }, [])

  const isToolApproved = useCallback(
    (toolName: string) => {
      return approvedTools.has(toolName)
    },
    [approvedTools]
  )

  const markToolApproved = useCallback((toolName: string) => {
    setApprovedTools((prev) => {
      const next = new Set(prev)
      next.add(toolName)
      return next
    })
  }, [])

  const getPendingApproval = useCallback(
    (toolCallId: string) => {
      return pendingApprovals.get(toolCallId)
    },
    [pendingApprovals]
  )

  return (
    <ToolApprovalContext.Provider
      value={{
        pendingApprovals,
        approvedTools,
        requestApproval,
        approveToolCall,
        denyToolCall,
        isToolApproved,
        markToolApproved,
        getPendingApproval,
      }}
    >
      {children}
    </ToolApprovalContext.Provider>
  )
}

export type { ToolApprovalContextType,  }
