import { EmptyState } from "@/components/page-layout";
import { ToolsetsGraphic } from "../toolsets/ToolsetsEmptyState";

export function DeploymentsEmptyState(): JSX.Element {
  return (
    <EmptyState
      heading="No deployments yet"
      description="The platform tracks how your MCP server evolves over time, allowing you to see change history and roll back if necessary."
      graphic={<ToolsetsGraphic />}
      graphicClassName="scale-90"
    />
  );
}
