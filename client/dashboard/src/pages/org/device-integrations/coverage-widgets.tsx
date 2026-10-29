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
import type {
  CoverageBucket,
  ManagedDevice,
  PolicyState,
} from "@gram/client/models/components/manageddevice.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { type Column, Table } from "@/components/ui/Table";
import { cn } from "@/lib/utils";
import { memo, useDeferredValue, useMemo, useState } from "react";
import { useLocation, useSearchParams } from "react-router";
import {
  BUCKETS,
  type CoverageDisplay,
  coverageDisplay,
  deviceCoverageKey,
  LINKED_DEVICE_PARAM,
  type LinkedDeviceState,
  matchesCoverageFilter,
  POLICY_STATES,
  UNMANAGED_USERS,
} from "./coverage-states";
import { DeviceDetailSheet } from "./device-detail-sheet";

function CoverageBadge({ device }: { device: ManagedDevice }): JSX.Element {
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

export function CoverageSummaryTiles({
  coverage,
}: {
  coverage: DeviceIntegrationCoverage;
}): JSX.Element {
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
    { key: "no_agent", display: BUCKETS.no_agent, count: coverage.noAgent },
    { key: "no_email", display: BUCKETS.no_email, count: coverage.noEmail },
    {
      key: "unresolved_email",
      display: BUCKETS.unresolved_email,
      count: coverage.unresolvedEmail,
    },
    { key: "missing", display: BUCKETS.missing, count: coverage.missing },
    // Not a device bucket: agent USERS the MDM knows no device for — the
    // inverse gap the ticket's bucket list requires.
    {
      key: "unmanaged_agent_users",
      display: UNMANAGED_USERS,
      count: coverage.unmanagedAgentUsers,
    },
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
export const ManagedDeviceTable = memo(function ManagedDeviceTable({
  devices,
  isLoading,
  isError = false,
  onRetry,
  hasMore = false,
  onLoadMore,
  isLoadingMore = false,
}: {
  devices: ManagedDevice[];
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
  const { values, setValue, clearValue, clearAll } =
    useFilterState(DEVICE_FILTERS);
  // Keep the raw (possibly undefined) reference: defaulting to a fresh []
  // here would change the memo dependency every render.
  const selectedBuckets = values.coverage;

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
      return matchesSearch && matchesCoverageFilter(device, selectedBuckets);
    });
  }, [devices, deferredSearch, selectedBuckets]);

  const columns = useMemo(() => deviceColumns(), []);
  // The device outlives `open`, so the sheet keeps its content while it
  // slides out. A refreshed list replaces the clicked snapshot.
  const [selected, setSelected] = useState<ManagedDevice | null>(null);
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

  if (isLoading) return <SkeletonTable />;
  if (isError) {
    return (
      <Stack gap={2} align="start">
        <Text muted>Could not load the device inventory.</Text>
        {onRetry ? (
          <Button variant="secondary" size="sm" onClick={onRetry}>
            <Button.Text>Retry</Button.Text>
          </Button>
        ) : null}
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
          schema={DEVICE_FILTERS}
          values={values}
          optionsById={{ coverage: COVERAGE_OPTIONS }}
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
            Showing {devices.length} synced devices so far — search covers only
            loaded devices.
          </Text>
        </Stack>
      ) : null}
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
const DEVICE_FILTERS = defineFilters([
  { id: "coverage", label: "Coverage", kind: "multiselect", pinned: true },
]);

function deviceColumns(): Column<ManagedDevice>[] {
  return [
    {
      key: "device",
      header: "Device",
      render: (device) => (
        <Stack gap={0.5} className="min-w-0">
          <Text variant="body" className="truncate font-medium">
            {device.hostname ?? device.externalId}
          </Text>
          <Text muted small className="truncate font-mono text-xs">
            {deviceSubtitle(device)}
          </Text>
        </Stack>
      ),
    },
    {
      key: "user",
      header: "Assigned user",
      render: (device) => <AssignedUserCell device={device} />,
    },
    ...trailingDeviceColumns,
  ];
}

function AssignedUserCell({ device }: { device: ManagedDevice }) {
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

const trailingDeviceColumns: Column<ManagedDevice>[] = [
  {
    key: "agentLastSeen",
    header: "Agent last seen",
    width: "130px",
    render: (device) => (
      <Text muted small className="whitespace-nowrap">
        {formatRelativeTime(device.agentLastSeenAt ?? null) ?? "—"}
      </Text>
    ),
  },
  {
    key: "mdmCheckIn",
    header: "MDM check-in",
    width: "130px",
    render: (device) => (
      <Text muted small className="whitespace-nowrap">
        {formatRelativeTime(device.mdmLastCheckInAt ?? null) ?? "—"}
      </Text>
    ),
  },
  {
    key: "coverage",
    header: "Coverage",
    width: "220px",
    render: (device) => <CoverageBadge device={device} />,
  },
];

function deviceSubtitle(device: ManagedDevice): string {
  const parts = [
    device.serialNumber,
    [device.osName, device.osVersion].filter(Boolean).join(" "),
  ].filter((part) => part && part.length > 0);
  return parts.join(" · ") || device.externalId;
}
