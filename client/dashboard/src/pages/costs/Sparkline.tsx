// A tiny GitHub-pulse-style sparkline of a value series (e.g. daily cost over
// the last month). Coloured by trend: red when rising (cost up is bad), green
// when falling, neutral grey when there's no clear trend. Pure SVG, no chart lib.
// The series math + trend logic lives in ./sparkline-math.
import {
  DRAW_POINTS,
  movingAverage,
  resample,
  SMOOTH_WINDOW,
  smoothPath,
  TREND_COLOR,
  trendDirection,
} from "./sparkline-math";

export function Sparkline({
  values,
  width = 96,
  height = 28,
  color: fixedColor,
}: {
  values: number[];
  width?: number;
  height?: number;
  // Force a colour (e.g. neutral for usage metrics); omit to colour by trend.
  color?: string;
}): JSX.Element | null {
  const usable = values.filter((v) => Number.isFinite(v));
  if (usable.length < 2 || usable.every((v) => v === 0)) {
    return <span className="text-muted-foreground/50 text-xs">—</span>;
  }

  // Smooth out daily noise, then collapse to a few averaged control points so
  // the curve through them is gentle and never spiky.
  const series = resample(movingAverage(values, SMOOTH_WINDOW), DRAW_POINTS);

  const pad = 2;
  const min = Math.min(...series);
  const max = Math.max(...series);
  const span = max - min || 1;
  const innerW = width - pad * 2;
  const innerH = height - pad * 2;

  const pts = series.map((v, i) => ({
    x: pad + (i / (series.length - 1)) * innerW,
    y: pad + innerH - ((v - min) / span) * innerH,
  }));

  const color = fixedColor ?? TREND_COLOR[trendDirection(values)];

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      fill="none"
      aria-hidden="true"
      className="overflow-visible"
    >
      <path
        d={smoothPath(pts)}
        stroke={color}
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
