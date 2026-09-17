import { RankedBarList } from "@/components/chart/RankedBarList";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { StatRow, type StatRowMetric } from "@/components/stat-row";
import { Card } from "@/components/ui/Card";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult.js";
import type { ToolMetric } from "@gram/client/models/components/toolmetric.js";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import {
  computeTelemetrySummary,
  formatLatency,
  selectSourceToolMetrics,
  toolNameFromUrn,
  type SourceTelemetrySummary,
} from "./sourceTelemetrySummary";

const WINDOW_DAYS = 7;
const TOP_TOOLS = 10;

// Only a rate that crosses this reads red; under it, amber says "worth a
// look" without alarming.
const ERROR_RATE_ALARM_PERCENT = 5;

function errorRateTone(errorRate: number): StatRowMetric["tone"] {
  if (errorRate === 0) return "neutral";
  if (errorRate > ERROR_RATE_ALARM_PERCENT) return "destructive";
  return "warning";
}

function summaryMetrics(summary: SourceTelemetrySummary): StatRowMetric[] {
  return [
    {
      key: "calls",
      label: "Calls",
      value: summary.totalCalls.toLocaleString(),
      tone: "information",
    },
    {
      key: "failures",
      label: "Failures",
      value: summary.totalFailures.toLocaleString(),
      tone: summary.totalFailures > 0 ? "destructive" : "neutral",
    },
    {
      key: "latency",
      label: "Avg latency",
      value: formatLatency(summary.avgLatencyMs),
      tone: "neutral",
    },
    {
      key: "error-rate",
      label: "Error rate",
      value: `${summary.errorRate.toFixed(1)}%`,
      tone: errorRateTone(summary.errorRate),
    },
  ];
}

/**
 * Seven days of calls to the tools generated from one source.
 *
 * Reads the project's pre-aggregated overview and keeps the rows for this
 * source's tools: the summary endpoint is the fast path, and a per-source
 * scan of raw logs would not be.
 */
export function SourceActivityPanel({
  sourceKey,
  toolUrns,
  isToolsLoading,
}: {
  /** Keys the query, so two sources never share a cache entry. */
  sourceKey: string;
  toolUrns: string[];
  /** True while the tool list is still loading, so an empty URN list does not
   * read as "no activity" before the tools are known. */
  isToolsLoading: boolean;
}): JSX.Element {
  const client = useGramContext();
  // Fixed at mount: a window that moved with every render would re-key the
  // query each time.
  const { from, to } = useMemo(() => {
    const end = new Date();
    const start = new Date(end);
    start.setDate(start.getDate() - WINDOW_DAYS);
    return { from: start, to: end };
  }, []);

  const { data, isLoading, isError, isLogsDisabled } = useLogsEnabledErrorCheck(
    useQuery<GetObservabilityOverviewResult>({
      queryKey: ["source-activity", sourceKey, from.toISOString()],
      queryFn: () =>
        unwrapAsync(
          telemetryGetObservabilityOverview(client, {
            getObservabilityOverviewPayload: {
              from,
              to,
              includeTimeSeries: false,
            },
          }),
        ),
      enabled: toolUrns.length > 0,
      throwOnError: false,
    }),
  );

  const metrics = useMemo(
    () => selectSourceToolMetrics(data?.topToolsByCount ?? [], toolUrns),
    [data, toolUrns],
  );
  const summary = useMemo(() => computeTelemetrySummary(metrics), [metrics]);

  return (
    <Card.Dashboard
      title="Activity"
      tooltip="Calls to the tools generated from this source, across every MCP server that carries them."
      action={
        <Text muted className="text-xs">
          Last {WINDOW_DAYS} days
        </Text>
      }
    >
      <SourceActivityBody
        isLoading={isToolsLoading || (toolUrns.length > 0 && isLoading)}
        isError={isError && !isLogsDisabled}
        isLogsDisabled={isLogsDisabled}
        metrics={metrics}
        summary={summary}
      />
    </Card.Dashboard>
  );
}

function SourceActivityBody({
  isLoading,
  isError,
  isLogsDisabled,
  metrics,
  summary,
}: {
  isLoading: boolean;
  isError: boolean;
  isLogsDisabled: boolean;
  metrics: ToolMetric[];
  summary: SourceTelemetrySummary | null;
}): JSX.Element {
  if (isLoading) {
    return (
      <div className="flex flex-col gap-6">
        <Skeleton className="h-[136px]" />
        <Skeleton className="h-40" />
      </div>
    );
  }
  if (isLogsDisabled) {
    return (
      <WidgetEmptyState message="Enable logging for this organization to see how this source's tools are used." />
    );
  }
  if (isError) {
    return (
      <WidgetEmptyState message="Couldn't load activity for this source. Reload to try again." />
    );
  }
  if (!summary) {
    return (
      <WidgetEmptyState message="No invocation data yet. Activity appears once tools from this source are called through an MCP server." />
    );
  }

  const topTools = metrics.slice(0, TOP_TOOLS).map((metric) => ({
    key: metric.gramUrn,
    label: toolNameFromUrn(metric.gramUrn),
    value: metric.callCount,
  }));

  return (
    <div className="flex flex-col gap-6">
      <StatRow metrics={summaryMetrics(summary)} />
      <div className="flex flex-col gap-3">
        <h4 className="text-eyebrow">Top tools by calls</h4>
        <RankedBarList items={topTools} />
      </div>
    </div>
  );
}
