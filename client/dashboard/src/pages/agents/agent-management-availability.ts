import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import {
  useIsPlatformAdmin,
  useOrganization,
  useSession,
} from "@/contexts/Auth";
import { DEMO_ORG_SLUG } from "@/lib/demo";

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
