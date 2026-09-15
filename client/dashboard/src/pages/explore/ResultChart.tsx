import { AXIS, TOOLTIP, withAlpha } from "@/components/chart/palette";
import { useSeriesColors } from "@/components/chart/useSeriesColors";
import { useIsDarkTheme } from "@/lib/theme";
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  type ChartDataset,
  type ChartOptions,
} from "chart.js";
import { useMemo, type JSX } from "react";
import { Chart } from "react-chartjs-2";
import { formatMeasureValue, type ChartType, type Grain } from "./exploreModel";
import {
  bucketTick,
  bucketTitle,
  isSparse,
  type SeriesSet,
} from "./resultSeries";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  LineElement,
  PointElement,
  Filler,
  Tooltip,
  Legend,
);

export const CHART_HEIGHT = 320;

type TimeseriesDataset = ChartDataset<"bar" | "line", (number | null)[]>;

/** A bucketed result as a line, area or bar chart, one series per tuple. */
export function ResultChart({
  seriesSet,
  unit,
  chartType,
  grain,
}: {
  seriesSet: SeriesSet;
  /** The unit every series shares; the axis and tooltips format by it. */
  unit: string;
  chartType: ChartType;
  grain: Grain;
}): JSX.Element {
  const colors = useSeriesColors();
  const isDark = useIsDarkTheme();
  const { buckets, series } = seriesSet;

  const datasets = useMemo<TimeseriesDataset[]>(
    () =>
      series.map((one, index) => {
        const color = colors[index % colors.length]!;
        const base = {
          label: one.label,
          data: one.points,
          backgroundColor: chartType === "area" ? withAlpha(color, 0.2) : color,
          borderColor: color,
        };
        if (chartType === "bar") return { ...base, type: "bar" };
        return {
          ...base,
          type: "line",
          borderWidth: 1.5,
          pointRadius: isSparse(one.points) ? 4 : 0,
          pointHoverRadius: 4,
          fill: chartType === "area",
          spanGaps: true,
        };
      }),
    [series, chartType, colors],
  );

  const options = useMemo<ChartOptions<"line" | "bar">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: {
          display: datasets.length > 1,
          position: "bottom",
          labels: { color: AXIS.label, boxWidth: 10, boxHeight: 10 },
        },
        tooltip: {
          ...TOOLTIP,
          callbacks: {
            title: (items) => {
              const iso = items[0]?.label;
              return iso ? bucketTitle(iso, grain) : "";
            },
            label: (context) =>
              `${context.dataset.label}: ${formatMeasureValue(Number(context.parsed.y ?? 0), unit)}`,
          },
        },
      },
      scales: {
        x: {
          grid: { display: false },
          ticks: {
            color: AXIS.label,
            autoSkip: true,
            maxRotation: 0,
            maxTicksLimit: 10,
            callback: (value) => {
              const iso = buckets[Number(value)];
              return iso === undefined ? "" : bucketTick(iso, grain);
            },
          },
        },
        y: {
          beginAtZero: true,
          grid: { color: isDark ? AXIS.gridDark : AXIS.grid },
          ticks: {
            color: AXIS.label,
            callback: (value) => formatMeasureValue(Number(value), unit),
          },
        },
      },
    }),
    [datasets.length, unit, isDark, buckets, grain],
  );

  return (
    <div style={{ height: CHART_HEIGHT }}>
      <Chart
        type={chartType === "bar" ? "bar" : "line"}
        data={{ labels: buckets, datasets }}
        options={options}
      />
    </div>
  );
}
