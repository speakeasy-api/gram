import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { InlineEmptyState } from "@/components/inline-empty-state";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  ErrorAlert,
} from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { categoryLabel } from "@/pages/device-agent/ai-scan-target-draft";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import { useEmployeeAIDetections } from "@gram/client/react-query/employeeAIDetections.js";

function SignalBadges({ signals }: { signals: string[] }): JSX.Element {
  return (
    <div className="flex flex-wrap gap-1">
      {signals.includes("running") && (
        <Badge variant="information">
          <Badge.Text>Running</Badge.Text>
        </Badge>
      )}
      {signals.includes("installed") && (
        <Badge variant="neutral">
          <Badge.Text>Installed</Badge.Text>
        </Badge>
      )}
    </div>
  );
}

const COLUMNS: Column<AIDetection>[] = [
  {
    key: "tool",
    header: "Tool",
    width: "1.2fr",
    render: (detection) => (
      <div className="min-w-0">
        <Text small className="truncate font-medium">
          {detection.displayName}
        </Text>
        <Text small muted mono className="truncate">
          {detection.targetId}
        </Text>
      </div>
    ),
  },
  {
    key: "category",
    header: "Category",
    width: "0.75fr",
    // categoryLabel is the scan-target editor's own label table, so a
    // category added there is rendered here without a second edit. The
    // ternary this replaced showed every assistant detection as "Harness".
    render: (detection) => (
      <Text small>{categoryLabel(detection.category)}</Text>
    ),
  },
  {
    key: "devices",
    header: "Devices",
    width: "0.55fr",
    render: (detection) => (
      <Text small>
        {detection.deviceCount}{" "}
        {detection.deviceCount === 1 ? "device" : "devices"}
      </Text>
    ),
  },
  {
    key: "signals",
    header: "Signals",
    width: "0.9fr",
    render: (detection) => <SignalBadges signals={detection.signals} />,
  },
  {
    key: "versions",
    header: "Versions",
    width: "1fr",
    render: (detection) => (
      <Text
        small
        mono
        className="truncate"
        title={detection.versions.join(", ")}
      >
        {detection.versions.length > 0 ? detection.versions.join(" · ") : "—"}
      </Text>
    ),
  },
  {
    key: "firstSeen",
    header: "First seen",
    width: "0.85fr",
    render: (detection) => (
      <Text small className="whitespace-nowrap">
        {formatShortDate(detection.firstSeen)}
      </Text>
    ),
  },
  {
    key: "lastSeen",
    header: "Last seen",
    width: "0.85fr",
    render: (detection) => (
      <Text small className="whitespace-nowrap">
        {formatShortDate(detection.lastSeen)}
      </Text>
    ),
  },
];

export function EmployeeShadowAISection({
  userEmail,
}: {
  userEmail: string | null;
}): JSX.Element {
  const detectionsQuery = useEmployeeAIDetections(
    { userEmail: userEmail ?? "" },
    undefined,
    {
      enabled: Boolean(userEmail),
      throwOnError: false,
    },
  );
  const detections = detectionsQuery.data?.detections ?? [];
  const scanDisabledDevices = detectionsQuery.data?.scanDisabledDevices ?? 0;

  let content: JSX.Element;
  if (!userEmail) {
    content = (
      <InlineEmptyState
        icon="user-round-search"
        heading="Shadow AI unavailable"
        description="This enrollment does not have a canonical email identity for matching device detections."
        orientation="horizontal"
      />
    );
  } else if (detectionsQuery.isPending) {
    content = <SkeletonTable />;
  } else if (detectionsQuery.isError) {
    content = (
      <ErrorAlert
        title="Unable to load Shadow AI detections"
        error={detectionsQuery.error}
      />
    );
  } else if (detections.length === 0) {
    content = (
      <InlineEmptyState
        icon="radar"
        heading="No detected AI tools"
        description="No AI harnesses or open models have been detected for this identity."
        orientation="horizontal"
      />
    );
  } else {
    content = (
      <div className="overflow-x-auto">
        <Table
          columns={COLUMNS}
          data={detections}
          rowKey={(detection) => detection.targetId}
          className="min-w-[820px]"
        />
      </div>
    );
  }

  return (
    <section className="bg-card border-border border p-5">
      <div className="mb-4">
        <h2 className="text-eyebrow">Shadow AI</h2>
        <p className="text-muted-foreground mt-1 text-sm">
          Organization-wide device-agent detections attributed to this identity.
        </p>
      </div>

      {scanDisabledDevices > 0 && (
        <Alert variant="warning" className="mb-4">
          <AlertTitle>Scan disabled</AlertTitle>
          <AlertDescription>
            {scanDisabledDevices === 1
              ? "Shadow AI scanning is turned off on one of this person's devices."
              : `Shadow AI scanning is turned off on ${scanDisabledDevices} of this person's devices.`}{" "}
            Results below may be out of date.
          </AlertDescription>
        </Alert>
      )}

      {content}
    </section>
  );
}
