import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

export function useLogsEnabled(): {
  enabled: boolean;
  pending: boolean;
  error: boolean;
  /** Confirmed off, not just unknown after a failed request. */
  off: boolean;
} {
  const organization = useOrganization();
  const { data, isPending, isError } = useProductFeatures({
    organizationId: organization.id,
  });
  const enabled = data?.logsEnabled === true;
  return {
    enabled,
    pending: isPending,
    error: isError,
    off: !isPending && !isError && !enabled,
  };
}
