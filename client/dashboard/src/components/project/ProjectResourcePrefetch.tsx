import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useMcpDiscoveryAccess } from "@/hooks/useMcpDiscoveryAccess";
import { useRBAC } from "@/hooks/useRBAC";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildLatestDeploymentQuery } from "@gram/client/react-query/latestDeployment.js";
import { buildListToolsetsQuery } from "@gram/client/react-query/listToolsets.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

/** Warm resource metadata after authentication, without waiting for MCP cards. */
export function ProjectResourcePrefetch(): null {
  const client = useGramContext();
  const queryClient = useQueryClient();
  const project = useProject();
  const { projectSlug } = useSlugs();
  const { hasScope, isLoading } = useRBAC();
  const { canReadServers } = useMcpDiscoveryAccess(projectSlug);
  const canReadDeployment =
    !!project.id && hasScope("project:read", project.id);

  useEffect(() => {
    // Do not warm the fallback project on organization pages or invalid routes.
    if (isLoading || !projectSlug || projectSlug !== project.slug) return;
    if (canReadDeployment) {
      // Match the existing deployment consumers' key. The project-scoped SDK
      // client supplies the request header, and invalidates on project changes.
      void queryClient.prefetchQuery(buildLatestDeploymentQuery(client));
    }
    if (canReadServers) {
      void queryClient.prefetchQuery(
        buildListToolsetsQuery(client, { gramProject: projectSlug }),
      );
    }
  }, [
    client,
    queryClient,
    projectSlug,
    project.slug,
    isLoading,
    canReadDeployment,
    canReadServers,
  ]);

  return null;
}
