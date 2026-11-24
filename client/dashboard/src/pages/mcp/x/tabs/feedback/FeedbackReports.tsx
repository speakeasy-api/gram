import { SplitRankedBarList } from "@/components/chart/SplitRankedBarList";
import {
  StatTile,
  StatTileGroup,
  StatTileSkeleton,
} from "@/components/chart/stat-tile";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
} from "@/components/filters";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { SkeletonTable } from "@/components/ui/SkeletonTable";
import { Stack } from "@/components/ui/Stack";
import { Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { McpFeedbackCounts } from "@gram/client/models/components/mcpfeedbackcounts.js";
import type { McpFeedbackToolCount } from "@gram/client/models/components/mcpfeedbacktoolcount.js";
import { useMcpFeedbackInfinite } from "@gram/client/react-query/mcpFeedback.js";
import { keepPreviousData } from "@tanstack/react-query";
import { useMemo, useRef, type ReactNode } from "react";
import { FeedbackReportDetails } from "./FeedbackReportDetails";
import { feedbackReportColumns } from "./feedbackReportColumns";
import { FEEDBACK_SEVERITIES, feedbackSeverity } from "./feedbackSeverity";

// How many reports each request for more brings back. A report is a few lines
// of prose, so a page is about what someone reads before narrowing the list.
const PAGE_SIZE = 25;

// How many tools the ranking shows. It sits above the reports, so it stays
// short enough to leave them close; the tool filter offers the rest.
const RANKING_SIZE = 5;

const FEEDBACK_FILTERS = defineFilters([
  { id: "severity", label: "Severity", kind: "select", pinned: true },
  { id: "tool", label: "Tool", kind: "select", pinned: true },
]);

const SEVERITY_OPTIONS = FEEDBACK_SEVERITIES.map(
  ({ value, label, meaning }) => ({ value, label, description: meaning }),
);

// The table's column tracks, so its skeleton holds the same shape: the
// expand control's track the table adds for expandable rows, then the columns.
const EXPAND_COLUMN_WIDTH = "64px" as const;
const COLUMN_WIDTHS = [
  EXPAND_COLUMN_WIDTH,
  ...feedbackReportColumns(() => {}).map((column) => column.width),
];

/**
 * What agents reported about this server: how much and how bad over the last
 * 30 days, which tools it was about, and every report, newest first.
 */
export function FeedbackReports({
  mcpServerId,
  feedbackIsOn,
  sendElsewhere,
}: {
  mcpServerId: string;
  /** With feedback off, a server with no reports has nothing to show here. */
  feedbackIsOn: boolean;
  /** How to send reports on from here, for viewers who can set that up. */
  sendElsewhere?: ReactNode;
}): JSX.Element | null {
  const { values, setValue, clearValue, clearAll } =
    useFilterState(FEEDBACK_FILTERS);
  const severity = feedbackSeverity(values.severity ?? undefined)?.value;
  const toolName = values.tool ?? undefined;
  const filtered = severity !== undefined || toolName !== undefined;
  const reportList = useRef<HTMLDivElement>(null);

  const query = useMcpFeedbackInfinite(
    { mcpServerId, severity, toolName, limit: PAGE_SIZE },
    undefined,
    // Changing a filter keeps the rows on screen until the new ones arrive.
    { placeholderData: keepPreviousData, throwOnError: false },
  );
  const firstPage = query.data?.pages[0]?.result;
  const reports = useMemo(
    () => query.data?.pages.flatMap((page) => page.result.feedback) ?? [],
    [query.data],
  );
  const columns = useMemo(
    () => feedbackReportColumns((tool) => setValue("tool", tool)),
    [setValue],
  );
  const toolOptions = useMemo(
    () => toolFilterOptions(firstPage?.tools ?? [], toolName),
    [firstPage?.tools, toolName],
  );

  // With feedback off there may be nothing to show at all, so nothing is drawn
  // until the answer is in; a frame that then vanished would be worse.
  if (query.isPending && !feedbackIsOn) return null;
  if (!query.isPending && !firstPage) {
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load the reports"
        description="Nothing was changed. Try again in a moment."
        action={
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void query.refetch()}
          >
            <Button.Text>Retry</Button.Text>
          </Button>
        }
      />
    );
  }
  const nothingEverReported =
    firstPage !== undefined &&
    !filtered &&
    reports.length === 0 &&
    firstPage.counts.total === 0;
  if (nothingEverReported && !feedbackIsOn) return null;
  if (nothingEverReported) {
    return (
      <InlineEmptyState
        icon="inbox"
        heading="No reports yet"
        description="Reports appear here as agents send them. An agent decides for itself when something is worth reporting, so a quiet server may simply be working."
        action={
          sendElsewhere && (
            <Text small muted>
              {sendElsewhere}
            </Text>
          )
        }
      />
    );
  }

  return (
    <Stack gap={6}>
      <FeedbackSummary
        counts={firstPage?.counts}
        tools={firstPage?.tools}
        onSelectTool={(tool) => {
          setValue("tool", tool);
          // The ranking sits above the list it narrows, which may be off screen.
          reportList.current?.scrollIntoView({
            behavior: "smooth",
            block: "start",
          });
        }}
      />

      <div ref={reportList} className="scroll-mt-6">
        <Stack gap={3}>
          <Stack gap={1}>
            <Text variant="subheading">Agent Feedback</Text>
            <Text small muted>
              Newest first. Secrets and personal data were masked before each
              report was stored. {sendElsewhere}
            </Text>
          </Stack>
          <Page.Toolbar>
            <Page.Toolbar.Filters
              schema={FEEDBACK_FILTERS}
              values={values}
              onChange={setValue as (id: string, value: FilterValue) => void}
              onClear={clearValue as (id: string) => void}
              onClearAll={clearAll}
              optionsById={{ severity: SEVERITY_OPTIONS, tool: toolOptions }}
            />
            <Page.Toolbar.Refresh
              onRefresh={() => void query.refetch()}
              isRefreshing={query.isFetching && !query.isFetchingNextPage}
            />
          </Page.Toolbar>
          {firstPage ? (
            <Table
              columns={columns}
              data={reports}
              rowKey={(report) => report.id}
              renderExpandedContent={(report) => (
                <FeedbackReportDetails report={report} />
              )}
              hasMore={query.hasNextPage}
              onLoadMore={async () => {
                await query.fetchNextPage();
              }}
              noResultsMessage={
                <span className="flex flex-wrap items-center gap-3">
                  <Text small muted>
                    No reports match these filters.
                  </Text>
                  <Button variant="tertiary" size="sm" onClick={clearAll}>
                    <Button.Text>Clear filters</Button.Text>
                  </Button>
                </span>
              }
            />
          ) : (
            <SkeletonTable label="Loading reports" columns={COLUMN_WIDTHS} />
          )}
          {query.isFetchNextPageError && (
            <Text small>
              Couldn&apos;t load more reports. The ones already loaded are still
              shown.
            </Text>
          )}
        </Stack>
      </div>
    </Stack>
  );
}

/**
 * The tools the window's reports are about, plus the one the list is narrowed
 * to when it is not among them, so its chip can still name it.
 */
function toolFilterOptions(
  tools: McpFeedbackToolCount[],
  selected: string | undefined,
): { value: string; label: string }[] {
  const names = tools.map((tool) => tool.toolName);
  if (selected !== undefined && !names.includes(selected)) names.push(selected);
  return names.map((name) => ({ value: name, label: name }));
}

/**
 * The last 30 days in figures. Without `counts` it draws the same frame with
 * placeholders, so the reports below do not move when the figures arrive.
 */
function FeedbackSummary({
  counts,
  tools,
  onSelectTool,
}: {
  counts: McpFeedbackCounts | undefined;
  tools: McpFeedbackToolCount[] | undefined;
  onSelectTool: (tool: string) => void;
}): JSX.Element {
  return (
    <Stack gap={3}>
      {/* One window for every figure in this block, named once. */}
      <p className="text-eyebrow">Last 30 days</p>
      <StatTileGroup>
        {counts ? (
          <>
            <StatTile
              title="Reports"
              value={counts.total}
              format="number"
              tone="information"
              subtext={totalCaption(counts)}
            />
            <StatTile
              title="Blocked"
              value={counts.blocked}
              format="number"
              tone={counts.blocked > 0 ? "destructive" : "neutral"}
              subtext="Could not finish the task"
            />
            <StatTile
              title="Degraded"
              value={counts.degraded}
              format="number"
              tone={counts.degraded > 0 ? "warning" : "neutral"}
              subtext="Finished by a slower path"
            />
            <StatTile
              title="Cosmetic"
              value={counts.cosmetic}
              format="number"
              tone="neutral"
              subtext="Worked, but was confusing"
            />
          </>
        ) : (
          <>
            <StatTileSkeleton caption />
            <StatTileSkeleton caption />
            <StatTileSkeleton caption />
            <StatTileSkeleton caption />
          </>
        )}
      </StatTileGroup>
      {/* Reports that name no tool leave nothing to rank. */}
      {(tools === undefined || tools.length > 0) && (
        <div className="bg-card border p-5">
          <h3 className="text-eyebrow mb-3">Tools agents reported most</h3>
          {tools ? (
            // The red share of each bar is the reports that blocked an agent.
            <SplitRankedBarList
              failedLabel="blocked"
              failedTitle="blocked"
              items={tools.slice(0, RANKING_SIZE).map((tool) => ({
                key: tool.toolName,
                label: tool.toolName,
                value: tool.feedbackCount,
                failed: tool.blockedCount,
                onSelect: () => onSelectTool(tool.toolName),
              }))}
            />
          ) : (
            <RankingSkeleton />
          )}
        </div>
      )}
    </Stack>
  );
}

/**
 * The caption under the total. Severity is optional, so the three tiles beside
 * it can add up to less; when they do, this says where the rest went.
 */
function totalCaption(counts: McpFeedbackCounts): string {
  if (counts.unspecified === 0) return "Sent by agents on this server";
  return counts.unspecified === 1
    ? "1 of them gave no severity"
    : `${counts.unspecified.toLocaleString()} of them gave no severity`;
}

/** Rows the height of the ranking's, as many as a full ranking holds. */
function RankingSkeleton(): JSX.Element {
  return (
    <div className="space-y-2.5">
      {Array.from({ length: RANKING_SIZE }, (_, row) => (
        <Skeleton key={row} className="h-5 w-full" />
      ))}
    </div>
  );
}
