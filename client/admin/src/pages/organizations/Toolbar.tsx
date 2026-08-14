import { useNavigate, useSearch } from "@tanstack/react-router";
import type { Column, RowData } from "@tanstack/react-table";
import { SearchIcon } from "lucide-react";
import { useEffect, useRef, useState, type JSX } from "react";

import type {
  DataTableFeatures,
  DataTableInstance,
} from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  FILTER_GROUPS,
  filterSummary,
  filtersToSearch,
  optionsFor,
  type FilterGroupKey,
  type FilterSelection,
} from "@/lib/organizationFilters";
import { cn } from "@/lib/utils";
import type { OrganizationsSearch } from "@/routes/organizations.index";

import { FilterSheet } from "./FilterSheet";

const ROUTE_ID = "/organizations/";
const SEARCH_DEBOUNCE_MS = 300;

export function Toolbar(): JSX.Element {
  const search = useSearch({ from: ROUTE_ID });
  const navigate = useNavigate({ from: ROUTE_ID });

  // The input holds a draft. The URL holds the committed term, and the draft
  // reaches it debounced.
  const committed = search.q ?? "";
  const [draft, setDraft] = useState(committed);
  const [lastCommitted, setLastCommitted] = useState(committed);

  // The back button, and a link an operator pasted, both move `q` under the
  // input. Following it while rendering repaints the input in the same commit,
  // where an effect would leave the stale term on screen for a frame and then
  // debounce it straight back into the URL.
  if (committed !== lastCommitted) {
    setLastCommitted(committed);
    // The draft reaches the URL trimmed, so the debounce's own commit lands
    // back here as a change. Repainting on that one would eat a space the
    // operator just typed. `lastCommitted` still moves either way, or this
    // block runs on every render.
    if (draft.trim() !== committed) setDraft(committed);
  }

  useEffect(() => {
    const next = draft.trim();
    // A term that settles back on the committed one is not a change, so a typo
    // and a backspace leave the URL alone.
    if (next === committed) return;

    const timer = setTimeout(() => {
      void navigate({
        search: (prev: OrganizationsSearch) => ({
          ...prev,
          q: next || undefined,
          page: undefined,
        }),
        // Keystroke rate. One history entry per burst of typing, not one per
        // keystroke.
        replace: true,
      });
    }, SEARCH_DEBOUNCE_MS);

    return () => clearTimeout(timer);
  }, [draft, committed, navigate]);

  // Which group the sheet is showing, and null when it is closed.
  const [openGroup, setOpenGroup] = useState<FilterGroupKey | null>(null);
  const triggers = useRef<Partial<Record<FilterGroupKey, HTMLButtonElement>>>(
    {},
  );
  // The trigger the sheet has to give the keyboard back to. A ref rather than
  // `openGroup`, because the sheet asks for it as it unmounts: by then the
  // state that opened it has already been cleared.
  const openedFrom = useRef<FilterGroupKey | null>(null);

  const filters: FilterSelection = {
    type: search.type ?? [],
    trial: search.trial ?? [],
    disabled: search.disabled ?? [],
  };

  const applyFilters = (next: FilterSelection): void => {
    void navigate({
      search: (prev: OrganizationsSearch) => ({
        ...prev,
        ...filtersToSearch(next),
        // Page 1. The rows a page-two cursor points at were counted under the
        // filters that minted it.
        page: undefined,
      }),
    });
  };

  const openFilters = (group: FilterGroupKey): void => {
    openedFrom.current = group;
    setOpenGroup(group);
  };

  return (
    <div className="mb-2 flex items-center gap-2">
      <div className="relative w-80">
        <SearchIcon className="text-muted-foreground pointer-events-none absolute top-1/2 left-2 size-4 -translate-y-1/2" />
        <Input
          aria-label="Search organizations"
          placeholder="Search by name, slug or id..."
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          className="w-full py-1.5 pr-2 pl-8"
        />
      </div>

      {/* One trigger per group, all opening the same sheet. The group is the
          unit an operator thinks in, and a single Filters button would make
          them find the group again inside the sheet. */}
      {FILTER_GROUPS.map((group) => {
        const chosen = filters[group.key];
        return (
          <Button
            key={group.key}
            ref={(node) => {
              if (node) triggers.current[group.key] = node;
            }}
            variant={chosen.length > 0 ? "secondary" : "ghost"}
            size="xs"
            // The count is the whole visible signal, and a count does not say
            // what it counts. The name a screen reader announces says it.
            aria-label={`${group.label} filter: ${filterSummary(
              group,
              chosen,
              optionsFor(group, chosen),
            )}`}
            onClick={() => openFilters(group.key)}
          >
            {group.label}
            {chosen.length > 0 && <Badge>{chosen.length}</Badge>}
          </Button>
        );
      })}

      <FilterSheet
        value={filters}
        openGroup={openGroup}
        onOpenChange={(open) => {
          if (!open) setOpenGroup(null);
        }}
        onApply={applyFilters}
        onReturnFocus={() => {
          const group = openedFrom.current;
          if (group) triggers.current[group]?.focus();
        }}
      />
    </div>
  );
}

/**
 * The strip above the header row. It is mounted whether or not rows are
 * selected, so the slice that adds bulk actions swaps its contents in place
 * and no row moves under the pointer.
 */
export function TableActionBar<T extends RowData>({
  table,
  onColumnToggled,
}: {
  table: DataTableInstance<T>;
  // Told after a toggle lands, so a page that overrides what this menu writes
  // can answer a request the menu cannot satisfy on its own.
  onColumnToggled?: (columnId: string, label: string) => void;
}): JSX.Element {
  // Read off the table rather than walked a second time here, so the menu
  // cannot disagree with the table about how many columns are left.
  //
  // Only the hideable ones count. A column that opts out of hiding is visible
  // whatever the operator does, so counting it holds this total off the floor
  // and the guard below never fires: the operator can then hide every column
  // that carries data and be left with a table of controls and no records.
  const visibleCount = table
    .getVisibleLeafColumns()
    .filter((column) => column.getCanHide()).length;

  return (
    <div className="flex items-center gap-3 border-b px-3 py-2">
      <span className="text-muted-foreground text-xs">Nothing selected</span>
      <span className="flex-1" />
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="xs">
            Columns
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {table.getAllLeafColumns().map((column) => {
            const checked = column.getIsVisible();
            // Hiding the last one leaves a header with no cells above rows with
            // no cells. This menu still lists every column, so the operator can
            // climb back out, but the table is unreadable until they do.
            const locked =
              !column.getCanHide() || (checked && visibleCount === 1);
            return (
              <DropdownMenuCheckboxItem
                key={column.id}
                checked={checked}
                // Radix drops a `disabled` item out of the menu's roving focus,
                // so a keyboard or a screen reader never reaches it. Marking it
                // instead keeps it reachable, and moves the dimming here.
                aria-disabled={locked}
                className={cn(locked && "opacity-50")}
                // Radix fires onCheckedChange even when onSelect prevents the
                // default, so the guard has to sit on both.
                onSelect={(event) => {
                  if (locked) event.preventDefault();
                }}
                onCheckedChange={() => {
                  if (locked) return;
                  column.toggleVisibility();
                  onColumnToggled?.(column.id, columnLabel(column));
                }}
              >
                {columnLabel(column)}
              </DropdownMenuCheckboxItem>
            );
          })}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

// A header is a renderable in general, and only a string carries a label a
// screen reader can announce here. The id is the readable fallback.
function columnLabel<T extends RowData>(
  column: Column<DataTableFeatures, T>,
): string {
  const { header } = column.columnDef;
  return typeof header === "string" ? header : column.id;
}
