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
  return useCallback(
    (server) => {
      const href = renamedServerHref(
        routes,
        mcpServerSlug ?? "",
        server,
        location,
      );
      if (href) void navigate(href, { replace: true });
    },
    [routes, mcpServerSlug, location, navigate],
  );
}
