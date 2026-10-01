import type { WorkloadOrganizationConnectionDetails } from "@gram/client/models/components/workloadorganizationconnectiondetails.js";
import { useWorkloadOrganizationConnectionDetails } from "@gram/client/react-query/workloadOrganizationConnectionDetails.js";

export const TOKEN_ENDPOINT_HINT =
  "The authorization server’s token endpoint the issuer must use";

export const AUDIENCE_RULE =
  "The assertion's aud must be exactly this issuer or the token endpoint URL. Nothing else on that host is accepted.";

/**
 * The organization's own token endpoint. Every value is read from the server,
 * which derives it the same way the endpoint's discovery document does.
 */
export function useOrganizationConnection(): ReturnType<
  typeof useWorkloadOrganizationConnectionDetails
> {
  return useWorkloadOrganizationConnectionDetails(undefined, undefined, {
    throwOnError: false,
  });
}

/**
 * Why there is no token endpoint worth copying, or null when there is one.
 */
export function unavailableMessage(
  details: WorkloadOrganizationConnectionDetails,
): string | null {
  if (!details.available) {
    return "Organization token endpoint isn't enabled for this organization";
  }
  if (details.notReadyReason === "workload_grant_unavailable") {
    return "This deployment doesn't accept workload identity tokens";
  }
  return null;
}

/**
 * A warning to show beside a token endpoint that is served but where every
 * exchange will still fail, or null when nothing Gram knows of stops one.
 */
export function notReadyWarning(
  details: WorkloadOrganizationConnectionDetails,
): string | null {
  if (details.notReadyReason === "agent_rollout_disabled") {
    return "Agent authorization isn't enabled for this organization, and the token endpoint refuses workload identity tokens without it. Every exchange will fail until it is enabled.";
  }
  return null;
}
