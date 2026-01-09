import { AssistantTool } from '@assistant-ui/react'
import {
  ApprovalHelpers,
  FrontendTool,
  wrapFrontendToolWithApproval,
} from '@/lib/tools'
import { useMemo } from 'react'

export function FrontendTools({
  tools: frontendTools,
  toolsRequiringApproval,
  approvalHelpers,
}: {
  tools: Record<string, AssistantTool>
  toolsRequiringApproval?: string[]
  approvalHelpers?: ApprovalHelpers
}) {
  const approvalSet = useMemo(
    () => new Set(toolsRequiringApproval ?? []),
    [toolsRequiringApproval]
  )

  const wrappedTools = useMemo(() => {
    return Object.entries(frontendTools).map(([name, tool]) => {
      if (approvalSet.has(name) && approvalHelpers) {
        return wrapFrontendToolWithApproval(
          tool as FrontendTool<Record<string, unknown>, unknown>,
          name,
          approvalHelpers
        ) as AssistantTool
      }
      return tool
    })
  }, [frontendTools, approvalSet, approvalHelpers])

  return <>{wrappedTools.map((tool) => tool({}))}</>
}
