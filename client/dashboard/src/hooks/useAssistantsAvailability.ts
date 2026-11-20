import { useOrganization } from "@/contexts/Auth";
import { getHttpStatusCode } from "@/lib/route-errors";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

export type AssistantsAvailability = "loading" | "available" | "unavailable";

/**
 * Whether this deployment runs assistants. It is a deployment-wide setting
 * (the server's assistant runtime provider), served on the organization's
 * product features. Assistant surfaces stay hidden while it loads, so a
 * deployment without assistants never flashes them.
 *
 * Only an explicit `false` hides assistants once loaded. A server that does
 * not send the field predates the setting and always runs assistants, and a
 * caller refused the organization's features (a custom role without
 * org:read) learns nothing about the deployment; the server still refuses
 * assistant requests there when assistants are off.
 */
export function useAssistantsAvailability(): AssistantsAvailability {
  const organization = useOrganization();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  if (features.isPending) return "loading";
  if (features.isError) {
    return getHttpStatusCode(features.error) === 403
      ? "available"
      : "unavailable";
  }
  return features.data?.assistantsAvailable === false
    ? "unavailable"
    : "available";
}

/** True only once the deployment is confirmed to run assistants. */
export function useAssistantsAvailable(): boolean {
  return useAssistantsAvailability() === "available";
}
