import { createFileRoute, Outlet } from "@tanstack/react-router";

import { mcpServersSearch } from "@/pages/organization/mcpServersSearch";

// A layout, so the list and a server's health page share the crumb and the
// project in the address. The crumb takes the project with it back to the list.
export const Route = createFileRoute("/organizations/$idOrSlug/mcp-servers")({
  component: Outlet,
  validateSearch: mcpServersSearch,
  staticData: { crumb: "MCP Servers" },
});
