import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import {
  bucketStartNsToMs,
  pickTimeBucketMs,
} from "@/components/observe/toolUsageTimeSeriesChartData";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import type { ToolUsageTargetTimeSeriesPoint } from "@gram/client/models/components/toolusagetargettimeseriespoint.js";
import { format } from "date-fns";
import { useMemo } from "react";

const HOUR_MS = 60 * 60 * 1000;
// The same colours the status dots use, so a red bar and a red dot are read as
// the same fact rather than two palettes that happen to share a meaning.
// emerald-500, rose-500, amber-500, and the muted grey a pending dot carries.
const OK_COLOR = "#10b981";
const OK_HOVER_COLOR = "#059669";
const FAILED_COLOR = "#f43f5e";
const BLOCKED_COLOR = "#f59e0b";
const PENDING_COLOR = "#d4d4d8";

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
  onRangeSelect,
  onResetRange,
  isZoomed = false,
  loading = false,
  height = 88,
}: {
  timeSeries: ToolUsageTargetTimeSeriesPoint[];
  from: Date;
  to: Date;
  onRangeSelect?: (from: Date, to: Date) => void;
  onResetRange?: () => void;
  isZoomed?: boolean;
  loading?: boolean;
  /** Shorter on a short viewport, where the list needs the pixels more. */
  height?: number;
}): JSX.Element {
  // The series is filtered server-side by the same payload the rows are, and
  // every outcome the status filter can express has its own count — so the
  // strip draws whatever came back and needs no client-side correction.
  const chart = useMemo(() => {
    const fromMs = from.getTime();
    const rangeMs = Math.max(to.getTime() - fromMs, HOUR_MS);
    const bucketMs = pickTimeBucketMs(rangeMs);
    const bucketCount = Math.floor(rangeMs / bucketMs) + 1;

    const ok = Array.from<number>({ length: bucketCount }).fill(0);
    const failed = Array.from<number>({ length: bucketCount }).fill(0);
    const blocked = Array.from<number>({ length: bucketCount }).fill(0);
    const pending = Array.from<number>({ length: bucketCount }).fill(0);
    const timestamps = Array.from(
      { length: bucketCount },
      (_, index) => fromMs + index * bucketMs,
    );

    for (const point of timeSeries) {
      const ms = bucketStartNsToMs(point.bucketStartNs);
      if (ms === null) continue;
      // The bucket containing `from` starts before it. Clamp rather than drop
      // it, or the first bar of every window silently loses its events.
      const index = Math.min(
        Math.max(Math.floor((ms - fromMs) / bucketMs), 0),
        bucketCount - 1,
      );
      const failures = Number(point.failureCount);
      const blocks = Number(point.blockedCount);
      const pends = Number(point.pendingCount);
      // eventCount is every call in the bucket, so the successful remainder is
      // what the three named outcomes leave behind.
      ok[index] =
        (ok[index] ?? 0) +
        Math.max(Number(point.eventCount) - failures - blocks - pends, 0);
      failed[index] = (failed[index] ?? 0) + failures;
      blocked[index] = (blocked[index] ?? 0) + blocks;
      pending[index] = (pending[index] ?? 0) + pends;
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

    // No points at all: hand the chart empty labels so it renders its own
    // no-data state instead of a full axis of zero-height bars.
    if (timeSeries.length === 0) {
      return {
        bucketMs,
        timestamps: [],
        labels: [],
        tooltipLabels: [],
        datasets: [],
      };
    }

    return {
      bucketMs,
      timestamps,
      labels,
      tooltipLabels: timestamps.map((ts) =>
        format(new Date(ts), showDate ? "MMM d, HH:mm" : "HH:mm"),
      ),
      // A series of all zeroes still adds a legend entry and a tooltip line
      // for an outcome that never happened, so each one earns its place.
      datasets: [
        ...(ok.some((value) => value > 0)
          ? [
              {
                label: "Calls",
                data: ok,
                backgroundColor: OK_COLOR,
                hoverBackgroundColor: OK_HOVER_COLOR,
              },
            ]
          : []),
        ...(failed.some((value) => value > 0)
          ? [
              {
                label: "Failures",
                data: failed,
                backgroundColor: FAILED_COLOR,
                hoverBackgroundColor: FAILED_COLOR,
              },
            ]
          : []),
        ...(blocked.some((value) => value > 0)
          ? [
              {
                label: "Blocked",
                data: blocked,
                backgroundColor: BLOCKED_COLOR,
                hoverBackgroundColor: BLOCKED_COLOR,
              },
            ]
          : []),
        ...(pending.some((value) => value > 0)
          ? [
              {
                label: "Pending",
                data: pending,
                backgroundColor: PENDING_COLOR,
                hoverBackgroundColor: PENDING_COLOR,
              },
            ]
          : []),
      ],
    };
  }, [timeSeries, from, to]);

  // Only before there is anything to show. Once a shape has been drawn the
  // caller keeps it on screen while the next one loads, so a narrowed filter
  // updates the bars in place instead of flashing a grey block where the
  // chart was.
  if (loading) {
    return (
      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between gap-2">
          <span className="text-muted-foreground/70 text-[11px]">
            Drag to zoom
          </span>
        </div>
        <div style={{ height }}>
          <Skeleton className="size-full" />
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2">
        <span className="text-muted-foreground/70 text-[11px]">
          Drag to zoom
        </span>
        <div className="flex items-center gap-2">
          {isZoomed && onResetRange && (
            <Button variant="tertiary" size="sm" onClick={onResetRange}>
              Reset range
            </Button>
          )}
        </div>
      </div>

      {/* The crosshair says the action is a horizontal selection before the
          reader tries it. */}
      <div className="cursor-crosshair" style={{ height }}>
        <StackedTimeBarChart
          labels={chart.labels}
          timestamps={chart.timestamps}
          bucketMs={chart.bucketMs}
          tooltipLabels={chart.tooltipLabels}
          datasets={chart.datasets}
          onRangeSelect={onRangeSelect}
          height={height}
          compact
        />
      </div>
    </div>
  );
}
