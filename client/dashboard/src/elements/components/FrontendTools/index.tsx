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
        // Render each tool as its own element rather than calling it inline:
        // an inline call runs the tool's hooks inside THIS component, so a
        // frontend-tool set whose size changes between renders (e.g. the
        // assistant onboarding tools, which are gated on RBAC grants that
        // resolve after the first paint) changes this component's hook count
        // and trips "Rendered more hooks than during the previous render".
        const Tool = tool as AssistantTool;
        return <Tool key={name} />;
      })}
    </>
  );
}
