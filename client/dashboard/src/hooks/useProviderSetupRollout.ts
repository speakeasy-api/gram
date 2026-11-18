import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

/** Setup remains unavailable until the current organization's rollout is confirmed. */
export function useProviderSetupRollout(): "loading" | "enabled" | "disabled" {
  const organization = useOrganization();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  if (features.isPending) return "loading";
  return !features.isError && features.data?.providerSetupEnabled === true
    ? "enabled"
    : "disabled";
}
