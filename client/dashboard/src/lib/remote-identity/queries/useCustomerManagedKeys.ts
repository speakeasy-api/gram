import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

/**
 * Whether the organization may sign with its own KMS keys. Key sets, the keys
 * behind them, and attaching a set to a client are all gated on the
 * customer_managed_encryption_keys entitlement, so every signing key control
 * asks this first. Reads as false while loading so nothing flashes enabled.
 */
export function useCustomerManagedKeys(): {
  entitled: boolean;
  isLoading: boolean;
} {
  const organization = useOrganization();
  const { data, isLoading } = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { staleTime: 30_000, throwOnError: false },
  );
  return {
    entitled: !isLoading && data?.customerManagedEncryptionKeysEnabled === true,
    isLoading,
  };
}
