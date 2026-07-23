import { Dimension } from "@gram/client/models/components/queryfilter.js";
import {
  type Crumb,
  friendlyName,
  isSessionsAxis,
  LABELS,
  pluralLabel,
} from "./taxonomy";

// The breakdown section's title + caption. A non-component module so the strings
// stay unit-testable (EntityProfile may only export components) — the grammar
// here has to survive every dimension label in the taxonomy and every drill
// depth, so it's covered by a table-driven test rather than spot-checked.
//
// Cost arrives preformatted (lib/money's formatCost): this module's job is
// grammar, not money.

/**
 * Title the breakdown table by the cut in view ("Cost by Model") rather than by
 * the mechanism ("Breakdown by"): the title echoes whichever axis is lit in the
 * track beside it, which is what teaches the idea on the first click.
 */
export function breakdownTitle(axisValue: string, groupBy: Dimension): string {
  if (isSessionsAxis(axisValue)) return "Agent sessions";
  return `Cost by ${LABELS[groupBy] ?? "group"}`;
}

// "Adam" → "Adam's", "Operations" → "Operations'". English drops the extra s on
// a name that already ends in one.
function possessive(name: string): string {
  return /s$/i.test(name) ? `${name}'` : `${name}'s`;
}

/**
 * Name the slice the breakdown is splitting, following the drill path:
 *
 *   []                              → "all project spend"
 *   [Adam]                          → "Adam's spend"
 *   [Adam, claude-code]             → "Adam's claude-code spend"
 *   [R&D, Engineering, Adam, cc]    → "R&D's Engineering's Adam's cc spend"
 *
 * Every ancestor possesses the one below it; the deepest reads as a modifier of
 * `noun` ("claude-code spend"), except at depth 1 where there is nothing between
 * it and the noun to modify, so it possesses instead ("Adam's spend").
 *
 * `noun` is what's being counted — "spend" for a cost breakdown, "sessions" for
 * the session list, which can't be "spend" without reading as nonsense.
 */
export function scopePhrase(path: Crumb[], noun: string): string {
  if (path.length === 0) return `all project ${noun}`;
  const names = path.map((c) => friendlyName(c.dim, c.value));
  const last = names[names.length - 1]!;
  if (names.length === 1) return `${possessive(last)} ${noun}`;
  const owners = names.slice(0, -1).map(possessive).join(" ");
  return `${owners} ${last} ${noun}`;
}

/**
 * Narrate the cut in the user's own numbers and their own drill path ("Showing
 * Adam's claude-code spend — $4.04 across 2 Models."), as the title's
 * description.
 *
 * Deliberately concrete: a general definition of "breakdown" reads as jargon,
 * because the idea only lands against the slice actually on screen. Naming the
 * path and the split together is what a tooltip nobody hovers can't do.
 */
export function breakdownCaption({
  axisValue,
  groupBy,
  path,
  costLabel,
  groupCount,
}: {
  axisValue: string;
  groupBy: Dimension;
  // The full drill path, root → entity in view. Empty at the project root.
  path: Crumb[];
  // The slice's total spend, already formatted (e.g. "$4.04").
  costLabel: string;
  groupCount: number;
}): string {
  // The sessions axis lists rather than groups; say so, since that contrast is
  // what makes the other axes legible as breakdowns.
  if (isSessionsAxis(axisValue)) {
    return `Showing ${scopePhrase(path, "sessions")}, listed individually.`;
  }
  const scope = scopePhrase(path, "spend");
  const groupLabel = LABELS[groupBy] ?? "group";
  // Loading and empty slices have no count or total to quote yet, so describe
  // the cut without asserting "$0.00 across 0 …".
  if (groupCount === 0) {
    return `Showing ${scope}, broken down by ${groupLabel}.`;
  }
  // A single group isn't a split — the active axis is offered even when it has
  // only one value (see pivotOptions), so say what that actually means.
  if (groupCount === 1) {
    return `Showing ${scope} — ${costLabel}, all from a single ${groupLabel}.`;
  }
  return `Showing ${scope} — ${costLabel} across ${groupCount} ${pluralLabel(groupBy)}.`;
}
