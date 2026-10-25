import { formatChartLabel } from "@/components/chart/chartUtils";
import { ACCENT_RED, GOOD_GREEN } from "@/components/chart/palette";
import type { TimeSeriesDataset } from "@/components/observe/toolUsageTimeSeriesChartData";
import type { UserSessionTrend } from "@gram/client/models/components/usersessiontrend.js";

const BUCKET_MS: Record<string, number> = {
  hour: 3_600_000,
  day: 24 * 3_600_000,
};

/** One chart's props over a trend's buckets. */
type TrendChart = {
  bucketMs: number;
  timestamps: number[];
  labels: string[];
  tooltipLabels: string[];
  datasets: TimeSeriesDataset[];
};

/**
 * Two charts over the same buckets: created and revoked side by side as
 * events, and the active stock on its own, so the two are never summed.
 */
export function trendChartData(
  trend: UserSessionTrend,
  from: Date,
  to: Date,
  activeColor: string,
): { events: TrendChart; active: TrendChart } {
  const span = to.getTime() - from.getTime();
  const timestamps = trend.buckets.map((bucket) => bucket.start.getTime());
  const labels = trend.buckets.map((bucket) =>
    formatChartLabel(bucket.start, span),
  );
  const at = (date: Date) =>
    date.toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    });
  const bucketMs = BUCKET_MS[trend.bucket] ?? span;
  return {
    events: {
      bucketMs,
      timestamps,
      labels,
      tooltipLabels: trend.buckets.map(
        (bucket) => `${at(bucket.start)} – ${at(bucket.end)}`,
      ),
      datasets: [
        {
          label: "Created",
          stack: "created",
          data: trend.buckets.map((bucket) => bucket.created),
          backgroundColor: GOOD_GREEN,
          hoverBackgroundColor: GOOD_GREEN,
        },
        {
          label: "Revoked",
          stack: "revoked",
          data: trend.buckets.map((bucket) => bucket.revoked),
          backgroundColor: ACCENT_RED,
          hoverBackgroundColor: ACCENT_RED,
        },
      ],
    },
    active: {
      bucketMs,
      timestamps,
      labels,
      tooltipLabels: trend.buckets.map(
        (bucket) => `Just before ${at(bucket.end)}`,
      ),
      datasets: [
        {
          label: "Active",
          data: trend.buckets.map((bucket) => bucket.active),
          backgroundColor: activeColor,
          hoverBackgroundColor: activeColor,
        },
      ],
    },
  };
}
