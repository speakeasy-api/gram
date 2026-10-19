import { createFileRoute } from "@tanstack/react-router";
import { ExternalMcpMigration } from "@/pages/external-mcp-migration/ExternalMcpMigration";
import { externalMcpMigrationSearch } from "@/pages/external-mcp-migration/externalMcpMigrationSearch";
export const Route = createFileRoute("/external-mcp-migration")({
  component: ExternalMcpMigration,
  validateSearch: externalMcpMigrationSearch,
  staticData: { crumb: "External MCP migration" },
});
