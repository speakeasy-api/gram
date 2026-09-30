import { createFileRoute } from "@tanstack/react-router";

import { mcpServerHealthKey } from "@/lib/gramAdminClient";
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
    // The same key the page reads, so the bar fills from the page's request.
    crumb: ({ idOrSlug, serverId }, search) => {
      const { project, window } = mcpServerHealthSearch(search);
      return idOrSlug && serverId && project
        ? { queryKey: mcpServerHealthKey(idOrSlug, project, serverId, window) }
        : undefined;
    },
  },
});
