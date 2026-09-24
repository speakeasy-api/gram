import { createContext, useContext, useMemo, useState } from "react";

// Shared hover/color link between a stacked chart and the table that lists the
// same series. The chart panel publishes how it painted each stack and which
// stack the pointer is over; the table keys its row swatches and its dimming
// to that, so a bar segment and its row are visibly the same thing.
//
// Series identity is the panel's dataset key (TimeSeriesStack.key, falling
// back to its label) — never the display label alone, so a table row and a
// chart stack only link when the caller derived both from the same value.
//
// Consumers are optional on both ends: the panel keeps its own local focus
// state when no provider is above it, and a table outside a provider simply
// renders no swatches.

// How one series is painted. `outline` marks the hollow treatment used for an
// "unset" series — a box drawn in the color rather than filled with it.
export type SeriesStyle = { color: string; outline?: boolean };

export type SeriesFocus = {
  /** Paint style per series key, as resolved for the current theme. */
  styleByKey: Map<string, SeriesStyle>;
  /** The series the pointer is over, or null. */
  focusKey: string | null;
  setFocusKey: (key: string | null) => void;
  setStyleByKey: (styles: Map<string, SeriesStyle>) => void;
};

export const SeriesFocusContext = createContext<SeriesFocus | null>(null);

// The provider's value. Held by whichever component owns both the chart and
// the table (they are siblings, so neither can own it alone).
export function useSeriesFocusState(): SeriesFocus {
  const [focusKey, setFocusKey] = useState<string | null>(null);
  const [styleByKey, setStyleByKey] = useState<Map<string, SeriesStyle>>(
    () => new Map(),
  );
  return useMemo(
    () => ({ styleByKey, focusKey, setFocusKey, setStyleByKey }),
    [styleByKey, focusKey],
  );
}

export function useSeriesFocus(): SeriesFocus | null {
  return useContext(SeriesFocusContext);
}
