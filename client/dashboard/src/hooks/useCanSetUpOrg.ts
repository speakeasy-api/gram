import { useProductTier } from "@/hooks/useProductTier";
import { useRBAC } from "@/hooks/useRBAC";

/**
 * Whether to offer org setup (SSO, directory sync, policies): an admin task on
 * the tiers that carry the enterprise feature set. Shared by the sidebar entry
 * and the org home card so the two surfaces appear and disappear together.
 */
export function useCanSetUpOrg(): boolean {
  const { hasScope } = useRBAC();
  const productTier = useProductTier();

  return (
    hasScope("org:admin") &&
    (productTier === "enterprise" || productTier === "payg")
  );
}
