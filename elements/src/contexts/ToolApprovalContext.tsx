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
  /** Whitelist a tool name so all future calls are auto-approved */
  whitelistTool: (toolName: string) => void
  /** Confirm a specific pending tool call approval */
  confirmPendingApproval: (toolCallId: string) => void
  /** Reject a specific pending tool call approval */
  rejectPendingApproval: (toolCallId: string) => void
  isToolApproved: (toolName: string) => boolean
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
  const whitelistTool = useCallback((toolName: string) => {
    setApprovedTools((prev) => {
      const next = new Set(prev)
      next.add(toolName)
      return next
    })
  }, [])

  const confirmPendingApproval = useCallback((toolCallId: string) => {
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

  const rejectPendingApproval = useCallback((toolCallId: string) => {
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
        whitelistTool,
        confirmPendingApproval,
        rejectPendingApproval,
        isToolApproved,
        getPendingApproval,
      }}
    >
      {children}
    </ToolApprovalContext.Provider>
  )
}

export type { ToolApprovalContextType }
