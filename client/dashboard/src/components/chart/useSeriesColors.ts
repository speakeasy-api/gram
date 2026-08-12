import { useContext } from "react";
import { ConfigContext } from "@/components/ui/context/config";
import { otherSeriesForTheme, seriesForTheme } from "./palette";

// Whether the resolved theme is dark, read from the config context
// OPTIONALLY (rather than via useConfig, which throws without a
// ConfigProvider) so charts render with the light palette in bare
// test/storybook mounts.
export function useIsDarkTheme(): boolean {
  const config = useContext(ConfigContext);
  return config?.theme === "dark";
}

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
