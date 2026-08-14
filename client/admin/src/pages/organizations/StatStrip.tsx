import { useQuery } from "@tanstack/react-query";
import type { JSX } from "react";

import { organizationsStatsQuery } from "@/lib/adminQueries";
import {
  errorMessage,
  type AdminOrganizationStats,
  type TrialState,
} from "@/lib/gramAdminApi";
import {
  DISABLED_STATES,
  NO_FILTERS,
  type DisabledState,
  type FilterSelection,
} from "@/lib/organizationFilters";

import { useApplyFilters } from "./applyFilters";

// Typed, not bare strings: both readers drop a value they do not recognise, so
// a misspelling would filter on nothing rather than on the wrong thing.
const ENDING_SOON: TrialState = "ending_soon";
const DISABLED: DisabledState = "disabled";

// The correction two of these cells carry: an absent status filter means active
// only, and every figure here counts the disabled organizations too.
const EVERY_STATUS: DisabledState[] = [...DISABLED_STATES];

// A digit that turns out to be wrong is worse than a dash, and which filter a
// cell applies does not depend on the count it shows.
const NO_FIGURE = "—";

type StatCell = {
  label: string;
  value: (stats: AdminOrganizationStats) => number;
  /** Absent where the cell has no second line. */
  subLine?: (stats: AdminOrganizationStats) => string;
  /** Applied whole, so the filters a cell does not name are cleared. */
  filters: FilterSelection;
  /** What pressing does. The figure alone never says the cell is a control. */
  action: string;
};

const STAT_CELLS: StatCell[] = [
  {
    label: "Organizations",
    value: (stats) => stats.total,
    subLine: (stats) => `${figure(stats.created_last_7_days)} new this week`,
    filters: { ...NO_FILTERS, disabled: EVERY_STATUS },
    action: "Show every organization",
  },
  {
    // No sub-line: the design's "N with no owner" is cut, Gram has no owners.
    label: "Trials ending in 7 days",
    value: (stats) => stats.trials_ending_soon,
    filters: { ...NO_FILTERS, trial: [ENDING_SOON], disabled: EVERY_STATUS },
    action: "Show the trials ending in 7 days",
  },
  {
    label: "Disabled",
    value: (stats) => stats.disabled,
    subLine: (stats) => `${figure(stats.disabled_last_7_days)} this week`,
    filters: { ...NO_FILTERS, disabled: [DISABLED] },
    action: "Show the disabled organizations",
  },
];

// All five fields are Required in the Goa design, so one cannot go missing.
// Cheap insurance: nothing in this app catches a throw, so a page would go dark.
function figure(value: number | undefined): string {
  return typeof value === "number" ? value.toLocaleString() : NO_FIGURE;
}

/** How much work is waiting across the platform, each figure a way into it. */
export function StatStrip(): JSX.Element {
  const applyFilters = useApplyFilters();
  const { data, isPending, isError, error } = useQuery(organizationsStatsQuery);

  return (
    <>
      <div
        // Named: the three figures only mean something together, and a reader
        // arriving on one is otherwise told a number and not what it counts.
        role="group"
        aria-label="Platform totals"
        className="mb-2 grid grid-cols-3 divide-x rounded-lg border"
      >
        {STAT_CELLS.map((cell) => (
          <button
            key={cell.label}
            type="button"
            // Readable on demand rather than announced: the dash is not spoken
            // at default verbosity, so the cell is a label with no figure.
            aria-busy={isPending}
            onClick={() => applyFilters(cell.filters, { clearSearch: true })}
            className="flex flex-col items-start gap-0.5 px-4 py-3 text-left transition-colors first:rounded-l-lg last:rounded-r-lg hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
          >
            <span className="text-muted-foreground text-xs">{cell.label}</span>
            <span className="text-2xl font-semibold tabular-nums">
              {data ? figure(cell.value(data)) : NO_FIGURE}
            </span>
            {/* Empty, but holding its height: the toolbar and the table below
                would otherwise drop a line when the figures land. */}
            {cell.subLine ? (
              <span className="text-muted-foreground min-h-4 text-xs">
                {data ? cell.subLine(data) : null}
              </span>
            ) : null}
            <span className="sr-only">{cell.action}</span>
          </button>
        ))}
      </div>

      {/* Reported where the table reports its own failures. Without this a
          strip of dashes reads the same as a platform with nothing on it. */}
      {isError && (
        <div className="text-muted-foreground mb-2 text-sm">
          Could not load the platform totals: {errorMessage(error)}
        </div>
      )}
    </>
  );
}
