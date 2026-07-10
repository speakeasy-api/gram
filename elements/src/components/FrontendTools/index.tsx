import { AssistantTool } from "@assistant-ui/react";
import type { FrontendTools } from "#elements/types";

export function FrontendTools({
  tools: frontendTools,
}: {
  tools: FrontendTools;
}): React.JSX.Element {
  return (
    <>
      {Object.entries(frontendTools).map(([, tool]) =>
        (tool as AssistantTool)({}),
      )}
    </>
  );
}
