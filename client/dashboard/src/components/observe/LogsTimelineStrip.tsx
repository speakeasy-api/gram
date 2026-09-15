import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import {
  bucketStartNsToMs,
  pickTimeBucketMs,
} from "@/components/observe/toolUsageTimeSeriesChartData";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import type { ToolUsageTargetTimeSeriesPoint } from "@gram/client/models/components/toolusagetargettimeseriespoint.js";
import { format } from "date-fns";
import { useMemo } from "react";

const HOUR_MS = 60 * 60 * 1000;
// Successful calls take a desaturated emerald — the same reading as the green
// status dot on a row, dialled down so a wall of them stays background. Grey
// said "no data" rather than "these worked". Failures keep the only saturated
// colour on the strip, so the eye lands on them first.
const OK_COLOR = "#b3cfc3";
const OK_HOVER_COLOR = "#9dc0b1";
const FAILED_COLOR = "#f43f5e";

/**
 * The shape of the window above the rows, and a way to narrow it: drag across
 * the strip to set the time range.
 *
 * It deliberately does not stack by server the way the Insights chart does. At
 * 64px tall a ten-server stack is a black mass with unreadable slivers on top,
 * and the question here is not "which server" — the rail answers that — but
 * "when did calls happen, and when did they fail". So: two series, the second
 * being the part worth reacting to.
 */
export function LogsTimelineStrip({
  timeSeries,
  from,
  to,
  statuses = [],
  onRangeSelect,
  onResetRange,
  isZoomed = false,
  loading = false,
  degraded = false,
}: {
  timeSeries: ToolUsageTargetTimeSeriesPoint[];
  from: Date;
  to: Date;
  /** Applied status filter, honoured for the outcomes the series can express. */
  statuses?: string[];
  onRangeSelect?: (from: Date, to: Date) => void;
  onResetRange?: () => void;
  isZoomed?: boolean;
  loading?: boolean;
  degraded?: boolean;
}): JSX.Element {
  // The series carries a count and a failure count per bucket, so a filter on
  // errors or successes can be honoured here by dropping the other half. The
  // server has no per-status time series, so blocked and pending cannot be
  // separated this way — those still widen the strip past the table, which the
  // caller flags.
  const showOk = statuses.length === 0 || statuses.includes("success");
  const showFailed = statuses.length === 0 || statuses.includes("error");
  const chart = useMemo(() => {
    const fromMs = from.getTime();
    const rangeMs = Math.max(to.getTime() - fromMs, HOUR_MS);
    const bucketMs = pickTimeBucketMs(rangeMs);
    const bucketCount = Math.floor(rangeMs / bucketMs) + 1;

    const ok = new Array<number>(bucketCount).fill(0);
    const failed = new Array<number>(bucketCount).fill(0);
    const timestamps = Array.from(
      { length: bucketCount },
      (_, index) => fromMs + index * bucketMs,
    );

    for (const point of timeSeries) {
      const ms = bucketStartNsToMs(point.bucketStartNs);
      if (ms === null) continue;
      const index = Math.floor((ms - fromMs) / bucketMs);
      if (index < 0 || index >= bucketCount) continue;
      const failures = Number(point.failureCount);
      ok[index] =
        (ok[index] ?? 0) + Math.max(Number(point.eventCount) - failures, 0);
      failed[index] = (failed[index] ?? 0) + failures;
    }

    const showDate = rangeMs > 24 * HOUR_MS;
    // Label the first bucket of each day (or each hour, on a sub-day window)
    // and leave the rest blank: the axis then names every day without
    // repeating itself under every bar.
    let lastBoundary: string | null = null;
    const labels = timestamps.map((ts) => {
      const date = new Date(ts);
      const boundary = format(date, showDate ? "yyyy-MM-dd" : "yyyy-MM-dd HH");
      if (boundary === lastBoundary) return "";
      lastBoundary = boundary;
      return format(date, showDate ? "MMM d" : "HH:mm");
    });

    return {
      bucketMs,
      timestamps,
      labels,
      tooltipLabels: timestamps.map((ts) =>
        format(new Date(ts), showDate ? "MMM d, HH:mm" : "HH:mm"),
      ),
      datasets: [
        ...(showOk
          ? [
              {
                label: "Calls",
                data: ok,
                backgroundColor: OK_COLOR,
                hoverBackgroundColor: OK_HOVER_COLOR,
              },
            ]
          : []),
        ...(showFailed
          ? [
              {
                label: "Failures",
                data: failed,
                backgroundColor: FAILED_COLOR,
                hoverBackgroundColor: FAILED_COLOR,
              },
            ]
          : []),
      ],
    };
  }, [timeSeries, from, to, showOk, showFailed]);

  if (loading) return <Skeleton className="h-16 w-full" />;

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2">
        <span className="text-muted-foreground/70 text-[11px]">
          Drag to zoom
        </span>
        <div className="flex items-center gap-2">
          {degraded && (
            <Badge variant="neutral">
              <Badge.Text>Summary and timeline ignore this filter</Badge.Text>
            </Badge>
          )}
          {isZoomed && onResetRange && (
            <Button variant="tertiary" size="sm" onClick={onResetRange}>
              Reset range
            </Button>
          )}
        </div>
      </div>

      <StackedTimeBarChart
        labels={chart.labels}
        timestamps={chart.timestamps}
        bucketMs={chart.bucketMs}
        tooltipLabels={chart.tooltipLabels}
        datasets={chart.datasets}
        onRangeSelect={onRangeSelect}
        height={88}
        compact
      />
    </div>
  );
}
