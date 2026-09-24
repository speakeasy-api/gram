import { ChartCard } from "@/components/chart/ChartCard";
import {
  formatChartLabel,
  unixNanoToDate,
} from "@/components/chart/chartUtils";
import { AXIS, TOOLTIP, withAlpha } from "@/components/chart/palette";
import {
  useIsDarkTheme,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { formatCompact } from "@/lib/format";
import type { EventVolumeBucket } from "@gram/client/models/components/eventvolumebucket.js";
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  Legend,
  LinearScale,
  Tooltip,
  type ChartData,
  type ChartOptions,
} from "chart.js";
import { useMemo, useState } from "react";
import { Bar } from "react-chartjs-2";

ChartJS.register(CategoryScale, LinearScale, BarElement, Tooltip, Legend);

const CHART_ID = "event-volume";

export interface EventVolumeChartProps {
  /** Zero-filled buckets in ascending time order. */
  buckets: EventVolumeBucket[];
  /** Span of the selected window in milliseconds, picks the axis label format. */
  timeRangeMs: number;
  isLoading: boolean;
  isError: boolean;
}

/**
 * Event volume over time: stacked bars splitting ingested OpenTelemetry log
 * records vs spans, driven by `telemetry.getEventVolume` buckets. Modeled on
 * `ToolCallsTimeSeriesChart`.
 */
export function EventVolumeChart({
  buckets,
  timeRangeMs,
  isLoading,
  isError,
}: EventVolumeChartProps): JSX.Element {
  const [expandedChart, setExpandedChart] = useState<string | null>(null);
  const isExpanded = expandedChart === CHART_ID;
  const height = isExpanded ? 420 : 220;
  const hasData = buckets.some((b) => b.logCount > 0 || b.spanCount > 0);

  const seriesColors = useSeriesColors();
  const chartData = useMemo<ChartData<"bar", number[], string>>(() => {
    const labels = buckets.map((b) =>
      formatChartLabel(unixNanoToDate(b.bucketTimeUnixNano), timeRangeMs),
    );

    return {
      labels,
      datasets: [
        {
          label: "Logs",
          data: buckets.map((b) => b.logCount),
          // First two slots of the categorical ramp (ink, brand blue) — two
          // neutral peers, unlike the success/failure split elsewhere.
          backgroundColor: withAlpha(seriesColors[0]!, 0.75),
          stack: "events",
        },
        {
          label: "Spans",
          data: buckets.map((b) => b.spanCount),
          backgroundColor: withAlpha(seriesColors[1]!, 0.75),
          stack: "events",
        },
      ],
    };
  }, [buckets, timeRangeMs, seriesColors]);

  // Chart.js paints the canvas with static defaults that ignore the CSS
  // theme, so gridlines and tick labels need explicit dark-mode colors.
  const isDark = useIsDarkTheme();

  const options = useMemo<ChartOptions<"bar">>(() => {
    const textColor = isDark ? AXIS.faded : AXIS.label;
    return {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: {
          position: "bottom",
          labels: {
            boxWidth: 12,
            usePointStyle: true,
            padding: 16,
            font: { size: 11 },
          },
        },
        tooltip: {
          ...TOOLTIP,
          padding: 12,
          usePointStyle: true,
          callbacks: {
            label: (item) =>
              ` ${item.dataset.label}: ${formatCompact(Number(item.parsed.y ?? 0))}`,
          },
        },
      },
      scales: {
        x: {
          stacked: true,
          grid: {
            display: true,
            color: isDark ? AXIS.gridDark : AXIS.grid,
          },
          ticks: { maxTicksLimit: 8, color: textColor },
        },
        y: {
          stacked: true,
          beginAtZero: true,
          grid: { color: isDark ? AXIS.gridDark : AXIS.grid },
          ticks: {
            color: textColor,
            callback: (value) => formatCompact(Number(value)),
          },
        },
      },
    };
  }, [isDark]);

  return (
    <ChartCard
      title="Event Volume"
      chartId={CHART_ID}
      expandedChart={expandedChart}
      onExpand={setExpandedChart}
      hasData={hasData}
      loading={isLoading}
      error={isError}
    >
      {!hasData ? (
        <WidgetEmptyState
          message="No events for the selected time range"
          className="h-[220px]"
        />
      ) : (
        <div style={{ height }}>
          <Bar data={chartData} options={options} />
        </div>
      )}
    </ChartCard>
  );
}
