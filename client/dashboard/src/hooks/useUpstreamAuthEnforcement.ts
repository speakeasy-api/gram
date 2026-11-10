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
 * every write on its own. A failed refetch keeps the last known state.
 */
export function useUpstreamAuthEnforcement(): UpstreamAuthEnforcementStatus {
  const organization = useOrganization();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  if (features.data) {
    // Absent when the server could not read the state.
    const enforced = features.data.upstreamAuthEnforced;
    if (enforced === undefined) {
      return "error";
    }
    return enforced ? "enforced" : "legacy";
  }
  return features.isPending ? "loading" : "error";
}
