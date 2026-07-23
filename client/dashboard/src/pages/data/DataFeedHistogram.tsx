import { CHART_COLORS, OTHER_COLOR } from "@/components/stacked-time-series";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { dateTimeFormatters } from "@/lib/dates";
import { formatCompact } from "@/lib/format";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import {
  BarController,
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  LinearScale,
  Tooltip,
  type ChartOptions,
} from "chart.js";
import { Info } from "lucide-react";
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

const BUCKET_MINUTES = 2;
const BUCKET_COUNT = 30; // one hour of two-minute buckets, newest at the right

// Series colors from the shared stacked time-series palette (the billing
// token-usage chart): metrics in the palette blue, logs in the neutral slate.
const METRIC_COLOR = CHART_COLORS[0]!;
const LOG_COLOR = OTHER_COLOR;

interface HistogramBuckets {
  labels: string[];
  logCounts: number[];
  metricCounts: number[];
}

/**
 * Buckets events into fixed two-minute windows ending now (the feed is a
 * live stream, so the right edge is the present).
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

// Static legend chip matching the StackedTimeSeriesPanel legend styling.
function LegendSwatch({
  color,
  label,
}: {
  color: string;
  label: string;
}): JSX.Element {
  return (
    <span className="text-muted-foreground flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs">
      <span
        aria-hidden
        className="size-2.5 rounded-[3px]"
        style={{ backgroundColor: color }}
      />
      {label}
    </span>
  );
}

/**
 * Event volume over time above the feed — a copy of the billing page's
 * token-usage time-series chart (StackedTimeSeriesPanel), stacked by kind
 * and fed by the currently filtered events.
 */
export function DataFeedHistogram({
  events,
}: {
  events: DataEvent[];
}): JSX.Element {
  const buckets = useMemo(() => bucketEvents(events), [events]);

  // Chart.js paints the canvas with static defaults that ignore the CSS
  // theme, so axis/legend text and gridlines need explicit dark-mode colors.
  const { theme } = useMoonshineConfig();
  const isDark = theme === "dark";

  const options = useMemo<ChartOptions<"bar">>(() => {
    const textColor = isDark ? "rgba(255, 255, 255, 0.85)" : "#666";
    const gridColor = isDark ? "#666" : "rgba(0, 0, 0, 0.08)";
    return {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label: (item) =>
              `${item.dataset.label}: ${Number(item.raw).toLocaleString()} events`,
          },
        },
      },
      scales: {
        x: {
          stacked: true,
          grid: { display: false },
          ticks: { maxTicksLimit: 16, color: textColor },
        },
        y: {
          stacked: true,
          beginAtZero: true,
          grid: { color: gridColor },
          ticks: {
            color: textColor,
            precision: 0,
            callback: (value) => formatCompact(Number(value)),
          },
        },
      },
    };
  }, [isDark]);

  const data = useMemo(
    () => ({
      labels: buckets.labels,
      datasets: [
        {
          label: "Logs",
          data: buckets.logCounts,
          backgroundColor: LOG_COLOR,
        },
        {
          label: "Metrics",
          data: buckets.metricCounts,
          backgroundColor: METRIC_COLOR,
        },
      ],
    }),
    [buckets],
  );

  return (
    <div className="border-border rounded-lg border p-4">
      <div className="flex items-center gap-1.5 text-sm font-semibold">
        Event Volume
        <SimpleTooltip tooltip="Events ingested over the last hour, stacked by kind. Reflects the active filters.">
          <Info className="text-muted-foreground size-3.5" />
        </SimpleTooltip>
      </div>
      <div className="mt-4" style={{ height: 280 }}>
        <Bar data={data} options={options} />
      </div>
      <div className="mt-3 flex flex-wrap items-center justify-center gap-1.5">
        <LegendSwatch color={LOG_COLOR} label="Logs" />
        <LegendSwatch color={METRIC_COLOR} label="Metrics" />
      </div>
    </div>
  );
}
