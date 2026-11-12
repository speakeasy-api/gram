import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import {
  pickTimeBucketMs,
  toolOutcomeDatasets,
} from "@/components/observe/toolUsageTimeSeriesChartData";
import { useOtherSeriesColor } from "@/components/chart/useSeriesColors";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Skeleton } from "@/components/ui/Skeleton";
import type { ToolUsageTargetTimeSeriesPoint } from "@gram/client/models/components/toolusagetargettimeseriespoint.js";
import { format } from "date-fns";
import { useMemo } from "react";

const HOUR_MS = 60 * 60 * 1000;
// How many dates the strip's axis can carry before they collide. Eight leaves
// room for "Aug 18"-width labels on the narrowest layout the page allows.
const MAX_AXIS_LABELS = 8;

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
  failed = false,
  onRetry,
  height = 88,
  maximised = false,
  onToggleMaximised,
}: {
  timeSeries: ToolUsageTargetTimeSeriesPoint[];
  from: Date;
  to: Date;
  onRangeSelect?: (from: Date, to: Date) => void;
  onResetRange?: () => void;
  isZoomed?: boolean;
  loading?: boolean;
  /**
   * The series read failed. With nothing drawn yet the strip says so: an
   * empty axis would read as a window with no calls in it. With a previous
   * series still drawn, it says that series could not be refreshed.
   */
  failed?: boolean;
  onRetry?: () => void;
  /** Shorter on a short viewport, where the list needs the pixels more. */
  height?: number;
  /** The strip spans the panel, with the summary stats in a row above it. */
  maximised?: boolean;
  onToggleMaximised?: () => void;
}): JSX.Element {
  // The series is filtered server-side by the same payload the rows are, and
  // every outcome the status filter can express has its own count — so the
  // strip draws whatever came back and needs no client-side correction.
  // The outcome colors come from the shared OUTCOME tokens, the same ones the
  // status dots and the agent's Tool calls chart use.
  const pendingColor = useOtherSeriesColor();
  const chart = useMemo(() => {
    const fromMs = from.getTime();
    const rangeMs = Math.max(to.getTime() - fromMs, HOUR_MS);
    const bucketMs = pickTimeBucketMs(rangeMs);
    const bucketCount = Math.floor(rangeMs / bucketMs) + 1;

    const timestamps = Array.from(
      { length: bucketCount },
      (_, index) => fromMs + index * bucketMs,
    );

    const showDate = rangeMs > 24 * HOUR_MS;
    // Label the first bucket of each day (or each hour, on a sub-day window)
    // and leave the rest blank: the axis then names every day without
    // repeating itself under every bar.
    //
    // autoSkip is off — the caller's empty labels do the skipping — so on a
    // 90-day window every one of those boundaries would be drawn and they run
    // into each other. Take every Nth instead, chosen so the axis never
    // carries more than MAX_AXIS_LABELS of them.
    let lastBoundary: string | null = null;
    const boundaryIndexes: number[] = [];
    const boundaryLabels = timestamps.map((ts, index) => {
      const date = new Date(ts);
      const boundary = format(date, showDate ? "yyyy-MM-dd" : "yyyy-MM-dd HH");
      if (boundary === lastBoundary) return "";
      lastBoundary = boundary;
      boundaryIndexes.push(index);
      return format(date, showDate ? "MMM d" : "HH:mm");
    });
    const stride = Math.ceil(boundaryIndexes.length / MAX_AXIS_LABELS);
    const keptIndexes = new Set(
      boundaryIndexes.filter((_, nth) => nth % stride === 0),
    );
    const labels = boundaryLabels.map((label, index) =>
      keptIndexes.has(index) ? label : "",
    );

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
      datasets: toolOutcomeDatasets(
        timeSeries,
        { fromMs, bucketMs, bucketCount },
        pendingColor,
      ),
    };
  }, [timeSeries, from, to, pendingColor]);

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

  if (failed && timeSeries.length === 0) {
    return (
      <div className="flex flex-col gap-1">
        <div
          role="alert"
          className="text-muted-foreground flex items-center justify-center gap-3 text-sm"
          style={{ height }}
        >
          Could not load the calls over time.
          {onRetry && (
            <Button variant="tertiary" size="sm" onClick={onRetry}>
              Retry
            </Button>
          )}
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
          {/* A failed refresh keeps the last series drawn, which may be for
              another filter or window, so it says the series is stale. */}
          {failed && (
            <span
              role="alert"
              className="text-muted-foreground flex items-center gap-2 text-[11px]"
            >
              Could not refresh the calls over time.
              {onRetry && (
                <Button variant="tertiary" size="sm" onClick={onRetry}>
                  Retry
                </Button>
              )}
            </span>
          )}
          {isZoomed && onResetRange && (
            <Button variant="tertiary" size="sm" onClick={onResetRange}>
              Reset range
            </Button>
          )}
          {onToggleMaximised && (
            <Button
              variant="tertiary"
              size="sm"
              onClick={onToggleMaximised}
              aria-label={maximised ? "Minimize timeline" : "Maximize timeline"}
            >
              <Icon name={maximised ? "minimize-2" : "maximize-2"} />
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
