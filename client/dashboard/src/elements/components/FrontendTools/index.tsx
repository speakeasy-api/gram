import { AssistantTool } from "@assistant-ui/react";
import type { FrontendTools } from "@/elements/types";

export function FrontendTools({
  tools: frontendTools,
}: {
  tools: FrontendTools;
}): React.JSX.Element {
  return (
    <>
      {Object.entries(frontendTools).map(([name, tool]) => {
        // Each tool owns hooks, and the set is sized by RBAC grants that
        // resolve after the first paint, so every tool needs its own fiber.
        const Tool = tool as AssistantTool;
        return <Tool key={name} />;
      })}
    </>
  );
}
