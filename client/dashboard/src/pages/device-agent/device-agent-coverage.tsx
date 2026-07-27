import { Page } from "@/components/page-layout";
import { MultiSelect } from "@/components/ui/multi-select";
import { SearchBar } from "@/components/ui/search-bar";
import { SkeletonTable } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { formatRelativeTime } from "@/lib/dates";
import type { DeviceIntegrationCoverage } from "@gram/client/models/components/deviceintegrationcoverage.js";
import type {
  CoverageBucket,
  ManagedDevice,
} from "@gram/client/models/components/manageddevice.js";
import { useDeviceIntegrationCoverage } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { useManagedDevices } from "@gram/client/react-query/managedDevices.js";
import { Badge, type Column, Stack, Table } from "@speakeasy-api/moonshine";
import {
  CheckCircle2,
  CircleSlash,
  HelpCircle,
  MailX,
  MonitorX,
  TriangleAlert,
} from "lucide-react";
import { useMemo, useState } from "react";

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

export function CoverageBucketBadge({
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

// Coverage section for the Device Agent page: bucket summary tiles plus the
// filterable managed-device list. Renders nothing until an MDM integration
// has synced at least one device, so the setup page stays clean.
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
    {
      throwOnError: false,
    },
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

function CoverageSummaryTiles({
  coverage,
}: {
  coverage: DeviceIntegrationCoverage;
}) {
  const tiles: { bucket: CoverageBucket; count: number }[] = [
    { bucket: "agent_active", count: coverage.agentActive },
    { bucket: "agent_stale", count: coverage.agentStale },
    { bucket: "no_agent", count: coverage.noAgent },
    { bucket: "no_email", count: coverage.noEmail },
    { bucket: "unresolved_email", count: coverage.unresolvedEmail },
    { bucket: "missing", count: coverage.missing },
  ];

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
      {tiles.map(({ bucket, count }) => (
        <CoverageStatTile key={bucket} bucket={bucket} count={count} />
      ))}
    </div>
  );
}

function CoverageStatTile({
  bucket,
  count,
}: {
  bucket: CoverageBucket;
  count: number;
}) {
  const display = bucketDisplay(bucket);
  return (
    <SimpleTooltip tooltip={display.detail}>
      <div className="border-border bg-card flex flex-col gap-1 rounded-lg border p-3">
        <Type variant="body" className="text-2xl font-semibold tabular-nums">
          {count}
        </Type>
        <Type muted small className="whitespace-nowrap">
          {display.label}
        </Type>
      </div>
    </SimpleTooltip>
  );
}

function ManagedDeviceTable({
  devices,
  isLoading,
}: {
  devices: ManagedDevice[];
  isLoading: boolean;
}) {
  const [search, setSearch] = useState("");
  const [selectedBuckets, setSelectedBuckets] = useState<string[]>([]);

  const filteredDevices = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase();
    return devices.filter((device) => {
      const matchesSearch =
        normalizedSearch.length === 0 ||
        [device.hostname, device.serialNumber, device.userEmail].some((value) =>
          value?.toLowerCase().includes(normalizedSearch),
        );
      const matchesBucket =
        selectedBuckets.length === 0 ||
        selectedBuckets.includes(device.coverageBucket);
      return matchesSearch && matchesBucket;
    });
  }, [devices, search, selectedBuckets]);

  const bucketOptions = useMemo(
    () =>
      (Object.keys(BUCKETS) as CoverageBucket[]).map((bucket) => ({
        label: bucketDisplay(bucket).label,
        value: bucket,
      })),
    [],
  );

  if (isLoading) return <SkeletonTable />;

  return (
    <Stack gap={3}>
      <Stack direction="horizontal" gap={2} className="h-fit">
        <SearchBar
          value={search}
          onChange={setSearch}
          placeholder="Search host, serial, or user"
          className="w-64"
        />
        <MultiSelect
          options={bucketOptions}
          defaultValue={selectedBuckets}
          onValueChange={setSelectedBuckets}
          placeholder="Filter by coverage"
          autoSize
        />
      </Stack>
      <Table
        columns={deviceColumns}
        data={filteredDevices}
        rowKey={(row) => row.id}
        noResultsMessage={<Type muted>No matching devices</Type>}
      />
    </Stack>
  );
}

const deviceColumns: Column<ManagedDevice>[] = [
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
      <Type muted small className="truncate">
        {device.userEmail ?? "—"}
      </Type>
    ),
  },
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
