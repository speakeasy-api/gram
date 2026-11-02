import { IdentityLink } from "@/components/identity-link";
import { isEmailAddress } from "@/lib/identity-urn";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { Page } from "@/components/page-layout";
import {
  defineFilters,
  type FilterValue,
  useFilterState,
} from "@/components/filters";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { formatRelativeTime } from "@/lib/dates";
import type { DeviceIntegrationCoverage } from "@gram/client/models/components/deviceintegrationcoverage.js";
import type { FleetCoverage } from "@gram/client/models/components/fleetcoverage.js";
import type {
  FleetDevice,
  CoverageBucket,
  PolicyState,
} from "@gram/client/models/components/fleetdevice.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { type Column, Table } from "@/components/ui/Table";
import { cn } from "@/lib/utils";
import { memo, useDeferredValue, useMemo, useState } from "react";
import { useLocation, useSearchParams } from "react-router";
import {
  AGENT_ONLY,
  BUCKETS,
  type CoverageDisplay,
  coverageDisplay,
  deviceCoverageKey,
  deviceName,
  LINKED_DEVICE_PARAM,
  type LinkedDeviceState,
  matchesCoverageFilter,
  matchesSourceFilter,
  osLabel,
  POLICY_STATES,
  SOURCE_OPTIONS,
  UNMANAGED_USERS,
} from "./coverage-states";
import { DeviceDetailSheet } from "./device-detail-sheet";

function CoverageBadge({ device }: { device: FleetDevice }): JSX.Element {
  const display = coverageDisplay(deviceCoverageKey(device));
  const Icon = display.icon;
  return (
    <SimpleTooltip tooltip={display.detail}>
      <Badge variant={display.variant} background className="shrink-0">
        <Badge.LeftIcon>
          <Icon className="h-3.5 w-3.5" />
        </Badge.LeftIcon>
        <Badge.Text>{display.label}</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}

// The MDM pages pass an inventory's coverage; the Devices tab passes the
// fleet's, which has no MDM gaps to show until an MDM is connected.
export function CoverageSummaryTiles({
  coverage,
}: {
  coverage: DeviceIntegrationCoverage | FleetCoverage;
}): JSX.Element {
  const fleet = "agentOnlyDevices" in coverage ? coverage : null;
  const inventory = "agentOnlyDevices" in coverage ? null : coverage;
  const showGaps = fleet ? fleet.mdmConnected : true;
  const tiles: { key: string; display: CoverageDisplay; count: number }[] = [
    // agent_active, split by whether each device enforces its policy. The
    // three problem states appear only when non-zero, so a healthy fleet
    // reads as one tile.
    {
      key: "attested",
      display: POLICY_STATES.attested,
      count: coverage.policyAttested,
    },
    ...nonZero("outdated", POLICY_STATES.outdated, coverage.policyOutdated),
    ...nonZero("halted", POLICY_STATES.halted, coverage.enforcementHalted),
    ...nonZero(
      "unreported",
      POLICY_STATES.unreported,
      coverage.policyUnreported,
    ),
    {
      key: "agent_stale",
      display: BUCKETS.agent_stale,
      count: coverage.agentStale,
    },
    // Only rendered when non-zero: user-level matching cannot produce this
    // bucket, so an always-present "0" tile would be dead weight for every
    // org that has not moved to device-level matching yet.
    ...nonZero(
      "agent_other_device",
      BUCKETS.agent_other_device,
      coverage.agentOtherDevice,
    ),
    // Without an MDM nothing lists the devices that lack the agent, so these
    // would be a row of zeros that reads as full coverage.
    ...(showGaps
      ? [
          {
            key: "no_agent",
            display: BUCKETS.no_agent,
            count: coverage.noAgent,
          },
          {
            key: "no_email",
            display: BUCKETS.no_email,
            count: coverage.noEmail,
          },
          {
            key: "unresolved_email",
            display: BUCKETS.unresolved_email,
            count: coverage.unresolvedEmail,
          },
          {
            key: "missing",
            display: BUCKETS.missing,
            count: coverage.missing,
          },
        ]
      : []),
    // The inverse gap. An inventory counts agent USERS it knows no device
    // for; the fleet lists those machines, so it counts them instead.
    ...(inventory
      ? [
          {
            key: "unmanaged_agent_users",
            display: UNMANAGED_USERS,
            count: inventory.unmanagedAgentUsers,
          },
        ]
      : fleet?.mdmConnected
        ? nonZero("agent_only", AGENT_ONLY, fleet.agentOnlyDevices)
        : []),
  ];

  return (
    <div
      className={cn(
        "grid grid-cols-2 gap-3 sm:grid-cols-4",
        // One row up to eight tiles; past that, two near-even rows.
        tiles.length <= 7
          ? "lg:grid-cols-7"
          : tiles.length === 8
            ? "lg:grid-cols-8"
            : tiles.length <= 10
              ? "lg:grid-cols-5"
              : "lg:grid-cols-6",
      )}
    >
      {tiles.map(({ key, display, count }) => (
        <CoverageStatTile key={key} display={display} count={count} />
      ))}
    </div>
  );
}

function nonZero(
  key: string,
  display: CoverageDisplay,
  count: number,
): { key: string; display: CoverageDisplay; count: number }[] {
  return count > 0 ? [{ key, display, count }] : [];
}

function CoverageStatTile({
  display,
  count,
}: {
  display: CoverageDisplay;
  count: number;
}) {
  return (
    <SimpleTooltip tooltip={display.detail}>
      <div className="border-border bg-card flex flex-col gap-1 border p-3">
        <Text variant="body" className="text-2xl font-semibold tabular-nums">
          {count}
        </Text>
        <Text muted small className="truncate">
          {display.label}
        </Text>
      </div>
    </SimpleTooltip>
  );
}

// Memoized so parent form/typing state changes (e.g. the configure sheet on
// the detail page) don't re-render up to 200 device rows per keystroke.
//
// "inventory" is one MDM's synced devices. "fleet" is the Devices tab: every
// device the org can see, where the MDM-only parts (check-in, source) only
// mean something once an MDM is connected.
export const DeviceTable = memo(function DeviceTable({
  devices,
  variant = "inventory",
  mdmConnected = true,
  isLoading,
  isError = false,
  onRetry,
  hasMore = false,
  onLoadMore,
  isLoadingMore = false,
}: {
  devices: FleetDevice[];
  variant?: "inventory" | "fleet";
  mdmConnected?: boolean;
  isLoading: boolean;
  // A failed inventory fetch must never masquerade as an empty fleet.
  isError?: boolean;
  onRetry?: () => void;
  hasMore?: boolean;
  onLoadMore?: () => void;
  isLoadingMore?: boolean;
}): JSX.Element {
  const [search, setSearch] = useState("");
  // Keep the input responsive; the table filter recomputation trails behind.
  const deferredSearch = useDeferredValue(search);
  const showSource = variant === "fleet" && mdmConnected;
  const filters = showSource ? FLEET_FILTERS : DEVICE_FILTERS;
  const { values, setValue, clearValue, clearAll } = useFilterState(filters);
  // Keep the raw (possibly undefined) references: defaulting to a fresh []
  // here would change the memo dependency every render.
  const selectedBuckets = values.coverage;
  const selectedSources = showSource
    ? (values as { source?: string[] }).source
    : undefined;

  const filteredDevices = useMemo(() => {
    const normalizedSearch = deferredSearch.trim().toLowerCase();
    return devices.filter((device) => {
      const matchesSearch =
        normalizedSearch.length === 0 ||
        [
          device.hostname,
          device.serialNumber,
          device.userEmail,
          device.externalId,
        ].some((value) => value?.toLowerCase().includes(normalizedSearch));
      return (
        matchesSearch &&
        matchesCoverageFilter(device, selectedBuckets) &&
        matchesSourceFilter(device, selectedSources)
      );
    });
  }, [devices, deferredSearch, selectedBuckets, selectedSources]);

  const columns = useMemo(
    () =>
      deviceColumns({
        userHeader: variant === "fleet" ? "User" : "Assigned user",
        showMdmCheckIn: mdmConnected,
        showSource,
      }),
    [variant, mdmConnected, showSource],
  );
  // The device outlives `open`, so the sheet keeps its content while it
  // slides out. A refreshed list replaces the clicked snapshot.
  const [selected, setSelected] = useState<FleetDevice | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const selectedDevice = selected
    ? (devices.find((device) => device.id === selected.id) ?? selected)
    : null;
  // A Command K result links here with ?device=<id> and carries the device
  // it matched, so the panel opens before that device's page has loaded.
  const [searchParams, setSearchParams] = useSearchParams();
  const location = useLocation();
  const linkedId = searchParams.get(LINKED_DEVICE_PARAM);
  const linkedSnapshot = (location.state as LinkedDeviceState | null)?.device;
  const linkedDevice = linkedId
    ? (devices.find((device) => device.id === linkedId) ??
      (linkedSnapshot?.id === linkedId ? linkedSnapshot : null))
    : null;
  const clearLinkedDevice = () => {
    if (!linkedId) return;
    setSearchParams(
      (params) => {
        params.delete(LINKED_DEVICE_PARAM);
        return params;
      },
      { replace: true },
    );
  };

  // Rendered in every state, so a Command K link opens its snapshot while
  // the inventory loads or after it fails.
  const detailSheet = (
    <DeviceDetailSheet
      device={linkedDevice ?? selectedDevice}
      open={linkedDevice ? true : sheetOpen}
      onOpenChange={(open) => {
        // Keep the linked device as the selection so the panel keeps its
        // content while it slides out.
        if (!open && linkedDevice) setSelected(linkedDevice);
        if (!open) clearLinkedDevice();
        setSheetOpen(open);
      }}
    />
  );

  if (isLoading) {
    return (
      <>
        <SkeletonTable />
        {detailSheet}
      </>
    );
  }
  if (isError) {
    return (
      <Stack gap={2} align="start">
        <Text muted>
          {variant === "fleet"
            ? "Could not load devices."
            : "Could not load the device inventory."}
        </Text>
        {onRetry ? (
          <Button variant="secondary" size="sm" onClick={onRetry}>
            <Button.Text>Retry</Button.Text>
          </Button>
        ) : null}
        {detailSheet}
      </Stack>
    );
  }

  return (
    <Stack gap={3}>
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={search}
          onChange={setSearch}
          placeholder="Search host, serial, or user"
          debounceMs={300}
        />
        <Page.Toolbar.Filters
          schema={filters}
          values={values}
          optionsById={{
            coverage: mdmConnected ? COVERAGE_OPTIONS : AGENT_COVERAGE_OPTIONS,
            source: SOURCE_OPTIONS,
          }}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
        />
      </Page.Toolbar>
      <Table
        columns={columns}
        data={filteredDevices}
        rowKey={(row) => row.id}
        onRowClick={(device) => {
          clearLinkedDevice();
          setSelected(device);
          setSheetOpen(true);
        }}
        noResultsMessage={<WidgetEmptyState message="No matching devices" />}
      />
      {hasMore ? (
        <Stack direction="horizontal" align="center" gap={3}>
          <Button
            variant="tertiary"
            size="sm"
            onClick={onLoadMore}
            disabled={isLoadingMore}
          >
            <Button.Text>
              {isLoadingMore ? "Loading…" : "Load more devices"}
            </Button.Text>
          </Button>
          <Text muted small>
            Showing {devices.length}{" "}
            {variant === "fleet" ? "devices" : "synced devices"} so far — search
            covers only loaded devices.
          </Text>
        </Stack>
      ) : null}
      {detailSheet}
    </Stack>
  );
});

// The policy states match what the Coverage column shows. agent_active stays
// as the whole bucket, for existing links and rows from a server that sends
// no policy state.
const COVERAGE_OPTIONS = [
  ...(Object.keys(POLICY_STATES) as PolicyState[]),
  ...(Object.keys(BUCKETS) as CoverageBucket[]),
].map((key) => ({
  label:
    key === "agent_active"
      ? "Agent active (any policy state)"
      : coverageDisplay(key).label,
  value: key,
}));
// Without an MDM every device is agent-only, which is only ever agent_active
// or agent_stale; the MDM gap buckets would match nothing.
const AGENT_COVERAGE_OPTIONS = COVERAGE_OPTIONS.filter(
  ({ value }) =>
    Object.hasOwn(POLICY_STATES, value) ||
    value === "agent_active" ||
    value === "agent_stale",
);
const DEVICE_FILTERS = defineFilters([
  { id: "coverage", label: "Coverage", kind: "multiselect", pinned: true },
]);
const FLEET_FILTERS = defineFilters([
  { id: "coverage", label: "Coverage", kind: "multiselect", pinned: true },
  { id: "source", label: "Source", kind: "multiselect", pinned: true },
]);

function deviceColumns({
  userHeader,
  showMdmCheckIn,
  showSource,
}: {
  userHeader: string;
  showMdmCheckIn: boolean;
  showSource: boolean;
}): Column<FleetDevice>[] {
  return [
    {
      key: "device",
      header: "Device",
      render: (device) => (
        <Stack gap={0.5} className="min-w-0">
          <Stack direction="horizontal" align="center" gap={2}>
            <Text variant="body" className="truncate font-medium">
              {deviceName(device)}
            </Text>
            {showSource && !device.inInventory ? (
              <SimpleTooltip tooltip={AGENT_ONLY.detail}>
                <Badge variant="neutral" className="shrink-0">
                  <Badge.Text>{AGENT_ONLY.label}</Badge.Text>
                </Badge>
              </SimpleTooltip>
            ) : null}
          </Stack>
          <Text muted small className="truncate font-mono text-xs">
            {deviceSubtitle(device)}
          </Text>
        </Stack>
      ),
    },
    {
      key: "user",
      header: userHeader,
      render: (device) => <AssignedUserCell device={device} />,
    },
    agentLastSeenColumn,
    ...(showMdmCheckIn ? [mdmCheckInColumn] : []),
    coverageColumn,
  ];
}

function AssignedUserCell({ device }: { device: FleetDevice }) {
  if (!device.userEmail) {
    return (
      <Text muted small className="truncate">
        —
      </Text>
    );
  }
  // The MDM's reported address is enough for the resolver, whether or not it
  // resolved to a member on the server side. What it reports is freeform
  // though, so a value that is not an address stays plain text rather than
  // becoming a URN the resolver rejects.
  return (
    <IdentityLink
      identifier={
        isEmailAddress(device.userEmail) ? { email: device.userEmail } : null
      }
      className="text-muted-foreground hover:text-foreground block truncate text-sm underline-offset-2"
    >
      {device.userEmail}
    </IdentityLink>
  );
}

const agentLastSeenColumn: Column<FleetDevice> = {
  key: "agentLastSeen",
  header: "Agent last seen",
  width: "130px",
  render: (device) => (
    <Text muted small className="whitespace-nowrap">
      {formatRelativeTime(device.agentLastSeenAt ?? null) ?? "—"}
    </Text>
  ),
};

const mdmCheckInColumn: Column<FleetDevice> = {
  key: "mdmCheckIn",
  header: "MDM check-in",
  width: "130px",
  render: (device) => (
    <Text muted small className="whitespace-nowrap">
      {formatRelativeTime(device.mdmLastCheckInAt ?? null) ?? "—"}
    </Text>
  ),
};

const coverageColumn: Column<FleetDevice> = {
  key: "coverage",
  header: "Coverage",
  width: "220px",
  render: (device) => <CoverageBadge device={device} />,
};

// An agent-only machine has no MDM OS, so it shows the agent's.
function deviceSubtitle(device: FleetDevice): string {
  const os =
    [device.osName, device.osVersion].filter(Boolean).join(" ") ||
    (device.agentState?.os ? osLabel(device.agentState.os) : "");
  const parts = [device.serialNumber, os].filter(
    (part) => part && part.length > 0,
  );
  return parts.join(" · ") || device.externalId || "";
}
