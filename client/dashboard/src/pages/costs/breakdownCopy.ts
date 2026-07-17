import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { isSessionsAxis, LABELS, pluralLabel } from "./taxonomy";

// The breakdown section's title + caption. A non-component module so the strings
// stay unit-testable (EntityProfile may only export components) — the grammar
// here has to survive every dimension label in the taxonomy, so it's covered by
// a table-driven test rather than spot-checked in the browser.
//
// Cost arrives preformatted: five formatCost copies already exist across the
// cost views, and this module's job is grammar, not money.

/**
 * Title the breakdown table by the cut in view ("Cost by Model") rather than by
 * the mechanism ("Breakdown by"): the title echoes whichever axis is lit in the
 * track beside it, which is what teaches the idea on the first click.
 */
export function breakdownTitle(axisValue: string, groupBy: Dimension): string {
  if (isSessionsAxis(axisValue)) return "Agent sessions";
  return `Cost by ${LABELS[groupBy] ?? "group"}`;
}

/**
 * Narrate the cut in the user's own numbers ("$12,340 split across 8 Job
 * Titles."), as the title's description. The power of a breakdown only lands
 * once you see one pot of spend divided into real groups, so this states both —
 * a tooltip nobody hovers can't.
 *
 * Deliberately unscoped: whose spend this is already reads off the hero above
 * (title + type badge), and no qualifier survives both halves of the taxonomy —
 * "$4.04 of R&D" is fine where "$4.04 of Adam" is not.
 */
export function breakdownCaption({
  axisValue,
  groupBy,
  costLabel,
  groupCount,
}: {
  axisValue: string;
  groupBy: Dimension;
  // The slice's total spend, already formatted (e.g. "$4.04").
  costLabel: string;
  groupCount: number;
}): string {
  // The sessions axis lists rather than groups; say so, since that contrast is
  // what makes the other axes legible as breakdowns.
  if (isSessionsAxis(axisValue)) {
    return "Every agent session, listed individually.";
  }
  const groupLabel = LABELS[groupBy] ?? "group";
  // Loading and empty slices have no count to quote yet, so describe the cut
  // without asserting "across 0 …".
  if (groupCount === 0) return `Splitting spend by ${groupLabel}.`;
  // A single group isn't a split — the active axis is offered even when it has
  // only one value (see pivotOptions), so say what that actually means.
  if (groupCount === 1) {
    return `${costLabel} — all from a single ${groupLabel}.`;
  }
  return `${costLabel} split across ${groupCount} ${pluralLabel(groupBy)}.`;
}
