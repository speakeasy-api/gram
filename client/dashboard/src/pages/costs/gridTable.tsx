import { cn } from "@/lib/utils";
import { ChevronDown, ChevronsUpDown, ChevronUp } from "lucide-react";

// Shared layout primitives for the costs-page grid tables (CostTable, SessionTable).
//
// One parent grid owns the column tracks; the header and every row span all of
// them (`col-span-full` → grid-column: 1 / -1) and re-use them via
// `grid-cols-subgrid` (grid-template-columns: subgrid). Every column therefore
// auto-sizes to the widest cell across the whole table and stays aligned, with
// no per-row hardcoded widths — so the table fits its container instead of
// overflowing horizontally. Apply alongside `grid` on each subgrid row.
export const SUBGRID_ROW_CLASS = "col-span-full grid-cols-subgrid";

// Empty cell occupying an edge gutter track (keeps row hover + dividers
// full-bleed while content columns stay inset).
export function Gutter(): JSX.Element {
  return <span aria-hidden="true" />;
}

export type SortDir = "asc" | "desc";

// A sortable column header: label + a tri-state chevron (inactive ⇅ / asc ↑ /
// desc ↓). Presentational only — callers own the sort state and toggling.
export function SortHeader({
  label,
  active,
  dir,
  onClick,
}: {
  label: string;
  active: boolean;
  dir: SortDir;
  onClick: () => void;
}): JSX.Element {
  let arrow = (
    <ChevronsUpDown className="text-muted-foreground/40 group-hover:text-foreground size-3.5" />
  );
  if (active) {
    arrow =
      dir === "asc" ? (
        <ChevronUp className="size-3.5" />
      ) : (
        <ChevronDown className="size-3.5" />
      );
  }
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "group hover:text-foreground inline-flex items-center gap-1 whitespace-nowrap transition-colors",
        active && "text-foreground",
      )}
    >
      {label}
      {arrow}
    </button>
  );
}
