import { ACCENT_RED, GOOD_GREEN } from "@/components/chart/palette";
import type { TimeSeriesDataset } from "@/components/observe/toolUsageTimeSeriesChartData";
import type { UserSessionTrend } from "@gram/client/models/components/usersessiontrend.js";

const BUCKET_MS: Record<string, number> = {
  hour: 3_600_000,
  day: 24 * 3_600_000,
};

// Buckets are UTC intervals, so every label and tooltip reads in UTC whatever
// the viewer's time zone: a local day label would name the wrong day west of
// UTC, and an hourly one would land on :30 in half-hour zones.
const UTC_DAY = new Intl.DateTimeFormat(undefined, {
  timeZone: "UTC",
  month: "short",
  day: "numeric",
});
const UTC_TIME = new Intl.DateTimeFormat(undefined, {
  timeZone: "UTC",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});

function utcAt(date: Date): string {
  return `${UTC_DAY.format(date)}, ${UTC_TIME.format(date)}`;
}

// A day bucket is named by its day; an hourly one by its time, with its day
// once the window spans more than one.
function utcLabel(date: Date, bucket: string, spanMs: number): string {
  if (bucket === "day") return UTC_DAY.format(date);
  if (spanMs <= BUCKET_MS.day!) return UTC_TIME.format(date);
  return utcAt(date);
}

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
    utcLabel(bucket.start, trend.bucket, span),
  );
  const bucketMs = BUCKET_MS[trend.bucket] ?? span;
  return {
    events: {
      bucketMs,
      timestamps,
      labels,
      tooltipLabels: trend.buckets.map(
        (bucket) => `${utcAt(bucket.start)} – ${utcAt(bucket.end)} UTC`,
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
        (bucket) => `Just before ${utcAt(bucket.end)} UTC`,
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
