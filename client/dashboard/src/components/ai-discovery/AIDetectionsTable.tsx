import { AIDetectionUsersSheet } from "@/components/ai-discovery/AIDetectionUsersSheet";
import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import {
  defineFilters,
  type FilterValue,
  useFilterState,
} from "@/components/filters";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, type SortDescriptor, Table } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import { Category } from "@gram/client/models/operations/listaidetections.js";
import { useAiDetections } from "@gram/client/react-query/aiDetections.js";
import { useAudiences } from "@gram/client/react-query/audiences.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { ChevronRight } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { validate as isUuid } from "uuid";

const CATEGORY_OPTIONS = [
  { value: Category.Harness, label: "Harness" },
  { value: Category.LocalModel, label: "Local model" },
];

const DETECTION_FILTERS = defineFilters([
  { id: "category", label: "Category", kind: "select", pinned: true },
  { id: "team", label: "Team", kind: "select", pinned: true },
]);

// Directory-group audiences carry their SCIM group id inside the principal
// URN; the detections filter wants the bare id.
const DIRECTORY_GROUP_URN_PREFIX = "directory_group:";

function categoryLabel(category: string): string {
  switch (category) {
    case Category.Harness:
      return "Harness";
    case Category.LocalModel:
      return "Local model";
    default:
      return category;
  }
}

// The URL param is free text; only forward values the API understands.
function parseCategory(value: string | null): Category | undefined {
  return value === Category.Harness || value === Category.LocalModel
    ? value
    : undefined;
}

function parseDirectoryGroupId(value: string | null): string | undefined {
  return value !== null && isUuid(value) ? value : undefined;
}

function SignalsCell({ signals }: { signals: string[] }): JSX.Element {
  if (signals.length === 0) {
    return (
      <Text muted small>
        —
      </Text>
    );
  }

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
    sortable: true,
    sortValue: (detection) => detection.displayName.toLowerCase(),
    width: "1.6fr",
    render: (detection) => (
      <div className="space-y-0.5">
        <Text variant="small" className="font-medium">
          {detection.displayName}
        </Text>
        {detection.targetId !== detection.displayName && (
          <Text variant="small" className="text-muted-foreground text-xs">
            {detection.targetId}
          </Text>
        )}
      </div>
    ),
  },
  {
    key: "category",
    header: "Category",
    sortable: true,
    sortValue: (detection) => categoryLabel(detection.category),
    width: "0.9fr",
    render: (detection) => (
      <Text variant="small">{categoryLabel(detection.category)}</Text>
    ),
  },
  {
    key: "users",
    header: "Users",
    sortable: true,
    sortValue: (detection) => detection.userCount,
    width: "0.5fr",
    render: (detection) => <Text variant="small">{detection.userCount}</Text>,
  },
  {
    key: "devices",
    header: "Devices",
    sortable: true,
    sortValue: (detection) => detection.deviceCount,
    width: "0.5fr",
    render: (detection) => <Text variant="small">{detection.deviceCount}</Text>,
  },
  {
    key: "signals",
    header: "Signal",
    width: "0.9fr",
    render: (detection) => <SignalsCell signals={detection.signals} />,
  },
  {
    key: "firstSeen",
    header: "First seen",
    sortable: true,
    sortValue: (detection) => detection.firstSeen.getTime(),
    width: "0.7fr",
    render: (detection) => (
      <Text variant="small">{formatShortDate(detection.firstSeen)}</Text>
    ),
  },
  {
    key: "lastSeen",
    header: "Last seen",
    sortable: true,
    sortValue: (detection) => detection.lastSeen.getTime(),
    width: "0.7fr",
    render: (detection) => (
      <Text variant="small">{formatShortDate(detection.lastSeen)}</Text>
    ),
  },
  {
    key: "open",
    header: "",
    width: "48px",
    render: (detection) => (
      <ChevronRight
        aria-label={`View users attributed to ${detection.displayName}`}
        className="text-muted-foreground size-4"
      />
    ),
  },
];

function DeviceAgentSetupEmptyState(): JSX.Element {
  const orgRoutes = useOrgRoutes();

  return (
    <InlineEmptyState
      icon="laptop"
      heading="Set up the device agent"
      description="Shadow AI is powered by device-agent scans. Install the agent on managed devices to inventory the AI coding tools and local model runtimes in use across your organization."
      action={
        <Link to={orgRoutes.deviceAgent.href()} className="hover:no-underline">
          <Button variant="secondary" size="sm">
            <Button.Text>Set up device agent</Button.Text>
          </Button>
        </Link>
      }
    />
  );
}

function RetryEmptyState({
  heading,
  description,
  onRetry,
}: {
  heading: string;
  description: string;
  onRetry: () => void;
}): JSX.Element {
  return (
    <InlineEmptyState
      icon="triangle-alert"
      heading={heading}
      description={description}
      action={
        <Button variant="secondary" size="sm" onClick={onRetry}>
          <Button.Text>Try again</Button.Text>
        </Button>
      }
    />
  );
}

export function AIDetectionsTable(): JSX.Element {
  const organization = useOrganization();
  const [selectedDetection, setSelectedDetection] =
    useState<AIDetection | null>(null);
  const [sort, setSort] = useState<SortDescriptor | null>({
    id: "lastSeen",
    direction: "desc",
  });
  const { values, setValue, clearValue, clearAll } =
    useFilterState(DETECTION_FILTERS);

  const category = parseCategory(values.category);
  const directoryGroupId = parseDirectoryGroupId(values.team);
  const hasActiveFilters = category !== undefined || Boolean(directoryGroupId);

  const detectionsQuery = useAiDetections({
    ...(category ? { category } : {}),
    ...(directoryGroupId ? { directoryGroupId } : {}),
  });
  // Distinguishes "no device-agent activity yet" from "scanned clean" when
  // the org has zero detections.
  const productFeaturesQuery = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { staleTime: 30_000, throwOnError: false },
  );
  // Team filter options come from the SCIM directory groups already surfaced
  // as plugin audiences. Failure just leaves the filter without options.
  const audiencesQuery = useAudiences(undefined, undefined, {
    throwOnError: false,
  });

  const teamOptions = useMemo(
    () =>
      (audiencesQuery.data?.audiences ?? [])
        .filter(
          (audience) =>
            audience.kind === "directory_group" &&
            audience.principalUrn.startsWith(DIRECTORY_GROUP_URN_PREFIX),
        )
        .map((audience) => ({
          value: audience.principalUrn.slice(DIRECTORY_GROUP_URN_PREFIX.length),
          label: audience.displayName,
        })),
    [audiencesQuery.data?.audiences],
  );

  const detections = detectionsQuery.data?.detections ?? [];
  const sortedDetections = sortTableData(
    detections,
    COLUMNS,
    sort,
  ) as AIDetection[];

  if (detectionsQuery.isLoading) {
    return <SkeletonTable />;
  }

  if (detectionsQuery.error && detections.length === 0) {
    return (
      <RetryEmptyState
        heading="AI detections could not be loaded"
        description="Try again now or come back later."
        onRetry={() => void detectionsQuery.refetch()}
      />
    );
  }

  if (detections.length === 0 && !hasActiveFilters) {
    if (productFeaturesQuery.isLoading) {
      return <SkeletonTable />;
    }

    if (productFeaturesQuery.error) {
      return (
        <RetryEmptyState
          heading="Device-agent status could not be loaded"
          description="Try again before reviewing the AI discovery inventory."
          onRetry={() => void productFeaturesQuery.refetch()}
        />
      );
    }

    if (productFeaturesQuery.data?.deviceAgent === false) {
      return <DeviceAgentSetupEmptyState />;
    }

    return (
      <InlineEmptyState
        icon="shield-check"
        heading="No AI tools detected"
        description="Device-agent scans have not found AI coding tools or local model runtimes on enrolled devices."
      />
    );
  }

  return (
    <div className="flex min-h-0 shrink flex-col gap-4">
      <Page.Toolbar className="shrink-0">
        <Page.Toolbar.Filters
          schema={DETECTION_FILTERS}
          values={values}
          optionsById={{ category: CATEGORY_OPTIONS, team: teamOptions }}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
        />
        {detectionsQuery.error && (
          <Page.Toolbar.Actions>
            <Text small className="text-destructive">
              Refresh failed; showing the last loaded results.
            </Text>
          </Page.Toolbar.Actions>
        )}
        <Page.Toolbar.Refresh
          onRefresh={() => void detectionsQuery.refetch()}
          isRefreshing={detectionsQuery.isFetching}
        />
      </Page.Toolbar>
      <div className="overflow-x-auto">
        <Table
          columns={COLUMNS}
          data={sortedDetections}
          rowKey={(detection) => detection.targetId}
          sort={sort}
          onSortChange={setSort}
          onRowClick={setSelectedDetection}
          noResultsMessage="No detections match the current filters"
          className="min-w-[900px]"
        />
      </div>
      <div className="border-border flex items-center gap-2 border-x border-b px-4 py-3">
        <Icon
          name="mouse-pointer-click"
          className="text-muted-foreground size-4 shrink-0"
        />
        <Text small muted>
          Select a tool row to review its attributed users.
        </Text>
      </div>
      {selectedDetection && (
        <AIDetectionUsersSheet
          key={selectedDetection.targetId}
          tool={selectedDetection}
          teamOptions={teamOptions}
          initialDirectoryGroupId={directoryGroupId}
          onOpenChange={(open) => {
            if (!open) {
              setSelectedDetection(null);
            }
          }}
        />
      )}
    </div>
  );
}
