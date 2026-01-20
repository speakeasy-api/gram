import { AssistantTool } from '@assistant-ui/react'
import type { FrontendTools } from '@/types'

export function FrontendTools({
  tools: frontendTools,
}: {
  tools: FrontendTools
}) {
  return (
    <>
      {Object.entries(frontendTools).map(([, tool]) =>
        (tool as AssistantTool)({})
      )}
    </>
  )
}
