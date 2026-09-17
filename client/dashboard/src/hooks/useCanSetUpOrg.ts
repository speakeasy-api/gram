import { useProductTier } from "@/hooks/useProductTier";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrganization } from "@/contexts/Auth";

/**
 * Eligibility for the promotional rollout CTA, not access to onboarding.
 */
export function useCanSetUpOrg(): boolean {
  const { hasScope } = useRBAC();
  const productTier = useProductTier();

  return (
    hasScope("org:admin") &&
    (productTier === "enterprise" || productTier === "payg")
  );
}

export function useCanViewOrgSetup(): boolean {
  const organization = useOrganization();
  const { hasScope, isLoading, error } = useRBAC();
  return (
    Boolean(organization.id) &&
    !isLoading &&
    !error &&
    hasScope("org:read", organization.id)
  );
}
