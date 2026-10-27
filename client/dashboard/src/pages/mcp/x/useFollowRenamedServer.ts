import { removeVisitsMatching } from "@/components/command-palette/recentlyVisited";
import { useUser } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useCallback } from "react";
import { useLocation, useNavigate, useParams } from "react-router";
import { renamedServerHref } from "./MCPServerDetailsRouting";

// Call before invalidating server queries, so the refetch uses the new slug instead of 404ing on the old one.
export function useFollowRenamedServer(): (server: {
  id: string;
  slug?: string | null | undefined;
}) => void {
  const { mcpServerSlug } = useParams<{ mcpServerSlug: string }>();
  const location = useLocation();
  const navigate = useNavigate();
  const routes = useRoutes();
  const { orgSlug, projectSlug } = useSlugs();
  const userId = useUser().id || undefined;
  return useCallback(
    (server) => {
      const href = renamedServerHref(
        routes,
        mcpServerSlug ?? "",
        server,
        location,
      );
      if (!href) return;
      // Recents under the retired slug would land on a not-found page.
      const oldBase = routes.mcp.details.href(mcpServerSlug ?? "");
      removeVisitsMatching(
        userId,
        orgSlug,
        projectSlug,
        (entry) =>
          entry.href === oldBase || entry.href.startsWith(`${oldBase}/`),
      );
      void navigate(href, { replace: true });
    },
    [routes, mcpServerSlug, location, navigate, userId, orgSlug, projectSlug],
  );
}
