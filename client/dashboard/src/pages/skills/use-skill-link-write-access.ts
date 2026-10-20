import { useProject } from "@/contexts/Auth";
import { hasScopeInGrants, useRBAC } from "@/hooks/useRBAC";
import type { SkillLinkTarget } from "./skill-links";

/**
 * Whether the caller may change which skills a target serves. The checks
 * mirror the server: MCP write on the server (its grants are keyed on the
 * toolset it fronts), project-level MCP write for a gateway, and project
 * write for the organization gateway.
 */
export function useCanWriteSkillLinkTarget(
  target: SkillLinkTarget,
  { grantResourceId }: { grantResourceId?: string } = {},
): boolean {
  const { grants, isLoading } = useRBAC();
  const project = useProject();
  if (isLoading || !grants) return false;
  switch (target.kind) {
    case "mcp_server":
      return hasScopeInGrants(
        grants,
        "mcp:write",
        grantResourceId ?? target.mcpServerId,
        project.id,
      );
    case "meta_mcp_server":
      return hasScopeInGrants(grants, "mcp:write", project.id, project.id);
    case "organization_gateway":
      return hasScopeInGrants(grants, "project:write", project.id, project.id);
  }
}
