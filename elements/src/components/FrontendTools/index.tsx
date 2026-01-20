import type { FrontendTool } from '@/lib/tools'
import { AssistantTool } from '@assistant-ui/react'

export function FrontendTools({
  tools: frontendTools,
}: {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  tools: Record<string, AssistantTool | FrontendTool<any, any>>
}) {
  return (
    <>
      {Object.entries(frontendTools).map(([, tool]) =>
        (tool as AssistantTool)({})
      )}
    </>
  )
}
