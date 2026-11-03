import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";

/**
 * Whether this organization is in the Organizational Memory rollout. The
 * flag only decides what the dashboard shows; every memory endpoint checks
 * the same key and the caller's grants on its own.
 */
export function useOrgMemoryEnabled(): boolean {
  return useFeatureFlag(FEATURE_FLAGS.orgMemory).status === "enabled";
}
