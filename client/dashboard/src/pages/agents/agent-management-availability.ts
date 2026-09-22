import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import {
  useIsPlatformAdmin,
  useOrganization,
  useSession,
} from "@/contexts/Auth";
import { useFeatureFlag, type FeatureFlagResult } from "@/hooks/useFeatureFlag";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { FEATURE_FLAGS } from "@/lib/featureFlags";

export const DEMO_UNAVAILABLE_REASON =
  "Agent management is unavailable in the shared demo because it requires active organization membership.";

/** Why this session cannot manage agents at all, or null when it can. */
export function useAgentManagementAvailability(): {
  sessionReason: string | null;
  isDemo: boolean;
} {
  const organization = useOrganization();
  const session = useSession();
  const isPlatformAdmin = useIsPlatformAdmin();
  const hasUnsupportedSession =
    session.organizationOverride || Boolean(session.impersonatorEmail);
  const hasScopeOverride =
    getRBACScopeOverrideHeader(import.meta.env.DEV || isPlatformAdmin) !== null;
  let sessionReason: string | null = null;
  if (hasUnsupportedSession)
    sessionReason =
      "Support and impersonated sessions cannot manage agents. Switch to an ordinary Gram session.";
  else if (hasScopeOverride)
    sessionReason =
      "Agent management is disabled while an RBAC scope override is active.";
  return { sessionReason, isDemo: organization.slug === DEMO_ORG_SLUG };
}

/**
 * Why agent identities are unavailable under the rollout, or null when both
 * agent management and agent credentials are enabled.
 */
export function agentIdentityRolloutReason(
  management: FeatureFlagResult,
  credentials: FeatureFlagResult,
): string | null {
  const statuses = [management.status, credentials.status];
  if (statuses.every((status) => status === "enabled")) return null;
  if (statuses.includes("disabled"))
    return "Agent identities are disabled for this organization.";
  if (statuses.includes("loading"))
    return "Checking agent identity availability…";
  return "Agent identity availability could not be determined.";
}

/** The shared rollout gate for agent identity setup and key issuance. */
export function useAgentIdentityRollout(): string | null {
  const management = useFeatureFlag(FEATURE_FLAGS.agentManagement);
  const credentials = useFeatureFlag(FEATURE_FLAGS.agentCredentials);
  return agentIdentityRolloutReason(management, credentials);
}
