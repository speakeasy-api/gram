import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";

export function useNetworkIngressRollout(): {
  rolloutEnabled: boolean;
  canManageIngress: boolean;
  adminRolloutEnabled: boolean;
} {
  const rollout = useFeatureFlag(FEATURE_FLAGS.networkIngressRollout);
  const { hasScope } = useRBAC();
  const rolloutEnabled = rollout.status === "enabled";
  const canManageIngress = hasScope("org:admin");

  return {
    rolloutEnabled,
    canManageIngress,
    adminRolloutEnabled: rolloutEnabled && canManageIngress,
  };
}
