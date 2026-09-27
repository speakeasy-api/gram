import { createFileRoute } from "@tanstack/react-router";

import { McpServersRoute } from "@/pages/organization/McpServers";
import { mcpServersSearch } from "@/pages/organization/mcpServersSearch";

export const Route = createFileRoute("/organizations/$idOrSlug/mcp-servers")({
  component: McpServersRoute,
  validateSearch: mcpServersSearch,
  staticData: { crumb: "MCP Servers" },
});
