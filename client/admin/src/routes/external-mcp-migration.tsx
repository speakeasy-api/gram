import { createFileRoute } from "@tanstack/react-router";
import { ExternalMcpMigration } from "@/pages/external-mcp-migration/ExternalMcpMigration";
export const Route = createFileRoute("/external-mcp-migration")({
  component: ExternalMcpMigration,
  staticData: { crumb: "External MCP migration" },
});
