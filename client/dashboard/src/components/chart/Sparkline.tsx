// A tiny inline trend chart — pure SVG, no Chart.js — for stat tiles and
// table cells where a full canvas chart would be overkill. Colored by trend
// (red/green/neutral, first-vs-last) when `trendColor` is set; otherwise a
// fixed `color` or `currentColor`. Series math lives in ./sparkline-math.
import {
  DRAW_POINTS,
  movingAverage,
  resample,
  SMOOTH_WINDOW,
  smoothPath,
  TREND_COLOR,
  trendDirection,
} from "./sparkline-math";

export type SparklineMode = "line" | "area";

export type SparklineProps = {
  data: number[];
  mode?: SparklineMode;
  /** Colors the line by trend direction (red=up, green=down, neutral=flat). */
  trendColor?: boolean;
  /** Fixed color override, used when `trendColor` is false. Defaults to `currentColor`. */
  color?: string;
  width?: number;
  height?: number;
  strokeWidth?: number;
};

export function Sparkline({
  data,
  mode = "line",
  trendColor = false,
  color,
  width = 120,
  height = 32,
  strokeWidth = 1.5,
}: SparklineProps): JSX.Element | null {
  const usable = data.filter((v) => Number.isFinite(v));
  if (usable.length < 2 || usable.every((v) => v === 0)) {
    return <span className="text-muted-foreground/50 text-xs">—</span>;
  }

  // Smooth out noise, then collapse to a few averaged control points so the
  // curve through them is gentle and never spiky. Computed on the finite-
  // filtered `usable` series — a single NaN/Infinity in the raw input would
  // otherwise propagate into NaN path coordinates and blank the sparkline.
  const series = resample(movingAverage(usable, SMOOTH_WINDOW), DRAW_POINTS);

  const pad = strokeWidth + 0.5;
  const min = Math.min(...series);
  const max = Math.max(...series);
  const span = max - min || 1;
  const innerW = width - pad * 2;
  const innerH = height - pad * 2;
  const baseline = height - pad;

  const pts = series.map((v, i) => ({
    x: pad + (i / (series.length - 1)) * innerW,
    y: pad + innerH - ((v - min) / span) * innerH,
  }));

  const lineColor = trendColor
    ? TREND_COLOR[trendDirection(usable)]
    : (color ?? "currentColor");
  const linePath = smoothPath(pts);
  const areaPath =
    mode === "area"
      ? `${linePath} L ${pts[pts.length - 1]!.x.toFixed(1)},${baseline.toFixed(1)} L ${pts[0]!.x.toFixed(1)},${baseline.toFixed(1)} Z`
      : null;

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      fill="none"
      aria-hidden="true"
      className="overflow-visible"
    >
      {areaPath && (
        <path d={areaPath} fill={lineColor} fillOpacity={0.16} stroke="none" />
      )}
      <path
        d={linePath}
        stroke={lineColor}
        strokeWidth={strokeWidth}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
