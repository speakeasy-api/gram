import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

export type UpstreamAuthEnforcementStatus =
  | "loading"
  | "enforced"
  | "legacy"
  | "error";

/**
 * The organization's upstream auth enforcement state. It selects which
 * upstream identity surface a server's settings show; the server authorizes
 * every write on its own.
 */
export function useUpstreamAuthEnforcement(): UpstreamAuthEnforcementStatus {
  const organization = useOrganization();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  if (features.isPending) {
    return "loading";
  }
  if (features.isError || !features.data) {
    return "error";
  }
  return features.data.upstreamAuthEnforced ? "enforced" : "legacy";
}
