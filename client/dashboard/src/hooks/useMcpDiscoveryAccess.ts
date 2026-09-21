import { useOrganization } from "@/contexts/Auth";
import { hasScopeInGrants, useRBAC } from "./useRBAC";

/** The insights shell can target a project other than the URL-active project. */
export function useMcpDiscoveryAccess(projectSlug?: string) {
  const organization = useOrganization();
  const { grants } = useRBAC();
  const projectId = organization.projects.find(
    (project) => project.slug === projectSlug,
  )?.id;
  return {
    canReadServers:
      !!projectId && hasScopeInGrants(grants, "mcp:read", undefined, projectId),
    canReadEndpoints:
      !!projectId && hasScopeInGrants(grants, "mcp:read", projectId, projectId),
  };
}
