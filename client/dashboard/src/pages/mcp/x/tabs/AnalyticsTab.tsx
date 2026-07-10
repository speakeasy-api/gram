import { MetricCard } from "@/components/chart/MetricCard";
import { RankedBar } from "@/components/chart/RankedBar";
import { ToolCallsTimeSeriesChart } from "@/components/chart/ToolCallsTimeSeriesChart";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { Card } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { type DateRangePreset, getPresetRange } from "@gram-ai/elements";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { ObservabilitySummary } from "@gram/client/models/components/observabilitysummary.js";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

function toolLabelFromUrn(urn: string): string {
  const parts = urn.split(":");
  return parts[parts.length - 1] || urn;
}

function errorRate(summary: ObservabilitySummary): number {
  return summary.totalToolCalls > 0
    ? (summary.failedToolCalls / summary.totalToolCalls) * 100
    : 0;
}

export function AnalyticsTab({
  mcpServer,
}: {
  mcpServer: McpServer | undefined;
}): JSX.Element {
  const gramClient = useGramContext();
  const mcpServerId = mcpServer?.id ?? "";

  const [dateRange, setDateRange] = useState<DateRangePreset>("7d");
  const [customRange, setCustomRange] = useState<{
    from: Date;
    to: Date;
  } | null>(null);
  const [customRangeLabel, setCustomRangeLabel] = useState<string | null>(null);
  const [expandedChart, setExpandedChart] = useState<string | null>(null);

  const { from, to, timeRangeMs } = useMemo(() => {
    const range = customRange ?? getPresetRange(dateRange);
    return {
      from: range.from,
      to: range.to,
      timeRangeMs: range.to.getTime() - range.from.getTime(),
    };
  }, [customRange, dateRange]);

  const {
    data: telemetryData,
    isLoading: isLoadingTelemetry,
    isLogsDisabled,
  } = useLogsEnabledErrorCheck(
    useQuery<GetObservabilityOverviewResult>({
      // `mcp_server_id` scopes this to the fronting server, capturing both
      // remote-backed and toolset-backed activity under one id.
      queryKey: [
        "mcp-server-analytics",
        mcpServerId,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: () =>
        unwrapAsync(
          telemetryGetObservabilityOverview(gramClient, {
            getObservabilityOverviewPayload: {
              from,
              to,
              mcpServerId,
              includeTimeSeries: true,
            },
          }),
        ),
      enabled: mcpServerId !== "",
      placeholderData: keepPreviousData,
      throwOnError: false,
    }),
  );

  const summary = telemetryData?.summary;
  const comparison = telemetryData?.comparison;
  const timeSeries = useMemo(
    () => telemetryData?.timeSeries ?? [],
    [telemetryData],
  );

  const topByCount = useMemo(
    () =>
      (telemetryData?.topToolsByCount ?? []).map((tool) => ({
        key: tool.gramUrn,
        label: toolLabelFromUrn(tool.gramUrn),
        value: tool.callCount,
      })),
    [telemetryData],
  );

  const topByFailureRate = useMemo(
    () =>
      (telemetryData?.topToolsByFailureRate ?? [])
        .filter((tool) => tool.failureCount > 0)
        .map((tool) => ({
          key: tool.gramUrn,
          label: toolLabelFromUrn(tool.gramUrn),
          value: tool.failureRate * 100,
        })),
    [telemetryData],
  );

  return (
    <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 flex-col gap-1">
          <Heading variant="h4">Analytics</Heading>
          <Type muted small>
            Tool call activity for this MCP server, spanning both remote-backed
            and toolset-backed sources.
          </Type>
        </div>
        <TimeRangePicker
          preset={customRange ? null : dateRange}
          customRange={customRange}
          customRangeLabel={customRangeLabel}
          onPresetChange={(preset) => {
            setDateRange(preset);
            setCustomRange(null);
            setCustomRangeLabel(null);
          }}
          onCustomRangeChange={(rangeFrom, rangeTo, label) => {
            setCustomRange({ from: rangeFrom, to: rangeTo });
            setCustomRangeLabel(label ?? null);
          }}
          onClearCustomRange={() => {
            setCustomRange(null);
            setCustomRangeLabel(null);
          }}
          disabled={isLoadingTelemetry}
        />
      </div>

      {isLogsDisabled ? (
        <InlineEmptyState
          title="Observability is not enabled"
          description="Enable logs for this organization to see analytics for this MCP server."
        />
      ) : (
        <div className="flex flex-col gap-6">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {isLoadingTelemetry && !summary ? (
              Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-[116px] w-full" />
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
            chartId="mcp-server-tool-calls"
            timeSeries={timeSeries}
            timeRangeMs={timeRangeMs}
            expandedChart={expandedChart}
            onExpand={setExpandedChart}
          />

          <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
            <Card>
              <Heading variant="h5" className="mb-3">
                Top tools by call count
              </Heading>
              {topByCount.length > 0 ? (
                <RankedBar items={topByCount} />
              ) : (
                <Type muted small>
                  No tool calls in the selected range.
                </Type>
              )}
            </Card>
            <Card>
              <Heading variant="h5" className="mb-3">
                Top tools by failure rate
              </Heading>
              {topByFailureRate.length > 0 ? (
                <RankedBar
                  items={topByFailureRate}
                  formatValue={(value) => `${value.toFixed(1)}%`}
                />
              ) : (
                <Type muted small>
                  No failed tool calls in the selected range.
                </Type>
              )}
            </Card>
          </div>
        </div>
      )}
    </div>
  );
}
