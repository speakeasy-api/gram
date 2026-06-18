// A tiny GitHub-pulse-style sparkline of a value series (e.g. daily cost over
// the last month). Coloured by trend: red when rising (cost up is bad), green
// when falling, neutral grey when there's no clear trend. Pure SVG, no chart lib.

const UP_COLOR = "#f43f5e"; // rising cost → rose-500
const DOWN_COLOR = "#10b981"; // falling cost → emerald-500
const FLAT_COLOR = "#94a3b8"; // no clear trend → grey

// Moving-average window applied before drawing / trend detection.
const SMOOTH_WINDOW = 11;
// Number of averaged control points the curve is drawn through (fewer = softer).
const DRAW_POINTS = 9;

function mean(xs: number[]): number {
  if (xs.length === 0) return 0;
  return xs.reduce((a, b) => a + b, 0) / xs.length;
}

// Net change end-to-start (smoothed last-third mean − first-third mean). Used
// for ranking the trend column; sign alone isn't enough to colour (see below).
export function trendOf(values: number[]): number {
  const s = movingAverage(values, SMOOTH_WINDOW);
  const n = s.length;
  if (n < 2) return 0;
  const seg = Math.max(1, Math.round(n / 3));
  return mean(s.slice(n - seg)) - mean(s.slice(0, seg));
}

type TrendDirection = "up" | "down" | "flat";

// A trend reads as "up" only when the line both ended clearly above where it
// started AND isn't falling away in its final portion (a plateau at the top
// still counts; a clear end-decline does not). "down" is the mirror. Anything
// mixed — rose-then-fell, recovered, choppy, or barely moved — is "flat". All
// thresholds are relative to the line's own range. Computed on the smoothed
// series.
export function trendDirection(values: number[]): TrendDirection {
  const s = movingAverage(values, SMOOTH_WINDOW);
  const n = s.length;
  if (n < 3) return "flat";
  const seg = Math.max(1, Math.round(n / 3));
  const firstMean = mean(s.slice(0, seg));
  const lastMean = mean(s.slice(n - seg));
  const middle = s.slice(seg, n - seg);
  const midMean = middle.length ? mean(middle) : firstMean;

  const overall = lastMean - firstMean; // end vs start
  const recent = lastMean - midMean; // final-portion direction
  const dead = 0.12 * (Math.max(...s) - Math.min(...s) || 1);

  if (overall > dead && recent > -dead) return "up";
  if (overall < -dead && recent < dead) return "down";
  return "flat";
}

const TREND_COLOR: Record<TrendDirection, string> = {
  up: UP_COLOR,
  down: DOWN_COLOR,
  flat: FLAT_COLOR,
};

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

// Centered moving average to tame daily spikes before drawing.
export function movingAverage(values: number[], window: number): number[] {
  if (values.length <= 2) return values;
  const half = Math.floor(window / 2);
  return values.map((_, i) => {
    let sum = 0;
    let count = 0;
    for (let j = i - half; j <= i + half; j++) {
      if (j >= 0 && j < values.length) {
        sum += values[j] ?? 0;
        count += 1;
      }
    }
    return count ? sum / count : 0;
  });
}

// Collapse a series into k evenly-spaced averaged buckets.
export function resample(values: number[], k: number): number[] {
  if (values.length <= k) return values;
  const out: number[] = [];
  for (let i = 0; i < k; i++) {
    const start = Math.floor((i * values.length) / k);
    const end = Math.max(start + 1, Math.floor(((i + 1) * values.length) / k));
    out.push(mean(values.slice(start, end)));
  }
  return out;
}

// A smooth curve through the points via Catmull-Rom → cubic-bezier conversion.
export function smoothPath(pts: { x: number; y: number }[]): string {
  if (pts.length < 2) return "";
  const d = [`M ${pts[0]!.x.toFixed(1)},${pts[0]!.y.toFixed(1)}`];
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i - 1] ?? pts[i]!;
    const p1 = pts[i]!;
    const p2 = pts[i + 1]!;
    const p3 = pts[i + 2] ?? p2;
    const cp1x = p1.x + (p2.x - p0.x) / 6;
    const cp1y = p1.y + (p2.y - p0.y) / 6;
    const cp2x = p2.x - (p3.x - p1.x) / 6;
    const cp2y = p2.y - (p3.y - p1.y) / 6;
    d.push(
      `C ${cp1x.toFixed(1)},${cp1y.toFixed(1)} ${cp2x.toFixed(1)},${cp2y.toFixed(1)} ${p2.x.toFixed(1)},${p2.y.toFixed(1)}`,
    );
  }
  return d.join(" ");
}
