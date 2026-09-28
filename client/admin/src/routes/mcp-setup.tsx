import { AdminMcpSetup } from "@/pages/admin-mcp/AdminMcpSetup";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/mcp-setup")({
  component: AdminMcpSetup,
  staticData: { crumb: "Admin MCP" },
});
