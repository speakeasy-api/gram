import { useOrganization } from "@/contexts/Auth";
import { getHttpStatusCode } from "@/lib/route-errors";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

export type FunctionsAvailability = "loading" | "available" | "unavailable";

/**
 * Whether this deployment runs Speakeasy Functions. It is a deployment-wide
 * setting (the server's Functions provider), served on the organization's
 * product features. Functions entry points stay hidden while it loads, so a
 * deployment without Functions never flashes them.
 *
 * Only an explicit `false` hides Functions once loaded. A server that does not
 * send the field predates the setting and always runs Functions, and a caller
 * refused the organization's features (a custom role without org:read) learns
 * nothing about the deployment; the server still refuses Functions requests
 * there when Functions are off.
 */
export function useFunctionsAvailability(): FunctionsAvailability {
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
  return features.data?.functionsAvailable === false
    ? "unavailable"
    : "available";
}

/** True only once the deployment is confirmed to run Functions. */
export function useFunctionsAvailable(): boolean {
  return useFunctionsAvailability() === "available";
}
