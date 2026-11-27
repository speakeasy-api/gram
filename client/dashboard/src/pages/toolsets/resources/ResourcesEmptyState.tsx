import { EmptyState } from "@/components/page-layout";
import type { ReactNode } from "react";
import { ToolsetsGraphic } from "../ToolsetsEmptyState";

/** Matches the Prompts tab's empty state so the two hosted tabs read alike. */
export function ResourcesEmptyState({
  action,
}: {
  action: ReactNode;
}): JSX.Element {
  return (
    <EmptyState
      heading="No resources yet"
      description="Resources are files and data your MCP server shares with connected clients like Claude Desktop or Cursor, so agents can read them as context without calling a tool."
      nonEmptyProjectCTA={action}
      graphic={<ToolsetsGraphic />}
      graphicClassName="scale-90"
    />
  );
}
