import { useNavigate, useSearch } from "@tanstack/react-router";
import type { Column, RowData } from "@tanstack/react-table";
import { SearchIcon } from "lucide-react";
import { useEffect, useState, type JSX } from "react";

import type {
  DataTableFeatures,
  DataTableInstance,
} from "@/components/data-table";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ACCOUNT_TYPE_OPTIONS, isAccountType } from "@/lib/accountTypes";
import { cn } from "@/lib/utils";
import type { OrganizationsSearch } from "@/routes/organizations.index";

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

  const applyFilter = (patch: Partial<OrganizationsSearch>): void => {
    void navigate({
      search: (prev: OrganizationsSearch) => ({
        ...prev,
        ...patch,
        page: undefined,
      }),
    });
  };

  const selectedType = search.type ?? "";

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
      <Select
        value={selectedType || "all"}
        onValueChange={(value) =>
          applyFilter({ type: isAccountType(value) ? value : undefined })
        }
      >
        <SelectTrigger
          aria-label="Account type"
          className="h-auto w-auto px-2 py-1.5"
        >
          <SelectValue placeholder="All types" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All types</SelectItem>
          {ACCOUNT_TYPE_OPTIONS.map((t) => (
            <SelectItem key={t} value={t}>
              {t}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button
        variant={search.disabled ? "default" : "ghost"}
        size="xs"
        onClick={() =>
          applyFilter({ disabled: search.disabled ? undefined : true })
        }
      >
        {search.disabled ? "Hide disabled" : "Show disabled"}
      </Button>
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
}: {
  table: DataTableInstance<T>;
}): JSX.Element {
  // Read off the table rather than walked a second time here, so the menu
  // cannot disagree with the table about how many columns are left.
  const visibleCount = table.getVisibleLeafColumns().length;

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
                  if (!locked) column.toggleVisibility();
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
