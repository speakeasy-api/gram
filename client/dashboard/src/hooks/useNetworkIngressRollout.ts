import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRBAC } from "@/hooks/useRBAC";

export function useNetworkIngressRollout(): {
  rolloutEnabled: boolean;
  canManageIngress: boolean;
  adminRolloutEnabled: boolean;
} {
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const rolloutEnabled = features.data?.networkIngressEnabled === true;
  const canManageIngress = hasScope("org:admin");

  return {
    rolloutEnabled,
    canManageIngress,
    adminRolloutEnabled: rolloutEnabled && canManageIngress,
  };
}
