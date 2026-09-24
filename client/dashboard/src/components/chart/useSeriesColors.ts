import { useIsDarkTheme } from "@/lib/theme";
import {
  otherSeriesForTheme,
  seriesForTheme,
  type Trend,
  trendForTheme,
} from "./palette";

export { useIsDarkTheme } from "@/lib/theme";

// The categorical series ramp for the resolved theme. Chart.js paints to
// canvas and cannot follow the CSS theme, so chart components resolve the
// theme here (the same source they already use for AXIS.grid vs
// AXIS.gridDark) and re-render with the lifted dark ramp when it flips.
// Returns one of two stable module-constant arrays, so it is safe to use
// directly in memo dependency lists.
export function useSeriesColors(): string[] {
  return seriesForTheme(useIsDarkTheme());
}

// The neutral top-N "Other" rollup color for the resolved theme, so the fold
// recedes behind the named series on both canvases.
export function useOtherSeriesColor(): string {
  return otherSeriesForTheme(useIsDarkTheme());
}

// The up/down/flat trend trio for the resolved theme. Trend colors annotate
// SVG strokes and inline figures rather than CSS-styled text, so they can't
// follow the theme through a token and are resolved here like the series ramp.
export function useTrendColors(): Trend {
  return trendForTheme(useIsDarkTheme());
}
