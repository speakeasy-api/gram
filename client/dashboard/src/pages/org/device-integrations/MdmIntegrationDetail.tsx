import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { Switch } from "@/components/ui/Switch";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import type { DeviceIntegrationCoverage } from "@gram/client/models/components/deviceintegrationcoverage.js";
import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";
import { buildDeviceIntegrationCoverageQuery } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { useDeviceIntegrationCoverage } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { useDeviceIntegrationProviders } from "@gram/client/react-query/deviceIntegrationProviders.js";
import { useManagedDevicesInfinite } from "@gram/client/react-query/managedDevices.js";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { useQueries } from "@tanstack/react-query";
import { PlugZap } from "lucide-react";
import { useMemo, useState } from "react";
import { Link, Navigate, useParams } from "react-router";
import { CoverageSummaryTiles, ManagedDeviceTable } from "./coverage-widgets";
import {
  isProviderVisible,
  isSink,
  providerRole,
  ROLE_COPY,
} from "./provider-role";
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

// Coverage moves on the hourly sync cadence; don't refire the heavy joins on
// every window focus.
const COVERAGE_STALE_TIME = 30_000;

// Detail page for one MDM integration: its connection state and controls,
// the vendor-scoped coverage breakdown, sync schedules, and the synced
// device inventory. Routed below the Device Agent tab shell, so it carries
// its own rollout-flag gate.
export default function MdmIntegrationDetail(): JSX.Element | null {
  const telemetry = useTelemetry();
  const mdmEnabled = telemetry.isFeatureEnabled("gram-device-integrations");
  const { provider: providerID = "" } = useParams<{ provider: string }>();
  const { data, isLoading } = useDeviceIntegrationProviders(
    undefined,
    undefined,
    { staleTime: COVERAGE_STALE_TIME },
  );

  if (mdmEnabled === undefined) return null;
  if (!mdmEnabled) return <Navigate to=".." replace />;

  const provider = data?.providers.find((p) => p.id === providerID);
  if (isLoading) return null;
  // Hidden providers 404 to the list even by direct URL, so a not-yet-supported
  // integration can't be reached out of band.
  if (!provider || !isProviderVisible(provider)) {
    return <Navigate to=".." replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [provider.id]: provider.displayName }}
        />
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
  const role = providerRole(provider);
  const sink = role === "sink";

  const [configureOpen, setConfigureOpen] = useState(false);
  const form = useDeviceIntegrationConfigForm(provider);
  const { runtimes, toggle, retry } = useDeviceScheduleRuntimes(
    provider.id,
    role,
  );
  const ui = providerUI(provider);
  const Icon = ui.icon;

  // A sink republishes the whole org's fleet, so its coverage and evidence are
  // org-wide (no provider filter) — that is exactly the record set it pushes.
  // A source shows only the inventory it reports, so it stays provider-scoped.
  const { data: coverage, isError: coverageError } =
    useDeviceIntegrationCoverage(
      sink ? undefined : { provider: provider.id },
      undefined,
      { throwOnError: false, staleTime: COVERAGE_STALE_TIME },
    );
  // Only sources own a device inventory; a sink has none of its own, so the
  // query never runs on a sink page.
  const devicesQuery = useManagedDevicesInfinite(
    { provider: provider.id, limit: 200 },
    undefined,
    { throwOnError: false, staleTime: COVERAGE_STALE_TIME, enabled: !sink },
  );
  const devices = useMemo(
    () => devicesQuery.data?.pages.flatMap((page) => page.result.devices) ?? [],
    [devicesQuery.data],
  );

  const scheduleRows = useMemo(
    () =>
      provider.schedules.map((schedule): DeviceIntegrationScheduleRow => ({
        key: `${provider.id}:${schedule.schedule}`,
        schedule,
        runtime: runtimeOrDefault(runtimes, schedule.schedule),
        configured: form.isConfigured,
        connectionEnabled: form.enabled,
        role,
        toggle,
        retry,
      })),
    [provider, runtimes, form.isConfigured, form.enabled, role, toggle, retry],
  );

  const handleSheetChange = (open: boolean) => {
    if (!open) form.resetDraft();
    setConfigureOpen(open);
  };

  return (
    <>
      <Page.Section>
        <Page.Section.Title stage="preview">
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
            {coverageError ? (
              <Text muted role="alert">
                Coverage could not be loaded. It will retry automatically.
              </Text>
            ) : null}
            {coverage ? (
              sink ? (
                <SinkEvidenceHeadline
                  coverage={coverage}
                  providerName={provider.displayName}
                  configured={form.isConfigured}
                  enabled={form.enabled}
                />
              ) : (
                <CoverageHeadline coverage={coverage} />
              )
            ) : null}
            {coverage && coverage.totalDevices > 0 ? (
              <CoverageSummaryTiles coverage={coverage} />
            ) : null}
            {sink ? <FleetSourceBreakdown /> : null}
          </Stack>
        </Page.Section.Body>
      </Page.Section>

      <Page.Section>
        {/* Secondary section: suppress the area eyebrow (primary title above). */}
        <Page.Section.Title area="">
          {sink ? "Push schedules" : "Sync schedules"}
        </Page.Section.Title>
        <Page.Section.Description>
          {ROLE_COPY[role].scheduleBlurb}
        </Page.Section.Description>
        <Page.Section.Body>
          <DeviceIntegrationSchedulesTable rows={scheduleRows} role={role} />
        </Page.Section.Body>
      </Page.Section>

      {/* Managed devices are an INVENTORY concept — a sink has none of its own,
          so the table appears only on source pages. The sink's fleet is the
          org-wide coverage above, sourced from the inventory note. */}
      {sink ? null : (
        <Page.Section>
          <Page.Section.Title area="">Managed devices</Page.Section.Title>
          <Page.Section.Description>
            The device inventory synced from {provider.displayName}, with each
            device's agent coverage.
          </Page.Section.Description>
          <Page.Section.Body>
            <ManagedDeviceTable
              devices={devices}
              isLoading={devicesQuery.isLoading}
              isError={devicesQuery.isError}
              onRetry={() => void devicesQuery.refetch()}
              hasMore={devicesQuery.hasNextPage}
              onLoadMore={() => void devicesQuery.fetchNextPage()}
              isLoadingMore={devicesQuery.isFetchingNextPage}
            />
          </Page.Section.Body>
        </Page.Section>
      )}

      <DeviceIntegrationConfigureSheet
        provider={provider}
        form={form}
        open={configureOpen}
        onOpenChange={handleSheetChange}
      />
    </>
  );
}

// The headline number an admin acts on. The sentence follows the server's
// attestation field, which reports the strongest claim holding for EVERY
// active device — not merely the org's matching mode. That distinction
// matters: agent_active is reachable through the email fallback even under
// device-level matching, so a mixed response is reported as "user" and must
// not print the per-machine sentence. Floors so the headline never claims
// 100% while any device is uncovered.
function coverageHeadlineCopy(coverage: DeviceIntegrationCoverage): string {
  const noun = coverage.totalDevices === 1 ? "device" : "devices";
  if (coverage.attestation === "device") {
    return `of ${coverage.totalDevices} managed ${noun} are running the agent`;
  }
  return `of ${coverage.totalDevices} managed ${noun} have an assigned user with an active agent`;
}

function CoverageHeadline({
  coverage,
}: {
  coverage: DeviceIntegrationCoverage;
}) {
  if (coverage.totalDevices === 0) {
    return (
      <Text muted>
        No devices synced yet. Devices appear after the first successful
        inventory sync.
      </Text>
    );
  }
  const percent = Math.floor(
    (coverage.agentActive / coverage.totalDevices) * 100,
  );
  return (
    <Stack direction="horizontal" align="baseline" gap={2}>
      <Text variant="body" className="text-3xl font-semibold tabular-nums">
        {percent}%
      </Text>
      <Text muted>{coverageHeadlineCopy(coverage)}</Text>
    </Stack>
  );
}

// A sink republishes the org-wide fleet, so its headline states what it is
// publishing — not a coverage percentage of its own. The "published" claim is
// gated on the connection actually being live: a not-connected or paused sink
// is not sending anything, however many devices the fleet holds, so it reads
// as "ready to publish" rather than asserting evidence that isn't flowing.
function SinkEvidenceHeadline({
  coverage,
  providerName,
  configured,
  enabled,
}: {
  coverage: DeviceIntegrationCoverage;
  providerName: string;
  configured: boolean;
  enabled: boolean;
}) {
  // No fleet: the dependency on an inventory source dominates, whatever the
  // sink's own state.
  if (coverage.totalDevices === 0) {
    return (
      <Text muted>
        Connect a device inventory source (Jamf, Iru, or Intune) first — there
        is nothing to publish to {providerName} yet.
      </Text>
    );
  }

  const noun = coverage.totalDevices === 1 ? "device" : "devices";
  const count = (
    <Text variant="body" className="text-3xl font-semibold tabular-nums">
      {coverage.totalDevices}
    </Text>
  );

  if (!configured) {
    return (
      <Stack direction="horizontal" align="baseline" gap={2}>
        {count}
        <Text muted>
          managed {noun} ready to publish — connect {providerName} to start.
        </Text>
      </Stack>
    );
  }
  if (!enabled) {
    return (
      <Stack direction="horizontal" align="baseline" gap={2}>
        {count}
        <Text muted>
          managed {noun} ready to publish — enable the connection to resume
          pushing to {providerName}.
        </Text>
      </Stack>
    );
  }
  return (
    <Stack direction="horizontal" align="baseline" gap={2}>
      {count}
      <Text muted>
        managed {noun} published to {providerName} as coverage evidence
      </Text>
    </Stack>
  );
}

// Closes the loop on where a sink's fleet comes from — the recurring "why is
// this empty / where do these devices come from" question. Links back to the
// integrations list, where the inventory sources live.
function InventorySourceNote() {
  return (
    <Text muted small>
      Coverage is computed from your{" "}
      <Link
        to=".."
        relative="path"
        className="text-foreground underline underline-offset-2"
      >
        connected inventory sources
      </Link>
      . This destination republishes that fleet — it has no device list of its
      own.
    </Text>
  );
}

// Which inventory sources actually feed this sink's fleet, and how many devices
// each contributes. A sink republishes the union of every source's inventory,
// so the source→sink trace is otherwise invisible on the compliance side; this
// makes it explicit. Per-source counts come from the same coverage endpoint
// scoped to each source, and re-derive on toggle because the enable/disable
// mutation invalidates all coverage queries.
function FleetSourceBreakdown() {
  const client = useSdkClient();
  const { data } = useDeviceIntegrationProviders(undefined, undefined, {
    staleTime: 300_000,
  });
  const sources = useMemo(
    () =>
      (data?.providers ?? []).filter(
        (provider) => !isSink(provider) && isProviderVisible(provider),
      ),
    [data],
  );
  const coverageQueries = useQueries({
    queries: sources.map((provider) => ({
      ...buildDeviceIntegrationCoverageQuery(client, { provider: provider.id }),
      staleTime: COVERAGE_STALE_TIME,
    })),
  });
  const contributions = useMemo(
    () =>
      sources
        .map((provider, i) => ({
          provider,
          total: coverageQueries[i]?.data?.totalDevices ?? 0,
        }))
        .filter((entry) => entry.total > 0)
        .sort((a, b) => b.total - a.total),
    [sources, coverageQueries],
  );

  // No source is contributing yet — fall back to the plain dependency note,
  // which spells out the "connect a source first" path.
  if (contributions.length === 0) {
    return <InventorySourceNote />;
  }

  return (
    <Stack gap={2}>
      <Text
        variant="small"
        className="text-muted-foreground font-mono text-[10.5px] tracking-wider uppercase"
      >
        Fleet sourced from
      </Text>
      <div className="flex flex-wrap gap-2">
        {contributions.map(({ provider, total }) => (
          <Link
            key={provider.id}
            to={`../${provider.id}`}
            relative="path"
            className="border-border bg-muted/40 hover:bg-muted flex items-center gap-2 border px-3 py-1.5 text-sm transition-colors"
          >
            <span className="font-medium">{provider.displayName}</span>
            <span className="text-muted-foreground tabular-nums">
              {total} {total === 1 ? "device" : "devices"}
            </span>
          </Link>
        ))}
      </div>
      <Text muted small>
        This destination republishes that fleet — it has no device list of its
        own.
      </Text>
    </Stack>
  );
}
