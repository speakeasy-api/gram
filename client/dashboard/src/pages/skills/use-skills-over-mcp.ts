import { useOrganization } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

/**
 * Whether the dashboard shows skills as served over MCP. It is on when the
 * org-targeted skills-over-mcp flag is on, or when staff turned on the
 * skills_over_mcp product feature for the organization from the admin
 * dashboard. The server reads the same two sources before it serves anything,
 * so the hints only appear where agents can really load skills.
 */
export function useSkillsOverMcpEnabled(): boolean {
  const organization = useOrganization();
  const flagEnabled =
    useFeatureFlag(FEATURE_FLAGS.skillsOverMcp).status === "enabled";
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false, enabled: !flagEnabled },
  );
  return flagEnabled || features.data?.skillsOverMcpEnabled === true;
}
