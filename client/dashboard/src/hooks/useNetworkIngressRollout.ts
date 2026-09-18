import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRBAC } from "@/hooks/useRBAC";

type NetworkIngressRolloutStatus = "loading" | "enabled" | "disabled" | "error";

export function useNetworkIngressRollout(): {
  status: NetworkIngressRolloutStatus;
  rolloutEnabled: boolean;
  canManageIngress: boolean;
} {
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const status: NetworkIngressRolloutStatus = features.isPending
    ? "loading"
    : features.isError || !features.data
      ? "error"
      : features.data.networkIngressEnabled
        ? "enabled"
        : "disabled";
  const canManageIngress = hasScope("org:admin");

  return {
    status,
    rolloutEnabled: status === "enabled",
    canManageIngress,
  };
}
