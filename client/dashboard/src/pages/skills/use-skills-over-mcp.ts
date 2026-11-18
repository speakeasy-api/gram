import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

export type SkillsOverMcpStatus =
  | { status: "loading" }
  | { status: "enabled" }
  | { status: "disabled" }
  | { status: "error" };

/**
 * Whether skills over MCP is on for the organization: the staff-managed
 * skills_over_mcp product feature, toggled per organization from the admin
 * dashboard and off by default. The server reads the same feature before it
 * serves anything, so the hints only appear where agents can really load
 * skills.
 */
export function useSkillsOverMcpStatus(): SkillsOverMcpStatus {
  const organization = useOrganization();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  if (features.isPending) return { status: "loading" };
  if (features.isError || !features.data) return { status: "error" };
  return features.data.skillsOverMcpEnabled
    ? { status: "enabled" }
    : { status: "disabled" };
}

export function useSkillsOverMcpEnabled(): boolean {
  return useSkillsOverMcpStatus().status === "enabled";
}
