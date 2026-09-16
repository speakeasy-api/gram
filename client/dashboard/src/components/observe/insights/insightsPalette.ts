/**
 * One palette for the whole insights board.
 *
 * The ranked cards and the trend chart were drawing from different sets — the
 * cards from saturated hues at low alpha, the chart from the generic series
 * colours, which led with black and pure blue. Two palettes on one screen read
 * as two unrelated products, so both now come from here.
 *
 * The hues are deliberately desaturated: a card's fill sits under its own
 * labels, and a stacked chart puts ten of them side by side. At full
 * saturation that is a set of highlighter pens; muted, it stays a chart.
 */
export const INSIGHT_HUES = {
  blue: "#5b83a8",
  violet: "#8779ad",
  teal: "#4f938c",
  rose: "#bf6274",
  olive: "#7d9459",
  amber: "#b08a55",
  plum: "#96688a",
  steel: "#6b7f96",
  clay: "#c08268",
  sage: "#6f9b7e",
} as const;

/** One hue per card, so the board reads as six answers rather than one list. */
export const CARD_COLORS = {
  servers: INSIGHT_HUES.blue,
  tools: INSIGHT_HUES.violet,
  clients: INSIGHT_HUES.teal,
  errors: INSIGHT_HUES.rose,
  skills: INSIGHT_HUES.olive,
  people: INSIGHT_HUES.amber,
} as const;

/**
 * Series colours for the trend chart, ordered so neighbouring stack segments
 * stay distinguishable. Starts on the same blue the servers card uses, since
 * the chart is stacked by server.
 */
export const INSIGHT_SERIES_COLORS = [
  INSIGHT_HUES.blue,
  INSIGHT_HUES.violet,
  INSIGHT_HUES.teal,
  INSIGHT_HUES.amber,
  INSIGHT_HUES.plum,
  INSIGHT_HUES.olive,
  INSIGHT_HUES.clay,
  INSIGHT_HUES.steel,
  INSIGHT_HUES.sage,
  INSIGHT_HUES.rose,
] as const;

/** Everything past the palette folds into one muted grey rather than cycling. */
export const INSIGHT_OTHER_COLOR = "#b9bcc0";
