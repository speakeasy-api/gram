import { useTelemetry } from "@/contexts/Telemetry";
import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

// Assignments only gate delivery for the device agent (agent.getPlugins);
// marketplace installs (Claude/Cursor/Codex) ship every published plugin. So
// the Assignments section — and the sidebar's install/reach stats that depend
// on it — only surface for device-agent orgs: those enrolled in the program
// (the gram-device-agent flag) or with devices that have actually synced
// (productFeatures.deviceAgent, which is member-readable unlike the admin-only
// synced-users list, so non-admin viewers see the section too).
//
// Shared by the plugin detail page and its sidebar nav so the two never drift
// on whether the Assignments section exists.
export function usePluginAssignmentsVisible(): boolean {
  const organization = useOrganization();
  const isDeviceAgentEnabled =
    useTelemetry().isFeatureEnabled("gram-device-agent") ?? false;
  const { data: productFeatures } = useProductFeatures({
    organizationId: organization.id,
  });
  return isDeviceAgentEnabled || (productFeatures?.deviceAgent ?? false);
}
