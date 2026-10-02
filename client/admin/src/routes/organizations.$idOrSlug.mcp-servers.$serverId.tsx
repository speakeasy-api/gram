import { createFileRoute } from "@tanstack/react-router";

import { mcpServerNameKey } from "@/lib/gramAdminClient";
import { McpServerHealthRoute } from "@/pages/organization/McpServerHealth";
import { mcpServerHealthSearch } from "@/pages/organization/mcpServerHealthSearch";

export const Route = createFileRoute(
  "/organizations/$idOrSlug/mcp-servers/$serverId",
)({
  component: McpServerHealthRoute,
  validateSearch: mcpServerHealthSearch,
  staticData: {
    // The page draws the server's own header in place of the organization's.
    hideRecordHeader: true,
    // The name the page's health read records as it lands, keyed without
    // the window, so a window change never blanks the crumb.
    crumb: ({ idOrSlug, serverId }, search) => {
      const { project } = mcpServerHealthSearch(search);
      return idOrSlug && serverId && project
        ? { queryKey: mcpServerNameKey(idOrSlug, project, serverId) }
        : undefined;
    },
  },
});
