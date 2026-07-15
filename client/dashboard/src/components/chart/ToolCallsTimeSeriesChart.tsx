import type { TimeSeriesBucket } from "@gram/client/models/components/timeseriesbucket.js";
import { useMemo } from "react";

import { ChartCard } from "./ChartCard";
import { unixNanoToDate } from "./chartUtils";
import { formatCompact } from "@/lib/format";
import { Timeseries, type TimeseriesSeries } from "./Timeseries";

export interface ToolCallsTimeSeriesChartProps {
  title: string;
  chartId: string;
  timeSeries: TimeSeriesBucket[];
  // Span of the selected window in milliseconds, used to pick the axis label format.
  timeRangeMs: number;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}

/**
 * Tool-call volume over time: stacked bars split successful vs failed calls,
 * with a smoothed trend line of total calls overlaid. Driven by the
 * `time_series` buckets returned from `getObservabilityOverview`.
 */
export function ToolCallsTimeSeriesChart({
  title,
  chartId,
  timeSeries,
  expandedChart,
  onExpand,
}: ToolCallsTimeSeriesChartProps): JSX.Element {
  const isExpanded = expandedChart === chartId;
  const height = isExpanded ? 420 : 260;
  const hasData = timeSeries.some((b) => b.totalToolCalls > 0);

  // Timeseries stacks every bar series and derives the trend line from their
  // sum, so "Successful" and "Failed" are all it needs.
  const series = useMemo<TimeseriesSeries[]>(() => {
    const points = (pick: (bucket: TimeSeriesBucket) => number) =>
      timeSeries.map((bucket) => ({
        x: unixNanoToDate(bucket.bucketTimeUnixNano),
        y: pick(bucket),
      }));

    return [
      {
        label: "Successful",
        data: points((b) => Math.max(b.totalToolCalls - b.failedToolCalls, 0)),
        color: "var(--fill-success-default)",
      },
      {
        label: "Failed",
        data: points((b) => b.failedToolCalls),
        color: "var(--fill-destructive-default)",
      },
    ];
  }, [timeSeries]);

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
    >
      <Timeseries
        series={series}
        mode="bar-with-trend"
        height={height}
        valueFormatter={formatCompact}
        emptyMessage="No tool calls for the selected time range"
      />
    </ChartCard>
  );
}
