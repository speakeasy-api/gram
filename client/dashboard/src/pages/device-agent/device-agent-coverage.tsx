import { Page } from "@/components/page-layout";
import { Stack } from "@speakeasy-api/moonshine";
import { useTelemetry } from "@/contexts/Telemetry";
import {
  CoverageSummaryTiles,
  ManagedDeviceTable,
} from "@/pages/org/device-integrations/coverage-widgets";
import { useDeviceIntegrationCoverage } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { useManagedDevices } from "@gram/client/react-query/managedDevices.js";

// Coverage section for the Device Agent page: bucket summary tiles plus the
// filterable managed-device list, org-wide across every connected MDM.
// Renders nothing until an MDM integration has synced at least one device,
// so the setup page stays clean.
export function DeviceAgentCoverage(): JSX.Element | null {
  const telemetry = useTelemetry();
  const integrationsEnabled =
    telemetry.isFeatureEnabled("gram-device-integrations") ?? false;

  if (!integrationsEnabled) return null;
  return <DeviceAgentCoverageInner />;
}

function DeviceAgentCoverageInner(): JSX.Element | null {
  const { data: coverage } = useDeviceIntegrationCoverage(
    undefined,
    undefined,
    { throwOnError: false },
  );
  const { data: devicePage, isLoading: devicesLoading } = useManagedDevices(
    { limit: 200 },
    undefined,
    { throwOnError: false },
  );

  if (!coverage || coverage.totalDevices === 0) return null;

  return (
    <Page.Section>
      <Page.Section.Title>Device coverage</Page.Section.Title>
      <Page.Section.Description>
        Managed devices from your MDM, joined against device agent heartbeats.
        Coverage is attested per assigned user: an active heartbeat means the
        device's user runs the agent, not that this device is monitored.
      </Page.Section.Description>
      <Page.Section.Body>
        <Stack gap={4}>
          <CoverageSummaryTiles coverage={coverage} />
          <ManagedDeviceTable
            devices={devicePage?.result.devices ?? []}
            isLoading={devicesLoading}
          />
        </Stack>
      </Page.Section.Body>
    </Page.Section>
  );
}
