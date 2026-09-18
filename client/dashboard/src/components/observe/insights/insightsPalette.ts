/**
 * The insights board's colors, taken from the shared chart palette.
 *
 * This file used to carry a hue set of its own — desaturated, close in spirit
 * to the shared one, but a separate list all the same. That is the same "two
 * palettes read as two products" problem it was written to solve, one level
 * up: the Costs board and this one sat side by side in the same nav and did
 * not match. Both now draw from `@/components/chart/palette`, so a change
 * there reaches every chart in the dashboard, and this board follows the
 * theme (the old literals were light-mode only).
 */
import {
  useOtherSeriesColor,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";

export type InsightCardKey =
  | "servers"
  | "tools"
  | "clients"
  | "errors"
  | "skills"
  | "people";

/**
 * One palette slot per card, so the board reads as six answers rather than one
 * list. Servers take slot 0 because the trend chart is stacked by server and
 * leads on the same color; errors take the rose slot, the nearest thing to a
 * warning tone in a ramp that reserves red for actual risk.
 */
const CARD_SLOT: Record<InsightCardKey, number> = {
  servers: 0,
  errors: 1,
  skills: 2,
  tools: 3,
  people: 4,
  clients: 5,
};

export function useCardColors(): Record<InsightCardKey, string> {
  const series = useSeriesColors();
  const at = (slot: number): string => series[slot % series.length]!;
  return {
    servers: at(CARD_SLOT.servers),
    tools: at(CARD_SLOT.tools),
    clients: at(CARD_SLOT.clients),
    errors: at(CARD_SLOT.errors),
    skills: at(CARD_SLOT.skills),
    people: at(CARD_SLOT.people),
  };
}

/**
 * Series colors for the trend chart. The shared ramp in its own order, which
 * starts on the same blue the servers card uses — the chart is stacked by
 * server, so the two agree on their leading color.
 */
export function useInsightSeriesColors(): string[] {
  return useSeriesColors();
}

/** Everything past the ramp folds into one muted neutral rather than cycling. */
export function useInsightOtherColor(): string {
  return useOtherSeriesColor();
}
