import { useQuery } from "@tanstack/react-query";
import type { JSX } from "react";

import { organizationsStatsQuery } from "@/lib/adminQueries";
import type { AdminOrganizationStats, TrialState } from "@/lib/gramAdminApi";
import {
  NO_FILTERS,
  type DisabledState,
  type FilterSelection,
} from "@/lib/organizationFilters";

import { useApplyFilters } from "./applyFilters";

// Typed as the states themselves rather than left as bare strings. Both readers
// drop a value they do not recognise, so a misspelling here would not filter on
// the wrong thing, it would silently filter on nothing: the cell would clear
// the filters and read as though it had worked.
const ENDING_SOON: TrialState = "ending_soon";
const DISABLED: DisabledState = "disabled";

// What is shown where the figures have not arrived. A digit that turns out to
// be wrong is worse than a dash, and the cells stay usable either way: which
// filter a cell applies does not depend on the count it is showing.
const NO_FIGURE = "—";

type StatCell = {
  label: string;
  value: (stats: AdminOrganizationStats) => number;
  /** Absent where the cell has no second line. */
  subLine?: (stats: AdminOrganizationStats) => string;
  /** Applied whole, so the two filters a cell does not name are cleared. */
  filters: FilterSelection;
};

const STAT_CELLS: StatCell[] = [
  {
    label: "Organizations",
    value: (stats) => stats.total,
    subLine: (stats) => `${figure(stats.created_last_7_days)} new this week`,
    filters: NO_FILTERS,
  },
  {
    // No sub-line. The design's "N with no owner" is cut: Gram has no concept
    // of an organization owner to count.
    label: "Trials ending in 7 days",
    value: (stats) => stats.trials_ending_soon,
    filters: { ...NO_FILTERS, trial: [ENDING_SOON] },
  },
  {
    label: "Disabled",
    value: (stats) => stats.disabled,
    subLine: (stats) => `${figure(stats.disabled_last_7_days)} this week`,
    filters: { ...NO_FILTERS, disabled: [DISABLED] },
  },
];

function figure(value: number): string {
  return value.toLocaleString();
}

/**
 * How much work is waiting, across the whole platform, with each figure a way
 * into the rows behind it.
 */
export function StatStrip(): JSX.Element {
  const applyFilters = useApplyFilters();

  // Deliberately not given the list's filters, and the query takes none: these
  // figures describe the platform rather than the current view. Passing the
  // filters would make the strip refetch on every filter change and report the
  // rows already on screen, which is a count the operator can read off the
  // table.
  const { data } = useQuery(organizationsStatsQuery);

  return (
    <div
      // Named, because the three figures only mean something together and a
      // screen reader arriving on one cell is otherwise told three numbers with
      // no account of what they are counting.
      role="group"
      aria-label="Platform totals"
      className="mb-2 grid grid-cols-3 divide-x rounded-lg border"
    >
      {STAT_CELLS.map((cell) => (
        <button
          key={cell.label}
          type="button"
          onClick={() => applyFilters(cell.filters)}
          className="flex flex-col items-start gap-0.5 px-4 py-3 text-left transition-colors first:rounded-l-lg last:rounded-r-lg hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          <span className="text-muted-foreground text-xs">{cell.label}</span>
          <span className="text-2xl font-semibold tabular-nums">
            {data ? figure(cell.value(data)) : NO_FIGURE}
          </span>
          {cell.subLine && data ? (
            <span className="text-muted-foreground text-xs">
              {cell.subLine(data)}
            </span>
          ) : null}
        </button>
      ))}
    </div>
  );
}
