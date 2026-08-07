import { useContext } from "react";
import { ConfigContext } from "@/components/ui/context/config";
import { seriesForTheme } from "./palette";

// The categorical series ramp for the resolved theme. Chart.js paints to
// canvas and cannot follow the CSS theme, so chart components resolve the
// theme here (the same source they already use for AXIS.grid vs
// AXIS.gridDark) and re-render with the lifted dark ramp when it flips.
// Returns one of two stable module-constant arrays, so it is safe to use
// directly in memo dependency lists.
//
// Reads the context optionally (rather than via useConfig, which throws
// without a ConfigProvider) so charts render with the light ramp in bare
// test/storybook mounts.
export function useSeriesColors(): string[] {
  const config = useContext(ConfigContext);
  return seriesForTheme(config?.theme === "dark");
}
