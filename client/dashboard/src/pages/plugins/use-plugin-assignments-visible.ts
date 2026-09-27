import { useRBAC } from "@/hooks/useRBAC";
import { useTelemetry } from "@/contexts/Telemetry";
import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

// Assignment reach is admin-only. Do not fetch org features for project-only
// writers, and do not expose cached flags after an authorization downgrade.
export function usePluginAssignmentsVisible(): boolean {
  const organization = useOrganization();
  const { hasScope, isLoading } = useRBAC();
  const canRead = !isLoading && hasScope("org:admin", organization.id);
  const isDeviceAgentEnabled =
    useTelemetry().isFeatureEnabled("gram-device-agent") ?? false;
  const { data: productFeatures } = useProductFeatures(
    {
      organizationId: organization.id,
    },
    undefined,
    { enabled: canRead },
  );
  return (
    canRead && (isDeviceAgentEnabled || (productFeatures?.deviceAgent ?? false))
  );
}
