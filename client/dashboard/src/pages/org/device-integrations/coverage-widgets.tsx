import { Page } from "@/components/page-layout";
import {
  defineFilters,
  type FilterValue,
  useFilterState,
} from "@/components/filters";
import { SkeletonTable } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { formatRelativeTime } from "@/lib/dates";
import type { DeviceIntegrationCoverage } from "@gram/client/models/components/deviceintegrationcoverage.js";
import type {
  CoverageBucket,
  ManagedDevice,
} from "@gram/client/models/components/manageddevice.js";
import {
  Badge,
  Button,
  type Column,
  Stack,
  Table,
} from "@speakeasy-api/moonshine";
import {
  CheckCircle2,
  CircleSlash,
  HelpCircle,
  MailX,
  MonitorX,
  TriangleAlert,
  UserX,
} from "lucide-react";
import { memo, useDeferredValue, useMemo, useState } from "react";
import { Link } from "react-router";

// Coverage joins the MDM-reported fleet against device agent heartbeats.
// Attestation is per assigned USER, not per device: an active heartbeat
// proves "this device's assigned user runs the agent somewhere", never
// "this device is monitored". Copy below must not overclaim.
type BucketDisplay = {
  label: string;
  variant: "destructive" | "neutral" | "success" | "warning";
  icon: React.ComponentType<{ className?: string }>;
  detail: string;
};

const FALLBACK_BUCKET: BucketDisplay = {
  label: "Unknown",
  variant: "neutral",
  icon: HelpCircle,
  detail: "Unrecognized coverage state.",
};

const BUCKETS: Record<CoverageBucket, BucketDisplay> = {
  agent_active: {
    label: "Agent active",
    variant: "success",
    icon: CheckCircle2,
    detail:
      "The assigned user's device agent reported a heartbeat within the active window.",
  },
  agent_stale: {
    label: "Agent stale",
    variant: "warning",
    icon: TriangleAlert,
    detail:
      "The assigned user's agent has gone quiet while the MDM still sees the device checking in — the drift case. The agent may be disabled or removed.",
  },
  no_agent: {
    label: "No agent",
    variant: "destructive",
    icon: CircleSlash,
    detail:
      "The assigned user has never reported a device agent heartbeat. This device's user is not running the agent.",
  },
  no_email: {
    label: "No email in MDM",
    variant: "neutral",
    icon: MailX,
    detail:
      "The MDM record has no assigned-user email, so agent coverage cannot be attested. Set the user's email on the device record in your MDM.",
  },
  unresolved_email: {
    label: "Unknown user",
    variant: "neutral",
    icon: HelpCircle,
    detail:
      "The MDM-assigned email does not match any member of this organization.",
  },
  missing: {
    label: "Missing from MDM",
    variant: "neutral",
    icon: MonitorX,
    detail:
      "The device stopped appearing in the MDM inventory. It may have been unenrolled or retired.",
  },
};

// Safe lookup: the SDK enum is closed today, but an unrecognized value from
// a newer server must degrade to a neutral badge, not a crash.
function bucketDisplay(bucket: CoverageBucket): BucketDisplay {
  return BUCKETS[bucket] ?? FALLBACK_BUCKET;
}

function CoverageBucketBadge({
  bucket,
}: {
  bucket: CoverageBucket;
}): JSX.Element {
  const display = bucketDisplay(bucket);
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
  const tiles: { key: string; display: BucketDisplay; count: number }[] = [
    {
      key: "agent_active",
      display: BUCKETS.agent_active,
      count: coverage.agentActive,
    },
    {
      key: "agent_stale",
      display: BUCKETS.agent_stale,
      count: coverage.agentStale,
    },
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
      display: UNMANAGED_USERS_TILE,
      count: coverage.unmanagedAgentUsers,
    },
  ];

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-7">
      {tiles.map(({ key, display, count }) => (
        <CoverageStatTile key={key} display={display} count={count} />
      ))}
    </div>
  );
}

const UNMANAGED_USERS_TILE: BucketDisplay = {
  label: "Agent users w/o device",
  variant: "neutral",
  icon: UserX,
  detail:
    "Users reporting device agent heartbeats whose email matches no managed device in the MDM inventory.",
};

function CoverageStatTile({
  display,
  count,
}: {
  display: BucketDisplay;
  count: number;
}) {
  return (
    <SimpleTooltip tooltip={display.detail}>
      <div className="border-border bg-card flex flex-col gap-1 rounded-lg border p-3">
        <Type variant="body" className="text-2xl font-semibold tabular-nums">
          {count}
        </Type>
        <Type muted small className="truncate">
          {display.label}
        </Type>
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
  const userHref = useEmployeeDetailHref();

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
      const matchesBucket =
        !selectedBuckets ||
        selectedBuckets.length === 0 ||
        selectedBuckets.includes(device.coverageBucket);
      return matchesSearch && matchesBucket;
    });
  }, [devices, deferredSearch, selectedBuckets]);

  const columns = useMemo(() => deviceColumns(userHref), [userHref]);

  if (isLoading) return <SkeletonTable />;
  if (isError) {
    return (
      <Stack gap={2} align="start">
        <Type muted>Could not load the device inventory.</Type>
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
          optionsById={{ coverage: BUCKET_OPTIONS }}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
        />
        <Page.Toolbar.Count>
          {/* The "+" keeps the count honest while unloaded pages remain. */}
          {filteredDevices.length}
          {hasMore ? "+" : ""} devices
        </Page.Toolbar.Count>
      </Page.Toolbar>
      <Table
        columns={columns}
        data={filteredDevices}
        rowKey={(row) => row.id}
        noResultsMessage={<Type muted>No matching devices</Type>}
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
          <Type muted small>
            Showing {devices.length} synced devices so far — search covers only
            loaded devices.
          </Type>
        </Stack>
      ) : null}
    </Stack>
  );
});

const BUCKET_OPTIONS = (Object.keys(BUCKETS) as CoverageBucket[]).map(
  (bucket) => ({
    label: bucketDisplay(bucket).label,
    value: bucket,
  }),
);

const DEVICE_FILTERS = defineFilters([
  { id: "coverage", label: "Coverage", kind: "multiselect", pinned: true },
]);

// Builds a link to the (project-scoped) Employee Detail page for a device's
// assigned user, when the device resolved to an org member. The employee
// pages live under a project, so the org's first project anchors the link.
function useEmployeeDetailHref(): (device: ManagedDevice) => string | null {
  const organization = useOrganization();
  const { orgSlug } = useSlugs();
  const projectSlug = organization.projects[0]?.slug;
  return useMemo(
    () => (device: ManagedDevice) => {
      if (!device.userId || !device.userEmail || !projectSlug) return null;
      return `/${orgSlug}/projects/${projectSlug}/employees/${encodeURIComponent(device.userEmail)}`;
    },
    [orgSlug, projectSlug],
  );
}

function deviceColumns(
  userHref: (device: ManagedDevice) => string | null,
): Column<ManagedDevice>[] {
  return [
    {
      key: "device",
      header: "Device",
      render: (device) => (
        <Stack gap={0.5} className="min-w-0">
          <Type variant="body" className="truncate font-medium">
            {device.hostname ?? device.externalId}
          </Type>
          <Type muted small className="truncate font-mono text-xs">
            {deviceSubtitle(device)}
          </Type>
        </Stack>
      ),
    },
    {
      key: "user",
      header: "Assigned user",
      render: (device) => (
        <AssignedUserCell device={device} href={userHref(device)} />
      ),
    },
    ...trailingDeviceColumns,
  ];
}

function AssignedUserCell({
  device,
  href,
}: {
  device: ManagedDevice;
  href: string | null;
}) {
  if (!device.userEmail) {
    return (
      <Type muted small className="truncate">
        —
      </Type>
    );
  }
  if (!href) {
    return (
      <Type muted small className="truncate">
        {device.userEmail}
      </Type>
    );
  }
  return (
    <Link
      to={href}
      className="text-muted-foreground hover:text-foreground block truncate text-sm underline-offset-2 hover:underline"
    >
      {device.userEmail}
    </Link>
  );
}

const trailingDeviceColumns: Column<ManagedDevice>[] = [
  {
    key: "agentLastSeen",
    header: "Agent last seen",
    width: "130px",
    render: (device) => (
      <Type muted small className="whitespace-nowrap">
        {formatRelativeTime(device.agentLastSeenAt ?? null) ?? "—"}
      </Type>
    ),
  },
  {
    key: "mdmCheckIn",
    header: "MDM check-in",
    width: "130px",
    render: (device) => (
      <Type muted small className="whitespace-nowrap">
        {formatRelativeTime(device.mdmLastCheckInAt ?? null) ?? "—"}
      </Type>
    ),
  },
  {
    key: "coverage",
    header: "Coverage",
    width: "170px",
    render: (device) => <CoverageBucketBadge bucket={device.coverageBucket} />,
  },
];

function deviceSubtitle(device: ManagedDevice): string {
  const parts = [
    device.serialNumber,
    [device.osName, device.osVersion].filter(Boolean).join(" "),
  ].filter((part) => part && part.length > 0);
  return parts.join(" · ") || device.externalId;
}
