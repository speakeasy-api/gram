// THE shared editorial chart palette. Every dashboard chart draws from these
// tokens so charts read as one system: ink + stepped warm neutrals for
// categorical series, ONE red accent reserved for risk/error/attention, and a
// muted green for "good" (falling cost, passing checks). No default blue, no
// rainbow ramps, no gradient/alpha area fills.
//
// Values are derived from the design tokens in
// `@/components/ui/styles/base.css`: brand red 500, brand green 600, the
// feedback-orange scale, and the neutral ramp. They are inlined as literal
// color strings because Chart.js paints to canvas and cannot resolve CSS
// custom properties.

// Brand red — base.css `--color-brand-red-500` hsl(4, 67%, 47%). Reserved for
// risk / error / attention; never a generic series hue.
export const ACCENT_RED = "hsl(4, 67%, 47%)";

// Muted green — base.css `--color-brand-green-600` hsl(107, 24%, 34%).
// "Good": costs trending down, successful outcomes.
export const GOOD_GREEN = "hsl(107, 24%, 34%)";

// Categorical series ramp, in fixed slot order — a series takes the slot its
// rank gives it and keeps that color; the ramp is never cycled or regenerated.
// Three constraints shaped it:
//
//   - No ink and no near-blacks. A near-black bar segment reads as a hole in
//     the stack rather than as a category, and it takes over every chart it
//     appears in.
//   - Muted, in the register of the cost cards above the chart (their spend
//     meters grade forest green → gray → brick). A saturated ramp made the
//     same page look like two products; these hues sit a step back, close to
//     that grading's tone without borrowing its meaning.
//   - Adjacent slots must stay tellable apart under color-vision deficiency,
//     not just to full-color readers. The ORDER is what secures that, so it
//     is not cosmetic: reordering the slots invalidates it. Muting fights
//     this directly — these values are as desaturated as the gates allow, and
//     pushing them further makes neighbouring slots merge.
//
// Both columns are validated as sets against their own surface (adjacent-pair
// CVD ΔE ≥ 8, normal-vision ΔE ≥ 15, chroma floor, lightness band). Light
// passes every gate (worst adjacent 8.9 CVD / 22.1 normal). Dark passes all
// but the teal↔plum pair, which lands at 6.8 CVD — inside the band that is
// legal only with a secondary encoding, which this chart always has: a
// permanent legend, and a table below whose rows carry the same labelled
// swatches. Re-run the numbers before changing a value or the order.
//
// Slot 0 also paints the panel's optional total line, so it stays the
// strongest, most brand-primary hue.
export const SERIES: string[] = [
  "#33699f", // blue
  "#c76b8b", // rose
  "#2f7038", // forest
  "#6f86d6", // periwinkle
  "#a8582f", // terracotta
  "#1f9a94", // teal
  "#7f4a8c", // plum
];

// Dark-surface counterpart of SERIES, index-aligned so a series keeps its hue
// (and legend identity) across themes. These are the same seven hues stepped
// for the dark canvas and validated as their own set — not an automatic flip
// of the light column.
const SERIES_DARK: string[] = [
  "#4f86bd", // blue
  "#c76b8b", // rose
  "#4a8c52", // forest
  "#7185d2", // periwinkle
  "#c06e42", // terracotta
  "#1f9a94", // teal
  "#b566c4", // plum
];

// The categorical ramp for the resolved theme. Chart.js paints to canvas and
// cannot follow the CSS theme, so callers resolve the theme themselves (the
// same way they already branch AXIS.grid vs AXIS.gridDark) and pass it here.
export function seriesForTheme(isDark: boolean): string[] {
  return isDark ? SERIES_DARK : SERIES;
}

// Light neutral for top-N "Other" rollup series — legible against the white
// canvas but still receding behind the named series.
export const OTHER_SERIES = "hsl(0, 0%, 78%)";

// Dark-surface counterpart: a mid-gray that recedes on a dark canvas the way
// OTHER_SERIES does on light.
const OTHER_SERIES_DARK = "hsl(0, 0%, 33%)";

// The rollup neutral for the resolved theme, mirroring seriesForTheme —
// Chart.js paints to canvas, so callers resolve the theme and pass it here.
export function otherSeriesForTheme(isDark: boolean): string {
  return isDark ? OTHER_SERIES_DARK : OTHER_SERIES;
}

// Severity bands: critical = brand red, high = feedback-orange-600,
// medium = feedback-orange-400, low/info = neutral (never blue).
export const SEVERITY = {
  critical: ACCENT_RED,
  high: "hsl(22, 70%, 53%)", // base.css feedback-orange-600
  medium: "hsl(27, 100%, 66%)", // base.css feedback-orange-400
  low: "hsl(0, 0%, 46%)", // neutral
} as const;

// Trend direction for cost/risk series: going UP is bad (red), DOWN is good
// (green), no clear direction is neutral. A step darker than the accent reds/
// greens — trend deltas annotate nearly every stat tile, so at full accent
// strength they shout.
export type Trend = { up: string; down: string; flat: string };

export const TREND: Trend = {
  up: "hsl(2, 65%, 39%)", // feedback-red-600
  down: "hsl(107, 24%, 27%)", // feedback-green-700
  flat: "hsl(0, 0%, 59%)",
};

// Dark-surface counterpart. The light tokens are deliberately a step darker
// than the accent red/green, which works against paper but sinks into a dark
// canvas — a trend delta or a graded cost figure has to stay readable, so both
// poles step up into the accent band. The neutral lightens with them.
const TREND_DARK: Trend = {
  up: "hsl(4, 76%, 63%)",
  down: "hsl(107, 32%, 56%)",
  flat: "hsl(0, 0%, 66%)",
};

// The trend trio for the resolved theme, mirroring seriesForTheme.
export function trendForTheme(isDark: boolean): Trend {
  return isDark ? TREND_DARK : TREND;
}

// Chart.js tooltip style: near-black square card (editorial print — no
// rounded corners). Spread into `plugins.tooltip` alongside callbacks.
export const TOOLTIP = {
  backgroundColor: "#171717",
  titleColor: "#fafafa",
  bodyColor: "#d4d4d4",
  borderColor: "#262626",
  borderWidth: 1,
  cornerRadius: 0,
  boxPadding: 4,
} as const;

// Axis label and hairline gridline colors. `grid` is for light surfaces;
// `gridDark` for dark ones (Chart.js canvases can't follow the CSS theme, so
// callers pick based on the resolved theme).
export const AXIS = {
  label: "hsl(0, 0%, 46%)",
  faded: "hsl(0, 0%, 59%)",
  grid: "rgba(0, 0, 0, 0.06)",
  gridDark: "rgba(255, 255, 255, 0.08)",
} as const;

// An alpha variant of a palette color, for hover/dim states. Handles the two
// formats the palette uses: `hsl(...)` strings and 6-digit hex.
export function withAlpha(color: string, alpha: number): string {
  if (color.startsWith("hsl(")) {
    return color.replace(/^hsl\(/, "hsla(").replace(/\)$/, `, ${alpha})`);
  }
  if (color.startsWith("#") && color.length === 7) {
    const suffix = Math.round(alpha * 255)
      .toString(16)
      .padStart(2, "0");
    return `${color}${suffix}`;
  }
  return color;
}
