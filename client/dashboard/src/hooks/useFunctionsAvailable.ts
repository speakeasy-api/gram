import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

export type FunctionsAvailabilityStatus =
  | "loading"
  | "available"
  | "unavailable";

/**
 * Reports whether this deployment can run Speakeasy Functions. It is a
 * deployment-wide setting, not an organization entitlement: a deployment that
 * runs with Functions turned off answers false for every organization.
 *
 * `available` is true only once the server has said so. While the answer is
 * loading, could not be loaded at all, or is absent from an older server, it
 * is false, so a deployment without Functions never flashes Functions entry
 * points. A failed refetch keeps the last answer: the setting comes from the
 * server's configuration, so a network error does not change it.
 */
export function useFunctionsAvailable(): {
  status: FunctionsAvailabilityStatus;
  available: boolean;
} {
  const organization = useOrganization();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const status: FunctionsAvailabilityStatus = features.isPending
    ? "loading"
    : features.data?.functionsAvailable === true
      ? "available"
      : "unavailable";

  return { status, available: status === "available" };
}
