import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import { useFleetCoverage } from "@gram/client/react-query/fleetCoverage.js";
import { useFleetDevicesInfinite } from "@gram/client/react-query/fleetDevices.js";
import {
  CoverageSummaryTiles,
  DeviceTable,
} from "@/pages/org/device-integrations/coverage-widgets";
import React, { useMemo } from "react";
import { Link } from "react-router";

// Coverage moves on the agents' poll cadence; don't refire the heavy joins
// on every window focus.
const FLEET_STALE_TIME = 30_000;

// Every machine running the agent, from the agents' own reports, plus the
// devices a connected MDM adds. Works with no MDM at all, which is the point:
// attestation should not depend on connecting one.
export function DeviceAgentDevicesTab(): React.JSX.Element {
  const orgRoutes = useOrgRoutes();
  const coverageQuery = useFleetCoverage(undefined, undefined, {
    throwOnError: false,
    staleTime: FLEET_STALE_TIME,
  });
  const devicesQuery = useFleetDevicesInfinite({ limit: 200 }, undefined, {
    throwOnError: false,
    staleTime: FLEET_STALE_TIME,
  });
  const devices = useMemo(
    () => devicesQuery.data?.pages.flatMap((page) => page.result.devices) ?? [],
    [devicesQuery.data],
  );
  const coverage = coverageQuery.data;
  const mdmConnected = coverage?.mdmConnected ?? true;

  return (
    <Stack gap={6} className="mt-3 mb-6">
      <div>
        <Heading variant="h4" className="mb-2">
          Devices
        </Heading>
        <Text muted small>
          Every machine running the agent, and whether it enforces the policy
          Speakeasy last sent it.
        </Text>
      </div>

      {coverageQuery.isError ? (
        <Text muted role="alert">
          Coverage could not be loaded. It will retry automatically.
        </Text>
      ) : null}

      {coverage && coverage.totalDevices === 0 && !mdmConnected ? (
        <Stack gap={3} align="start">
          <Text muted>
            No devices yet. A machine appears here once the agent is installed
            on it.
          </Text>
          <Button variant="secondary" size="sm" asChild>
            <Link to={orgRoutes.deviceAgent.href()}>
              <Button.Text>Install the agent</Button.Text>
            </Link>
          </Button>
        </Stack>
      ) : (
        <>
          {coverage && coverage.totalDevices > 0 ? (
            <Stack gap={2}>
              <CoverageSummaryTiles coverage={coverage} />
              {mdmConnected ? null : (
                <Text muted small>
                  <Link
                    to={orgRoutes.deviceAgent.mdmIntegrations.href()}
                    className="text-foreground underline underline-offset-4"
                  >
                    Connect an MDM
                  </Link>{" "}
                  to see the devices that aren't running the agent.
                </Text>
              )}
            </Stack>
          ) : null}
          {/* Waits for coverage too, so the columns don't change once it
              says whether an MDM is connected. */}
          <DeviceTable
            variant="fleet"
            mdmConnected={mdmConnected}
            devices={devices}
            isLoading={devicesQuery.isLoading || coverageQuery.isLoading}
            isError={devicesQuery.isError}
            onRetry={() => void devicesQuery.refetch()}
            hasMore={devicesQuery.hasNextPage}
            onLoadMore={() => void devicesQuery.fetchNextPage()}
            isLoadingMore={devicesQuery.isFetchingNextPage}
          />
        </>
      )}
    </Stack>
  );
}
