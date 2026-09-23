import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";

/**
 * Whether this organization is on the question-driven onboarding flow. Off,
 * missing, loading and error all keep the Setup board: the flag is a rollout
 * control, and the old flow is the safe default.
 */
export function useNewOnboardingEnabled(): boolean {
  return useFeatureFlag(FEATURE_FLAGS.newOnboarding).status === "enabled";
}
