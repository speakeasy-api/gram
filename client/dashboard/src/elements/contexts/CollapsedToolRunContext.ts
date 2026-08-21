import { createContext, useContext } from "react";

/**
 * Whether the tool call being rendered sits inside a collapsed run.
 *
 * A run's collapsible exists to hide the mechanics of a turn, so a card that
 * renders an answer belongs outside it — `ToolGroup` hoists a run whose every
 * call draws one. When it can't (a widget batched with plain tool calls, which
 * share a single wrapper), the widget would otherwise render inside the
 * disclosure, where the group's children stay mounted and merely hidden. A
 * built-in reads this and falls back to the generic tool card instead, so the
 * catalog is a thing the user is shown, never a thing they find by expanding a
 * run.
 *
 * Defaults to false: a tool part rendered outside any group, or under a host's
 * own `ToolGroup` override, keeps its card.
 */
const CollapsedToolRunContext = createContext(false);

export const CollapsedToolRunProvider = CollapsedToolRunContext.Provider;

export function useIsCollapsedToolRun(): boolean {
  return useContext(CollapsedToolRunContext);
}
