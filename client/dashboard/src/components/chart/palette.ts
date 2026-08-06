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

// Categorical series ramp: ink first, then the muted brand-rainbow hues from
// base.css (`--gradient-brand-primary-colors` — go/java/terraform/ruby/php/
// python/unity), closing on neutrals. Adjacent stacked segments must be
// tellable apart at a glance, which pure lightness steps couldn't do; the
// brand hues keep it editorial rather than default-chart-library bright.
export const SERIES: string[] = [
  "hsl(0, 0%, 7%)", // ink
  "hsl(215, 71%, 40%)", // brand blue 600 (java, muted a step)
  "hsl(108, 24%, 41%)", // brand green 500 (terraform)
  "hsl(23, 96%, 62%)", // brand orange (ruby)
  "hsl(220, 100%, 12%)", // brand navy (go)
  "hsl(68, 52%, 72%)", // brand chartreuse (unity)
  "hsl(334, 54%, 13%)", // brand maroon (php)
  "hsl(216, 100%, 80%)", // brand ice blue (python)
  "hsl(0, 0%, 59%)", // neutral tail
];

// Lightest neutral for top-N "Other" rollup series, so the fold recedes
// behind the named series.
export const OTHER_SERIES = "hsl(0, 0%, 86%)";

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
export const TREND = {
  up: "hsl(2, 65%, 39%)", // feedback-red-600
  down: "hsl(107, 24%, 27%)", // feedback-green-700
  flat: "hsl(0, 0%, 59%)",
} as const;

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
