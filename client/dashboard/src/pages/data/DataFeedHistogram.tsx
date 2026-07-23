import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import {
  BarController,
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  LinearScale,
  Tooltip,
  type ChartOptions,
} from "chart.js";
import { useMemo } from "react";
import { Bar } from "react-chartjs-2";
import type { DataEvent } from "./data-events";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarController,
  BarElement,
  Tooltip,
);

const BUCKET_MINUTES = 5;
const BUCKET_COUNT = 12; // one hour of five-minute buckets, newest at the right

// Chart.js needs literal colors. These match the kind treatment in the table:
// logs are neutral, metrics are the information blue.
const LOG_COLOR = "#a3a3a3";
const METRIC_COLOR = "#60a5fa";

const CHART_OPTIONS: ChartOptions<"bar"> = {
  responsive: true,
  maintainAspectRatio: false,
  animation: false,
  plugins: {
    legend: { display: false },
    tooltip: {
      backgroundColor: "#171717",
      titleColor: "#fafafa",
      bodyColor: "#d4d4d4",
      borderColor: "#262626",
      borderWidth: 1,
      displayColors: true,
      boxWidth: 8,
      boxHeight: 8,
    },
  },
  scales: {
    x: {
      stacked: true,
      grid: { display: false },
      ticks: {
        maxTicksLimit: 6,
        maxRotation: 0,
        font: { size: 10 },
        color: "#737373",
      },
    },
    y: {
      stacked: true,
      beginAtZero: true,
      grid: { color: "#e5e5e51a" },
      ticks: {
        precision: 0,
        maxTicksLimit: 4,
        font: { size: 10 },
        color: "#737373",
      },
    },
  },
};

interface HistogramBuckets {
  labels: string[];
  logCounts: number[];
  metricCounts: number[];
}

/**
 * Buckets events into fixed five-minute windows ending at the newest event
 * (the feed is a live stream, so the right edge is "now").
 */
function bucketEvents(events: DataEvent[]): HistogramBuckets {
  const bucketMs = BUCKET_MINUTES * 60_000;
  const end = Math.ceil(Date.now() / bucketMs) * bucketMs;
  const start = end - BUCKET_COUNT * bucketMs;

  const labels: string[] = [];
  const logCounts = Array.from({ length: BUCKET_COUNT }, () => 0);
  const metricCounts = Array.from({ length: BUCKET_COUNT }, () => 0);

  for (let i = 0; i < BUCKET_COUNT; i++) {
    labels.push(dateTimeFormatters.time.format(new Date(start + i * bucketMs)));
  }

  for (const event of events) {
    const index = Math.floor((event.timestamp.getTime() - start) / bucketMs);
    if (index < 0 || index >= BUCKET_COUNT) continue;
    if (event.kind === "metric") {
      metricCounts[index] = (metricCounts[index] ?? 0) + 1;
    } else {
      logCounts[index] = (logCounts[index] ?? 0) + 1;
    }
  }

  return { labels, logCounts, metricCounts };
}

function LegendSwatch({
  color,
  label,
}: {
  color: string;
  label: string;
}): JSX.Element {
  return (
    <span className="flex items-center gap-1.5">
      <span
        aria-hidden
        className="size-2 rounded-xs"
        style={{ backgroundColor: color }}
      />
      <Type muted small>
        {label}
      </Type>
    </span>
  );
}

/**
 * Volume-over-time histogram above the feed, the way standard log viewers
 * lead with one. Reflects the currently filtered events, stacked by kind.
 */
export function DataFeedHistogram({
  events,
}: {
  events: DataEvent[];
}): JSX.Element {
  const buckets = useMemo(() => bucketEvents(events), [events]);

  const data = useMemo(
    () => ({
      labels: buckets.labels,
      datasets: [
        {
          label: "Logs",
          data: buckets.logCounts,
          backgroundColor: LOG_COLOR,
          stack: "events",
          barPercentage: 0.9,
          categoryPercentage: 0.9,
        },
        {
          label: "Metrics",
          data: buckets.metricCounts,
          backgroundColor: METRIC_COLOR,
          stack: "events",
          barPercentage: 0.9,
          categoryPercentage: 0.9,
        },
      ],
    }),
    [buckets],
  );

  return (
    <div className="border-border rounded-md border p-3">
      <div className="mb-2 flex items-center justify-end gap-4">
        <LegendSwatch color={LOG_COLOR} label="Logs" />
        <LegendSwatch color={METRIC_COLOR} label="Metrics" />
      </div>
      <div className="h-24">
        <Bar data={data} options={CHART_OPTIONS} />
      </div>
    </div>
  );
}
