import { useSession } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { DEMO_ORG_SLUG } from "@/lib/demo";

export type KillswitchAccess = {
  canAccess: boolean;
  isLoading: boolean;
  reason: "allowed" | "demo" | "loading" | "rollout" | "scope" | "support";
};

/** Fail-closed customer Killswitch access shared by routing and discovery. */
export function useKillswitchAccess(): KillswitchAccess {
  const session = useSession();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const rollout = useFeatureFlag(FEATURE_FLAGS.killswitches);

  // The shared demo is intentionally read-only. This is only a discovery/UI
  // gate; the management API remains authoritative for every mutation.
  if (session.organization.slug === DEMO_ORG_SLUG) {
    return { canAccess: false, isLoading: false, reason: "demo" };
  }

  if (rbacLoading || rollout.status === "loading") {
    return { canAccess: false, isLoading: true, reason: "loading" };
  }

  if (rollout.status !== "enabled") {
    return { canAccess: false, isLoading: false, reason: "rollout" };
  }

  if (session.organizationOverride || Boolean(session.impersonatorEmail)) {
    return { canAccess: false, isLoading: false, reason: "support" };
  }

  if (!hasScope("org:admin")) {
    return { canAccess: false, isLoading: false, reason: "scope" };
  }

  return { canAccess: true, isLoading: false, reason: "allowed" };
}
