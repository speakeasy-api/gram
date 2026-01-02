import { AssistantTool } from '@assistant-ui/react'

export function FrontendTools({
  tools: frontendTools,
}: {
  tools: Record<string, AssistantTool>
}) {
  return <>{Object.entries(frontendTools).map(([, tool]) => tool({}))}</>
}
