import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/switch";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import type { DeviceIntegrationCoverage } from "@gram/client/models/components/deviceintegrationcoverage.js";
import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";
import { useDeviceIntegrationCoverage } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { useDeviceIntegrationProviders } from "@gram/client/react-query/deviceIntegrationProviders.js";
import { useManagedDevices } from "@gram/client/react-query/managedDevices.js";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { PlugZap } from "lucide-react";
import { useState } from "react";
import { Navigate, useParams } from "react-router";
import { CoverageSummaryTiles, ManagedDeviceTable } from "./coverage-widgets";
import {
  ConnectionStatusBadge,
  DeviceIntegrationConfigureSheet,
} from "./device-integration-configure-sheet";
import {
  type DeviceIntegrationScheduleRow,
  DeviceIntegrationSchedulesTable,
} from "./device-integration-schedules-table";
import { providerUI } from "./provider-ui";
import { useDeviceIntegrationConfigForm } from "./use-device-integration-config";
import {
  runtimeOrDefault,
  useDeviceScheduleRuntimes,
} from "./use-device-integration-schedules";

// Detail page for one MDM integration: its connection state and controls,
// the vendor-scoped coverage breakdown, sync schedules, and the synced
// device inventory.
export default function MdmIntegrationDetail(): JSX.Element | null {
  const { provider: providerID = "" } = useParams<{ provider: string }>();
  const { data, isLoading } = useDeviceIntegrationProviders();

  const provider = data?.providers.find((p) => p.id === providerID);
  if (isLoading) return null;
  if (!provider) return <Navigate to=".." replace />;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <MdmIntegrationDetailInner provider={provider} />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function MdmIntegrationDetailInner({
  provider,
}: {
  provider: DeviceIntegrationProvider;
}) {
  const [configureOpen, setConfigureOpen] = useState(false);
  const form = useDeviceIntegrationConfigForm(provider);
  const { runtimes, toggle, retry } = useDeviceScheduleRuntimes(provider.id);
  const ui = providerUI(provider);
  const Icon = ui.icon;

  const { data: coverage } = useDeviceIntegrationCoverage(
    { provider: provider.id },
    undefined,
    { throwOnError: false },
  );
  const { data: devicePage, isLoading: devicesLoading } = useManagedDevices(
    { provider: provider.id, limit: 200 },
    undefined,
    { throwOnError: false },
  );

  const scheduleRows = provider.schedules.map(
    (schedule): DeviceIntegrationScheduleRow => ({
      key: `${provider.id}:${schedule.schedule}`,
      schedule,
      runtime: runtimeOrDefault(runtimes, schedule.schedule),
      configured: form.isConfigured,
      connectionEnabled: form.enabled,
      toggle,
      retry,
    }),
  );

  return (
    <>
      <Page.Section>
        <Page.Section.Title>
          <Stack direction="horizontal" align="center" gap={2}>
            <Icon className="text-foreground h-5 w-5 shrink-0" />
            {provider.displayName}
            <ConnectionStatusBadge
              enabled={form.enabled}
              configured={form.isConfigured}
            />
          </Stack>
        </Page.Section.Title>
        <Page.Section.Description>{ui.description}</Page.Section.Description>
        <Page.Section.CTA>
          <Stack direction="horizontal" align="center" gap={3}>
            <RequireScope scope="org:admin" level="component">
              <SimpleTooltip
                tooltip={
                  form.isConfigured
                    ? "Pause or resume the whole connection. Applies immediately."
                    : "Connect the provider before enabling it."
                }
              >
                <span>
                  <Switch
                    checked={form.enabled}
                    onCheckedChange={form.saveEnabled}
                    disabled={
                      !form.isConfigured || form.isLoading || form.isMutating
                    }
                    aria-label={`Enable ${provider.displayName} connection`}
                  />
                </span>
              </SimpleTooltip>
            </RequireScope>
            <RequireScope scope="org:admin" level="component">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setConfigureOpen(true)}
              >
                <Button.LeftIcon>
                  <PlugZap className="size-3.5" />
                </Button.LeftIcon>
                <Button.Text>
                  {form.isConfigured ? "Configure" : "Connect"}
                </Button.Text>
              </Button>
            </RequireScope>
          </Stack>
        </Page.Section.CTA>
        <Page.Section.Body>
          <Stack gap={4}>
            {coverage ? <CoverageHeadline coverage={coverage} /> : null}
            {coverage && coverage.totalDevices > 0 ? (
              <CoverageSummaryTiles coverage={coverage} />
            ) : null}
          </Stack>
        </Page.Section.Body>
      </Page.Section>

      <Page.Section>
        <Page.Section.Title>Sync schedules</Page.Section.Title>
        <Page.Section.Description>
          Each schedule polls the vendor on its own cadence and can be paused or
          run immediately.
        </Page.Section.Description>
        <Page.Section.Body>
          <DeviceIntegrationSchedulesTable rows={scheduleRows} />
        </Page.Section.Body>
      </Page.Section>

      <Page.Section>
        <Page.Section.Title>Managed devices</Page.Section.Title>
        <Page.Section.Description>
          The device inventory synced from {provider.displayName}, with each
          device's agent coverage.
        </Page.Section.Description>
        <Page.Section.Body>
          <ManagedDeviceTable
            devices={devicePage?.result.devices ?? []}
            isLoading={devicesLoading}
          />
          <DeviceIntegrationConfigureSheet
            provider={provider}
            form={form}
            open={configureOpen}
            onOpenChange={setConfigureOpen}
          />
        </Page.Section.Body>
      </Page.Section>
    </>
  );
}

// The headline number an admin acts on: of the devices this vendor manages,
// how many have an assigned user with a live agent heartbeat.
function CoverageHeadline({
  coverage,
}: {
  coverage: DeviceIntegrationCoverage;
}) {
  if (coverage.totalDevices === 0) {
    return (
      <Type muted>
        No devices synced yet. Devices appear after the first successful
        inventory sync.
      </Type>
    );
  }
  const percent = Math.round(
    (coverage.agentActive / coverage.totalDevices) * 100,
  );
  return (
    <Stack direction="horizontal" align="baseline" gap={2}>
      <Type variant="body" className="text-3xl font-semibold tabular-nums">
        {percent}%
      </Type>
      <Type muted>
        of {coverage.totalDevices} managed device
        {coverage.totalDevices === 1 ? "" : "s"} have an assigned user with an
        active agent
        {coverage.unmanagedAgentUsers > 0
          ? ` · ${coverage.unmanagedAgentUsers} agent user${coverage.unmanagedAgentUsers === 1 ? "" : "s"} with no managed device`
          : ""}
      </Type>
    </Stack>
  );
}
