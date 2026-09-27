import { useOrganization, useProject } from "@/contexts/Auth";
import { hasAnyUnblockedScopeInGrants, useRBAC } from "@/hooks/useRBAC";

/** Plugin references are project-owned; this does not grant skill or MCP edits. */
export function usePluginWriteAccess(): boolean {
  const { grants, isLoading } = useRBAC();
  const project = useProject();
  const organization = useOrganization();
  if (isLoading) return false;
  return hasAnyUnblockedScopeInGrants(grants ?? [], [
    { scope: "org:admin", resourceId: organization.id },
    { scope: "plugin:write", resourceId: project.id, projectId: project.id },
  ]);
}
