import type { PageTab } from "@/components/page-templates";
import { useRoutes } from "@/routes";

export const MCP_SESSIONS_TITLE = "MCP Sessions";
export const MCP_SESSIONS_DESCRIPTION =
  "Who is connected to this project's MCP servers, and how they sign in.";

/**
 * The tabs MCP Sessions shows while gram-user-session-issuer-dashboard is on:
 * the connections themselves, then how sign-in and refresh work for them.
 */
export function useMcpSessionsTabs(): PageTab[] {
  const routes = useRoutes();
  return [
    { value: "overview", label: "Overview", href: routes.mcpSessions.href() },
    {
      value: "configuration",
      label: "Configuration",
      href: routes.mcpSessions.configuration.href(),
    },
  ];
}
