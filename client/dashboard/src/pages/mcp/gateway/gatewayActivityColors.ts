import {
  otherSeriesForTheme,
  seriesForTheme,
} from "@/components/chart/palette";
import type { GatewayMode } from "./gatewayActivity";

// Each mode keeps one categorical slot on every chart in the section, so a
// mode reads the same in the tiles, the session chart, and the filter. Slots
// 0–2 are adjacent in the validated ramp; calls with no recorded mode take the
// receding "other" gray.
const MODE_SLOTS: Record<Exclude<GatewayMode, "unrecorded">, number> = {
  progressive: 0,
  code_mode: 1,
  direct: 2,
};

export function gatewayModeColor(mode: GatewayMode, isDark: boolean): string {
  if (mode === "unrecorded") return otherSeriesForTheme(isDark);
  return seriesForTheme(isDark)[MODE_SLOTS[mode]]!;
}

// Lookups and tool calls take slots 4 and 5, apart from the mode slots, so a
// bar is never mistaken for a mode. The pair passes the palette checks on both
// surfaces (CVD ΔE 13.1 light, 12.1 dark).
export function gatewayPhaseColors(isDark: boolean): {
  discovery: string;
  execution: string;
} {
  const series = seriesForTheme(isDark);
  return { discovery: series[4]!, execution: series[5]! };
}

/** A legend entry: a square swatch beside its label, for HTML legends. */
export interface LegendItem {
  label: string;
  color: string;
}
