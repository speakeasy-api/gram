import { MetricCard } from "@/components/chart/MetricCard";
import { RankedBarList } from "@/components/chart/RankedBarList";
import { ToolCallsTimeSeriesChart } from "@/components/chart/ToolCallsTimeSeriesChart";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult.js";
import type { ObservabilitySummary } from "@gram/client/models/components/observabilitysummary.js";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import { Stack } from "@speakeasy-api/moonshine";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { PluginStatusBanner } from "./PluginStatusBanner";
import { TopUsersTable } from "./TopUsersTable";

// Both toolset-backed and remote-MCP-backed servers render through this same
// dashboard — the telemetry/plugin-membership backends already key off
// either id generically (see PluginServer.toolsetId/mcpServerId and
// GetObservabilityOverviewPayload.toolsetSlug's dual-purpose doc comment), so
// this ref is the minimal shared shape both variants can produce.
export type HostedServerRef =
  | { kind: "toolset"; id: string; slug: string; name: string }
  | { kind: "mcp-server"; id: string; slug: string; name: string };

function toolLabelFromUrn(urn: string): string {
  const parts = urn.split(":");
  return parts[parts.length - 1] || urn;
}

function errorRate(summary: ObservabilitySummary): number {
  return summary.totalToolCalls > 0
    ? (summary.failedToolCalls / summary.totalToolCalls) * 100
    : 0;
}

export function MCPOverviewTab({
  server,
}: {
  server: HostedServerRef;
}): React.JSX.Element {
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

  const { data, isLoading, isLogsDisabled } = useLogsEnabledErrorCheck(
    useQuery<GetObservabilityOverviewResult>({
      queryKey: [
        "mcp-detail-overview",
        server.slug,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: () =>
        unwrapAsync(
          telemetryGetObservabilityOverview(client, {
            getObservabilityOverviewPayload: {
              from,
              to,
              toolsetSlug: server.slug,
              includeTimeSeries: true,
            },
          }),
        ),
      placeholderData: keepPreviousData,
      throwOnError: false,
    }),
  );

  const summary = data?.summary;
  const comparison = data?.comparison;
  const timeSeries = useMemo(() => data?.timeSeries ?? [], [data]);

  const topByCount = useMemo(
    () =>
      (data?.topToolsByCount ?? []).map((tool) => ({
        key: tool.gramUrn,
        label: toolLabelFromUrn(tool.gramUrn),
        value: tool.callCount,
      })),
    [data],
  );

  const topByFailureRate = useMemo(
    () =>
      (data?.topToolsByFailureRate ?? [])
        .filter((tool) => tool.failureCount > 0)
        .map((tool) => ({
          key: tool.gramUrn,
          label: toolLabelFromUrn(tool.gramUrn),
          value: tool.failureRate * 100,
          valueLabel: `${(tool.failureRate * 100).toFixed(1)}%`,
        })),
    [data],
  );

  return (
    <Stack gap={6} className="mb-4">
      <PluginStatusBanner server={server} />

      <div className="flex justify-end">
        <TimeRangePicker
          preset={customRange ? null : dateRange}
          customRange={customRange}
          customRangeLabel={customRangeLabel}
          onPresetChange={setDateRangeParam}
          onCustomRangeChange={setCustomRangeParam}
          onClearCustomRange={clearCustomRange}
        />
      </div>

      {isLogsDisabled ? (
        <div className="flex flex-col items-center justify-center rounded-lg border p-12 text-center">
          <Type muted className="mb-1 block">
            Observability is not enabled
          </Type>
          <Type muted small>
            Enable logs for this organization to see usage for this MCP server.
          </Type>
        </div>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
            {isLoading && !summary ? (
              Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-[116px] w-full rounded-lg" />
              ))
            ) : (
              <>
                <MetricCard
                  title="Tool calls"
                  value={summary?.totalToolCalls ?? 0}
                  previousValue={comparison?.totalToolCalls}
                  format="compact"
                  comparisonLabel="vs previous period"
                />
                <MetricCard
                  title="Failed calls"
                  value={summary?.failedToolCalls ?? 0}
                  previousValue={comparison?.failedToolCalls}
                  format="compact"
                  invertDelta
                  comparisonLabel="vs previous period"
                />
                <MetricCard
                  title="Error rate"
                  value={summary ? errorRate(summary) : 0}
                  previousValue={comparison ? errorRate(comparison) : undefined}
                  format="percent"
                  invertDelta
                  thresholds={{ red: 10, amber: 5, inverted: true }}
                  comparisonLabel="vs previous period"
                />
                <MetricCard
                  title="Avg latency"
                  value={summary?.avgLatencyMs ?? 0}
                  previousValue={comparison?.avgLatencyMs}
                  format="ms"
                  invertDelta
                  comparisonLabel="vs previous period"
                />
              </>
            )}
          </div>

          <ToolCallsTimeSeriesChart
            title="Tool calls over time"
            chartId="mcp-detail-overview-tool-calls"
            timeSeries={timeSeries}
            timeRangeMs={timeRangeMs}
            expandedChart={expandedChart}
            onExpand={setExpandedChart}
          />

          <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
            <div className="rounded-lg border p-5">
              <Heading variant="h5" className="mb-3">
                Top tools by call count
              </Heading>
              {topByCount.length > 0 ? (
                <RankedBarList items={topByCount} />
              ) : (
                <WidgetEmptyState message="No tool calls in the selected range." />
              )}
            </div>
            <div className="rounded-lg border p-5">
              <Heading variant="h5" className="mb-3">
                Top tools by failure rate
              </Heading>
              {topByFailureRate.length > 0 ? (
                <RankedBarList items={topByFailureRate} />
              ) : (
                <WidgetEmptyState message="No failed tool calls in the selected range." />
              )}
            </div>
          </div>

          <TopUsersTable toolsetSlug={server.slug} from={from} to={to} />
        </>
      )}
    </Stack>
  );
}
