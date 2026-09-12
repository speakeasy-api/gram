import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import {
  defineFilters,
  type FilterValue,
  useFilterState,
} from "@/components/filters";
import { Page } from "@/components/page-layout";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { AIToolIcon } from "@/components/ai-tools/AIToolIcon";
import { Badge } from "@/components/ui/Badge";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, type SortDescriptor, Table } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { Text } from "@/components/ui/Text";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import { useAiDetections } from "@gram/client/react-query/aiDetections.js";
import { useMemo, useState } from "react";
import { AIToolDecisionSheet } from "./AIToolDecisionSheet";

const STATUS_FILTER_OPTIONS = [
  { value: "unreviewed", label: "Unreviewed" },
  { value: "allowed", label: "Allowed" },
  { value: "blocked", label: "Blocked" },
];

// What each tab calls the things in it, for the empty state and the search
// placeholder.
const TAB_NOUNS: Record<string, string> = {
  harness: "harnesses",
  assistant: "assistants",
  local_model: "open models",
};

const TOOL_FILTERS = defineFilters([
  { id: "status", label: "Status", kind: "select" },
]);

// Status ordering puts the rows an admin still has to act on first.
const STATUS_RANK: Record<string, number> = {
  unreviewed: 0,
  blocked: 1,
  allowed: 2,
};

function statusBadgeVariant(
  state: string,
): "warning" | "destructive" | "success" {
  switch (state) {
    case "blocked":
      return "destructive";
    case "allowed":
      return "success";
    default:
      return "warning";
  }
}

function statusLabel(state: string): string {
  switch (state) {
    case "blocked":
      return "Blocked";
    case "allowed":
      return "Allowed";
    default:
      return "Unreviewed";
  }
}

// Three states, no fourth. A tool Gram cannot recognise at the gateway reads
// unreviewed, and the server already resolves it that way: blocking is
// CIMD-only, so a tool publishing no document cannot be refused, and a cell
// that said "blocked" about one would be untrue in the one place an admin
// looks to check.
function AIToolStatusCell({ detection }: { detection: AIDetection }) {
  const state = detection.access.state;
  return (
    <Badge variant={statusBadgeVariant(state)}>
      <Badge.Text>{statusLabel(state)}</Badge.Text>
    </Badge>
  );
}

function AIToolsEmptyState({ noun }: { noun: string }) {
  return (
    <div className="bg-background flex min-h-32 flex-col items-center justify-center gap-1 px-4 py-8 text-center">
      <Text variant="body" className="font-medium">
        No {noun} detected yet
      </Text>
      <Text muted small className="max-w-md">
        Tools appear here once enrolled devices report a scan. Check back after
        the device agent’s first run.
      </Text>
    </div>
  );
}

export function AIToolsTable({
  category,
  canDecide,
}: {
  // Which tab this is. Pushed to the server so the read is narrowed there
  // rather than fetching the whole inventory and hiding half of it.
  category: "harness" | "assistant" | "local_model";
  // True for organization admins. A project viewer reads the inventory and
  // cannot change a decision, because a block reaches every project's
  // gateway and gives that tool's users an unrecoverable OAuth error.
  canDecide: boolean;
}): JSX.Element {
  const detectionsQuery = useAiDetections({ category });
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<SortDescriptor | null>({
    id: "status",
    direction: "asc",
  });
  const [decideTarget, setDecideTarget] = useState<AIDetection | null>(null);
  const { values, setValue, clearValue, clearAll } =
    useFilterState(TOOL_FILTERS);

  const columns: Column<AIDetection>[] = useMemo(() => {
    const base: Column<AIDetection>[] = [
      {
        key: "tool",
        header: "Tool",
        sortable: true,
        sortValue: (detection) => detection.displayName.trim().toLowerCase(),
        width: "1.6fr",
        render: (detection) => (
          <div className="flex min-w-0 items-center gap-2">
            <AIToolIcon
              targetId={detection.targetId}
              displayName={detection.displayName}
              className="size-4 shrink-0"
            />
            <Text variant="small" className="truncate font-medium">
              {detection.displayName}
            </Text>
          </div>
        ),
      },
      {
        key: "status",
        header: "Status",
        sortable: true,
        sortValue: (detection) => STATUS_RANK[detection.access.state] ?? 3,
        width: "0.8fr",
        render: (detection) => <AIToolStatusCell detection={detection} />,
      },
    ];

    // The counts are attribution and the server omits them for project
    // viewers. Dropping the columns rather than rendering empty cells keeps
    // the table honest about what this scope can answer.
    if (canDecide) {
      base.push(
        {
          key: "users",
          header: "Users",
          sortable: true,
          sortValue: (detection) => detection.userCount ?? 0,
          width: "0.4fr",
          render: (detection) => (
            <Text variant="small">{detection.userCount ?? "—"}</Text>
          ),
        },
        {
          key: "devices",
          header: "Devices",
          sortable: true,
          sortValue: (detection) => detection.deviceCount ?? 0,
          width: "0.4fr",
          render: (detection) => (
            <Text variant="small">{detection.deviceCount ?? "—"}</Text>
          ),
        },
      );
    }

    base.push(
      {
        key: "signals",
        header: "Signals",
        sortable: false,
        width: "0.8fr",
        render: (detection) => (
          <Text variant="small">{detection.signals.join(", ")}</Text>
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
    );

    return base;
  }, [canDecide]);

  const detections = detectionsQuery.data?.detections ?? [];
  const normalizedSearch = search.trim().toLowerCase();
  const statusFilter = values.status ?? undefined;
  const filtered = useMemo(() => {
    return detections.filter((detection: AIDetection) => {
      if (statusFilter && detection.access.state !== statusFilter) return false;
      if (normalizedSearch.length === 0) return true;
      return [detection.displayName, detection.targetId].some((value) =>
        value.toLowerCase().includes(normalizedSearch),
      );
    });
  }, [detections, normalizedSearch, statusFilter]);
  const sorted = sortTableData(filtered, columns, sort) as AIDetection[];

  if (detectionsQuery.isLoading) {
    return <SkeletonTable />;
  }

  if (detectionsQuery.error) {
    return (
      <div className="bg-background flex min-h-32 flex-col items-center justify-center gap-1 px-4 py-8 text-center">
        <Text variant="body" className="font-medium">
          Inventory could not be loaded
        </Text>
        <Text muted small className="max-w-md">
          Refresh the page or try again later.
        </Text>
      </div>
    );
  }

  const noun = TAB_NOUNS[category] ?? "tools";
  if (detections.length === 0) {
    return <AIToolsEmptyState noun={noun} />;
  }

  const noResultsMessage =
    normalizedSearch.length > 0
      ? `No ${noun} matching “${search.trim()}”`
      : undefined;

  return (
    <div className="flex min-h-0 shrink flex-col gap-4 overflow-hidden">
      <AIToolDecisionSheet
        detection={decideTarget}
        open={decideTarget !== null}
        onOpenChange={(open) => {
          if (!open) setDecideTarget(null);
        }}
      />
      <Page.Toolbar className="shrink-0">
        <Page.Toolbar.Search
          onChange={setSearch}
          placeholder={`Search ${noun}...`}
          value={search}
        />
        <Page.Toolbar.Filters
          schema={TOOL_FILTERS}
          values={values}
          optionsById={{ status: STATUS_FILTER_OPTIONS }}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
        />
      </Page.Toolbar>
      <Table
        columns={columns}
        className="min-h-0 flex-1 grid-rows-[auto_minmax(0,1fr)] overflow-x-auto overflow-y-hidden"
      >
        <Table.Header columns={columns} sort={sort} onSortChange={setSort} />
        <Table.Body
          columns={columns}
          data={sorted}
          noResultsMessage={noResultsMessage}
          onRowClick={canDecide ? (row) => setDecideTarget(row) : undefined}
          rowKey={(row) => row.targetId}
          className="min-h-0 content-start overflow-y-auto"
          renderRow={(row, rowElement) =>
            canDecide ? (
              <TableRowContextMenu
                key={row.targetId}
                actions={[
                  {
                    label: "Decide access",
                    onClick: () => setDecideTarget(row),
                  },
                ]}
              >
                {rowElement}
              </TableRowContextMenu>
            ) : (
              rowElement
            )
          }
        />
      </Table>
    </div>
  );
}
