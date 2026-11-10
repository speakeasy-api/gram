import { createFileRoute } from "@tanstack/react-router";

import { McpServersRoute } from "@/pages/organization/McpServers";

export const Route = createFileRoute("/organizations/$idOrSlug/mcp-servers/")({
  component: McpServersRoute,
});
