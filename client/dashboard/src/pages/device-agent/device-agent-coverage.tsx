import { Page } from "@/components/page-layout";
import { useTelemetry } from "@/contexts/Telemetry";
import {
  CoverageSummaryTiles,
  ManagedDeviceTable,
} from "@/pages/org/device-integrations/coverage-widgets";
import { useDeviceIntegrationCoverage } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { useManagedDevicesInfinite } from "@gram/client/react-query/managedDevices.js";
import { Stack } from "@speakeasy-api/moonshine";
import { useMemo } from "react";

// Coverage moves on the hourly sync cadence; don't refire the heavy joins on
// every window focus.
const COVERAGE_STALE_TIME = 30_000;

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
    { throwOnError: false, staleTime: COVERAGE_STALE_TIME },
  );
  const devicesQuery = useManagedDevicesInfinite({ limit: 200 }, undefined, {
    throwOnError: false,
    staleTime: COVERAGE_STALE_TIME,
  });
  const devices = useMemo(
    () => devicesQuery.data?.pages.flatMap((page) => page.result.devices) ?? [],
    [devicesQuery.data],
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
            devices={devices}
            isLoading={devicesQuery.isLoading}
            isError={devicesQuery.isError}
            onRetry={() => void devicesQuery.refetch()}
            hasMore={devicesQuery.hasNextPage}
            onLoadMore={() => void devicesQuery.fetchNextPage()}
            isLoadingMore={devicesQuery.isFetchingNextPage}
          />
        </Stack>
      </Page.Section.Body>
    </Page.Section>
  );
}
