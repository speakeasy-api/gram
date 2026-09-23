import { InlineEmptyState } from "@/components/inline-empty-state";
import { DETECTION_EVIDENCE_COLUMNS } from "@/components/shadow-ai/detectionColumns";
import { ErrorAlert } from "@/components/ui/Alert";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { categoryLabel } from "@/pages/device-agent/ai-scan-target-draft";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import { useEmployeeAIDetections } from "@gram/client/react-query/employeeAIDetections.js";

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
  // Devices, signals, versions and first and last seen: shared with the
  // Shadow AI tool page, which shows the same evidence per person for one
  // tool, so the two tables never drift apart.
  ...DETECTION_EVIDENCE_COLUMNS,
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

      {content}
    </section>
  );
}
