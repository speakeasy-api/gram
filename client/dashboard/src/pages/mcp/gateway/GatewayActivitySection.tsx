import { RankedBarList } from "@/components/chart/RankedBarList";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { ToolCallsTimeSeriesChart } from "@/components/chart/ToolCallsTimeSeriesChart";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";
import { Page } from "@/components/page-layout";
import { Skeleton, SkeletonTable } from "@/components/ui/Skeleton";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { telemetryGetMetaMcpServerUsage } from "@gram/client/funcs/telemetryGetMetaMcpServerUsage";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import type { GetMetaMcpServerUsageResult } from "@gram/client/models/components/getmetamcpserverusageresult.js";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult.js";
import type { ObservabilitySummary } from "@gram/client/models/components/observabilitysummary.js";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import {
  funnelItems,
  memberUsageRows,
  type MemberUsageRow,
} from "./gatewayActivity";
import type { MemberRow } from "./memberRows";

function errorRate(summary: ObservabilitySummary): number {
  return summary.totalToolCalls > 0
    ? (summary.failedToolCalls / summary.totalToolCalls) * 100
    : 0;
}

const memberColumns: Column<MemberUsageRow>[] = [
  {
    key: "label",
    header: "Member",
    render: (row) => <Text className="truncate">{row.label}</Text>,
  },
  {
    key: "toolCalls",
    header: "Calls",
    width: "100px",
    render: (row) => <Text>{row.toolCalls}</Text>,
  },
  {
    key: "errorCount",
    header: "Errors",
    width: "100px",
    render: (row) => <Text>{row.errorCount}</Text>,
  },
  {
    key: "errorRate",
    header: "Error rate",
    width: "110px",
    render: (row) => <Text>{row.errorRate.toFixed(1)}%</Text>,
  },
  {
    key: "lastCalledAt",
    header: "Last call",
    width: "180px",
    render: (row) => (
      <Text muted>
        {row.lastCalledAt ? row.lastCalledAt.toLocaleString() : "—"}
      </Text>
    ),
  },
];

function LoadError({ what }: { what: string }): JSX.Element {
  return (
    <div className="flex flex-col items-center justify-center border p-8 text-center">
      <Text muted className="mb-1 block">
        Could not load {what}
      </Text>
      <Text muted small>
        Try refreshing the page or changing the time range.
      </Text>
    </div>
  );
}

// Gateway-attributed usage for the selected range: the shared overview
// metrics scoped to calls dispatched through this gateway, the discovery
// funnel, and the per-member breakdown. The two queries fail independently so
// one failing never hides what the other loaded.
export function GatewayActivitySection({
  metaMcpServerId,
  memberRows,
}: {
  metaMcpServerId: string;
  memberRows: MemberRow[];
}): JSX.Element {
  const client = useGramContext();
  const [expandedChart, setExpandedChart] = useState<string | null>(null);

  const {
    dateRange,
    customRange,
    customRangeLabel,
    from,
    to,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  } = useDateRangeFilter();
  const timeRangeMs = useMemo(() => to.getTime() - from.getTime(), [from, to]);
  const rangeKey = [metaMcpServerId, from.toISOString(), to.toISOString()];

  const overview = useLogsEnabledErrorCheck(
    useQuery<GetObservabilityOverviewResult>({
      queryKey: ["gateway-overview", ...rangeKey],
      queryFn: () =>
        unwrapAsync(
          telemetryGetObservabilityOverview(client, {
            getObservabilityOverviewPayload: {
              from,
              to,
              metaMcpServerId,
              includeTimeSeries: true,
            },
          }),
        ),
      placeholderData: keepPreviousData,
      throwOnError: false,
    }),
  );
  const usage = useLogsEnabledErrorCheck(
    useQuery<GetMetaMcpServerUsageResult>({
      queryKey: ["gateway-usage", ...rangeKey],
      queryFn: () =>
        unwrapAsync(
          telemetryGetMetaMcpServerUsage(client, {
            getMetaMcpServerUsagePayload: { metaMcpServerId, from, to },
          }),
        ),
      placeholderData: keepPreviousData,
      throwOnError: false,
    }),
  );

  const isLogsDisabled = overview.isLogsDisabled || usage.isLogsDisabled;
  // With keepPreviousData a failed refetch still has stale data to show, so
  // each block falls back to its error state only with nothing to render.
  const overviewFailed = overview.isError && !overview.data;
  const usageFailed = usage.isError && !usage.data;

  const summary = overview.data?.summary;
  const comparison = overview.data?.comparison;
  const timeSeries = useMemo(
    () => overview.data?.timeSeries ?? [],
    [overview.data],
  );
  const funnel = useMemo(
    () => (usage.data ? funnelItems(usage.data.funnel) : []),
    [usage.data],
  );
  const members = useMemo(
    () => memberUsageRows(usage.data?.members ?? [], memberRows),
    [usage.data, memberRows],
  );
  const hasDiscovery = funnel.some((item) => item.value > 0);

  return (
    <Page.Section>
      {/* Section heading under the Overview page title: no eyebrow, smaller
          serif, matching the Members section above. */}
      <Page.Section.Title area="" className="text-display-xs">
        Activity
      </Page.Section.Title>
      <Page.Section.Description>
        Calls dispatched through this gateway, the discovery steps agents took
        to reach them, and how the load spread across members.
      </Page.Section.Description>
      <Page.Section.CTA>
        <TimeRangePicker
          preset={customRange ? null : dateRange}
          customRange={customRange}
          customRangeLabel={customRangeLabel}
          onPresetChange={setDateRangeParam}
          onCustomRangeChange={setCustomRangeParam}
          onClearCustomRange={clearCustomRange}
        />
      </Page.Section.CTA>
      <Page.Section.Body>
        {isLogsDisabled ? (
          <div className="flex flex-col items-center justify-center border p-12 text-center">
            <Text muted className="mb-1 block">
              Observability is not enabled
            </Text>
            <Text muted small>
              Enable logs for this organization to see usage for this gateway.
            </Text>
          </div>
        ) : (
          <div className="@container flex flex-col gap-6">
            {overviewFailed ? (
              <LoadError what="usage metrics" />
            ) : (
              <>
                <StatTileGroup>
                  {overview.isLoading && !summary ? (
                    Array.from({ length: 4 }).map((_, i) => (
                      <Skeleton key={i} className="h-[116px] flex-1" />
                    ))
                  ) : (
                    <>
                      <StatTile
                        title="Tool calls"
                        value={summary?.totalToolCalls ?? 0}
                        tone="information"
                        previousValue={comparison?.totalToolCalls}
                        format="compact"
                        comparisonLabel="vs previous period"
                      />
                      <StatTile
                        title="Failed calls"
                        value={summary?.failedToolCalls ?? 0}
                        tone={
                          (summary?.failedToolCalls ?? 0) > 0
                            ? "destructive"
                            : "neutral"
                        }
                        previousValue={comparison?.failedToolCalls}
                        format="compact"
                        invertDelta
                        comparisonLabel="vs previous period"
                      />
                      <StatTile
                        title="Error rate"
                        value={summary ? errorRate(summary) : 0}
                        previousValue={
                          comparison ? errorRate(comparison) : undefined
                        }
                        format="percent"
                        invertDelta
                        thresholds={{ red: 10, amber: 5, inverted: true }}
                        comparisonLabel="vs previous period"
                      />
                      <StatTile
                        title="Avg latency"
                        value={summary?.avgLatencyMs ?? 0}
                        tone="information"
                        previousValue={comparison?.avgLatencyMs}
                        format="ms"
                        invertDelta
                        comparisonLabel="vs previous period"
                      />
                    </>
                  )}
                </StatTileGroup>

                <ToolCallsTimeSeriesChart
                  title="Tool calls over time"
                  chartId="gateway-overview-tool-calls"
                  timeSeries={timeSeries}
                  timeRangeMs={timeRangeMs}
                  expandedChart={expandedChart}
                  onExpand={setExpandedChart}
                />
              </>
            )}

            {usageFailed ? (
              <LoadError what="the discovery funnel and member breakdown" />
            ) : (
              <div className="grid grid-cols-1 gap-6 @3xl:grid-cols-2">
                <div className="border p-5">
                  <h3 className="text-eyebrow mb-3">Discovery funnel</h3>
                  {usage.isLoading && !usage.data ? (
                    <Skeleton className="h-32" />
                  ) : hasDiscovery ? (
                    <RankedBarList items={funnel} />
                  ) : (
                    <WidgetEmptyState message="No discovery calls in the selected range." />
                  )}
                </div>
                <div className="border p-5">
                  <h3 className="text-eyebrow mb-3">Calls by member</h3>
                  {usage.isLoading && !usage.data ? (
                    <SkeletonTable />
                  ) : (
                    <Table
                      columns={memberColumns}
                      data={members}
                      rowKey={(row) => row.mcpServerId}
                      noResultsMessage={
                        <WidgetEmptyState message="No member calls in the selected range." />
                      }
                    />
                  )}
                </div>
              </div>
            )}
          </div>
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}
