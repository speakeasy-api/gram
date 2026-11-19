import { chartColors, SHARED_TOOLTIP } from "@/components/chart/chartTheme";
import { useIsDarkTheme } from "@/lib/theme";
import type { Chart, ChartOptions, Plugin } from "chart.js";
import { useMemo } from "react";
import { Bar } from "react-chartjs-2";
import { nowPosition } from "./format";

export interface HealthSeries {
  label: string;
  data: number[];
  color: string;
}

function monoFont(): string {
  if (typeof document === "undefined") return "monospace";
  const family = getComputedStyle(document.documentElement)
    .getPropertyValue("--font-mono")
    .trim();
  return family || "monospace";
}

/** Diagonal stripes in the series colour: a bucket still filling up. */
function hatch(color: string): CanvasPattern | string {
  if (typeof document === "undefined") return color;
  const tile = document.createElement("canvas");
  tile.width = 6;
  tile.height = 6;
  const ctx = tile.getContext("2d");
  if (!ctx) return color;
  ctx.globalAlpha = 0.35;
  ctx.fillStyle = color;
  ctx.fillRect(0, 0, 6, 6);
  ctx.globalAlpha = 1;
  ctx.strokeStyle = color;
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  ctx.moveTo(0, 6);
  ctx.lineTo(6, 0);
  ctx.moveTo(-1, 1);
  ctx.lineTo(1, -1);
  ctx.moveTo(5, 7);
  ctx.lineTo(7, 5);
  ctx.stroke();
  return ctx.createPattern(tile, "repeat") ?? color;
}

function nowMarker(
  position: { index: number; fraction: number } | null,
  color: string,
  font: string,
): Plugin<"bar"> {
  return {
    id: "healthNowMarker",
    afterDatasetsDraw(chart: Chart<"bar">) {
      if (!position) return;
      const meta = chart.getDatasetMeta(0);
      const bar = meta.data[position.index] as unknown as
        | { x: number; width: number }
        | undefined;
      if (!bar) return;
      const scale = chart.scales.x;
      const slot = scale
        ? scale.width / Math.max(1, chart.data.labels!.length)
        : bar.width;
      const x = bar.x + (position.fraction - 0.5) * slot;
      const { top, bottom } = chart.chartArea;
      const ctx = chart.ctx;
      ctx.save();
      ctx.strokeStyle = color;
      ctx.lineWidth = 1;
      ctx.setLineDash([3, 3]);
      ctx.beginPath();
      ctx.moveTo(x, top);
      ctx.lineTo(x, bottom);
      ctx.stroke();
      ctx.setLineDash([]);
      ctx.fillStyle = color;
      ctx.font = `10px ${font}`;
      ctx.textBaseline = "top";
      ctx.fillText("Now", x + 4, top);
      ctx.restore();
    },
  };
}

/** Stacked bars over time, marking now and the bucket still filling up. */
export function HealthBarChart({
  labels,
  tooltipLabels,
  timestamps,
  bucketMs,
  series,
  now,
  height,
}: {
  labels: string[];
  tooltipLabels: string[];
  timestamps: number[];
  bucketMs: number;
  series: HealthSeries[];
  now: Date;
  height: number;
}): JSX.Element {
  const isDark = useIsDarkTheme();
  const colors = chartColors(isDark);
  const font = useMemo(monoFont, []);
  const position = nowPosition(timestamps, bucketMs, now.getTime());
  const seam = isDark ? "#121212" : "#ffffff";
  const data = useMemo(
    () => ({
      labels,
      datasets: series.map((s, i) => ({
        label: s.label,
        data: s.data,
        stack: "all",
        backgroundColor: s.data.map((_, j) =>
          j === position?.index ? hatch(s.color) : s.color,
        ),
        borderColor: s.data.map((_, j) =>
          j === position?.index ? s.color : seam,
        ),
        borderWidth: s.data.map((_, j) =>
          j === position?.index ? 1 : i < series.length - 1 ? { top: 2 } : 0,
        ),
        borderSkipped: false as const,
        barPercentage: 0.7,
        categoryPercentage: 0.9,
      })),
    }),
    [labels, series, position?.index, seam],
  );
  const options: ChartOptions<"bar"> = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: "index", intersect: false },
    plugins: {
      legend: { display: false },
      tooltip: {
        ...SHARED_TOOLTIP,
        filter: (item) => (item.parsed.y ?? 0) !== 0,
        callbacks: {
          title: (items) => tooltipLabels[items[0]?.dataIndex ?? 0] ?? "",
          label: (item) => `${item.dataset.label}: ${item.formattedValue}`,
        },
      },
    },
    scales: {
      x: {
        stacked: true,
        grid: { display: false },
        border: { display: true, color: colors.gridLine },
        ticks: {
          autoSkip: false,
          maxRotation: 0,
          color: colors.labelFaded,
          font: { family: font, size: 11 },
        },
      },
      y: {
        stacked: true,
        beginAtZero: true,
        border: { display: false },
        grid: { color: colors.gridLine },
        ticks: {
          maxTicksLimit: 3,
          precision: 0,
          color: colors.labelFaded,
          font: { family: font, size: 11 },
        },
      },
    },
    layout: { padding: { right: 28 } },
    animation: { duration: 200 },
  };
  return (
    <div style={{ height }}>
      <Bar
        key={isDark ? "dark" : "light"}
        data={data}
        options={options}
        plugins={[nowMarker(position, colors.labelFaded, font)]}
      />
    </div>
  );
}

/** A legend entry as the design draws it: a dot and the series in words. */
export function LegendDot({
  color,
  label,
}: {
  color: string;
  label: string;
}): JSX.Element {
  return (
    <span className="inline-flex items-center gap-1.5 text-sm">
      <span
        aria-hidden
        className="size-2.5 shrink-0 rounded-full"
        style={{ backgroundColor: color }}
      />
      {label}
    </span>
  );
}
