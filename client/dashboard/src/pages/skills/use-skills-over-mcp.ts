import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";

/**
 * Whether the dashboard shows skills as served over MCP. The server reads the
 * same org-targeted flag before it serves anything, so the hints only appear
 * where agents can really load skills.
 */
export function useSkillsOverMcpEnabled(): boolean {
  return useFeatureFlag(FEATURE_FLAGS.skillsOverMcp).status === "enabled";
}
