import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { useTable, type ColumnVisibilityState } from "@tanstack/react-table";
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type JSX,
  type KeyboardEvent,
  type MouseEvent,
} from "react";

import { dataTableFeatures, DataTable as Table } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { organizationsListQuery } from "@/lib/adminQueries";
import {
  errorMessage,
  omitUnset,
  type AdminOrganization,
  type ListOrganizationsParams,
} from "@/lib/gramAdminApi";
import { cn } from "@/lib/utils";

import { ORG_COLUMNS } from "./columns";
import { PeekPanel } from "./PeekPanel";
import { useOpenOrganization } from "./rowActions";
import { TableActionBar, Toolbar } from "./Toolbar";

const ROUTE_ID = "/organizations/";
const PAGE_SIZE = 50;
const NO_ORGS: AdminOrganization[] = [];

// The columns peek keeps are the ones that name a row. Everything it hides is
// in the panel beside the table, so nothing is lost and the table fits.
//
// Module scope: a value rebuilt on every render rebuilds the column model with
// it. It is also merged over the operator's own visibility rather than written
// into it, so closing peek cannot undo a choice they made.
const PEEK_HIDDEN_COLUMNS: ColumnVisibilityState = {
  member_count: false,
  workos_id: false,
  disabled_at: false,
  free_trial_ends_at: false,
  created_at: false,
};

const ARROW_STEP: Record<string, number | undefined> = {
  ArrowDown: 1,
  ArrowUp: -1,
};

// Radix renders a menu and a select in a portal, but a portal still bubbles
// through the React tree. Both own the arrow keys for roving focus and Escape
// for dismissal, and a text field owns them too.
const KEYS_NOT_OURS = '[role="menu"],[role="listbox"],input,textarea';

function emptyStateMessage(isLoading: boolean, isError: boolean): string {
  if (isLoading) return "Loading...";
  if (isError) return "Unable to load organizations";
  return "No organizations found";
}

// The list API is cursor-paged, so a page cannot be addressed by number and the
// pager stays out of the URL. `filters` records which filter set produced the
// cursor: a cursor outlives its filters as a valid-looking string that points
// into the wrong result set.
type Pager = { filters: string; cursor?: string; stack: string[] };

export function OrganizationsList(): JSX.Element {
  const search = useSearch({ from: ROUTE_ID });
  const openOrganization = useOpenOrganization();

  // Column visibility is deliberately not in the URL. It is a per-operator
  // preference, not part of the view a link carries, and it resets on reload.
  const [columnVisibility, setColumnVisibility] =
    useState<ColumnVisibilityState>({});

  // The id, never the record. A page change or a filter change replaces every
  // row, and a panel holding the record outlives the table that produced it.
  const [peekedId, setPeekedId] = useState<string>();
  const peekedRow = useRef<HTMLTableRowElement>(null);

  // One object is the source of both the request and the signature below. Two
  // hand-written lists drift: a slice that adds a filter to the request would
  // otherwise have to remember to add it to the reset as well.
  //
  // Every value arrives validated, so nothing is normalised a second time here.
  const listParams: ListOrganizationsParams = {
    q: search.q,
    account_type: search.type,
    include_disabled: search.disabled,
  };

  // omitUnset, not the raw object: the signature has to call a param unset
  // wherever the request does, or a no-op edit resets the pager.
  const filters = JSON.stringify(omitUnset(listParams));
  const [pager, setPager] = useState<Pager>({ filters, stack: [] });
  // Reset while rendering, so the query below never asks for the stale cursor.
  // An effect would run after the request had already gone out.
  if (pager.filters !== filters) {
    setPager({ filters, stack: [] });
  }

  const { data, isLoading, isError, error, isPlaceholderData } = useQuery({
    ...organizationsListQuery({
      ...listParams,
      cursor: pager.cursor,
      limit: PAGE_SIZE,
    }),
    // Every filter and every page is a separate cache entry. Without this the
    // table empties on each change and the rows jump.
    placeholderData: keepPreviousData,
  });

  const goNext = () => {
    if (!data?.next_cursor) return;
    setPager({
      filters,
      cursor: data.next_cursor,
      stack: [...pager.stack, pager.cursor ?? ""],
    });
  };

  const goPrev = () => {
    if (pager.stack.length === 0) return;
    // An empty string on the stack is the first page, which has no cursor.
    const previous = pager.stack[pager.stack.length - 1];
    setPager({
      filters,
      cursor: previous || undefined,
      stack: pager.stack.slice(0, -1),
    });
  };

  const orgs = data?.organizations ?? NO_ORGS;

  // Derived on the way in, so the forced hides never reach the operator's own
  // state. Memoised because a fresh object each render is a fresh column model.
  const effectiveVisibility = useMemo(
    () =>
      peekedId
        ? { ...columnVisibility, ...PEEK_HIDDEN_COLUMNS }
        : columnVisibility,
    [peekedId, columnVisibility],
  );

  const table = useTable({
    features: dataTableFeatures,
    columns: ORG_COLUMNS,
    data: orgs,
    // Without this a row is keyed by its index, and React reuses those keys
    // across a page change and across a filter change.
    getRowId: (org) => org.id,
    state: { columnVisibility: effectiveVisibility },
    onColumnVisibilityChange: setColumnVisibility,
  });

  const rows = table.getRowModel().rows;

  // One pass gives both the record the panel paints and the anchor the arrow
  // keys step from.
  const peekedIndex = peekedId ? rows.findIndex((r) => r.id === peekedId) : -1;
  const peeked = peekedIndex === -1 ? undefined : rows[peekedIndex];

  // Reset while rendering rather than in an effect, so the panel never paints a
  // record the table on screen has already dropped.
  if (peekedId && !peeked) {
    setPeekedId(undefined);
  }

  useEffect(() => {
    // The scroll box is capped at 60vh, so a row the arrow keys move peek to
    // can sit below the fold with nothing on screen to say peek moved.
    peekedRow.current?.scrollIntoView({ block: "nearest" });
  }, [peekedId]);

  const closePeek = (): void => {
    // The row itself takes no focus by design, so the keyboard goes back to the
    // link in its Name cell. Read before the state change, while it is on screen.
    const link = peekedRow.current?.querySelector("a");
    setPeekedId(undefined);
    link?.focus();
  };

  const handleRowClick = (
    org: AdminOrganization,
    event: MouseEvent<HTMLTableRowElement>,
  ): void => {
    // Alt, not meta and not shift: meta-click is the browser's own
    // open-in-new-tab, and shift-click belongs to the range selection a later
    // slice adds. A plain click still opens the organization.
    if (event.altKey) {
      setPeekedId(org.id);
      return;
    }
    openOrganization(org);
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>): void => {
    if (!peeked || event.defaultPrevented) return;
    if ((event.target as HTMLElement).closest(KEYS_NOT_OURS)) return;

    if (event.key === "Escape") {
      event.preventDefault();
      closePeek();
      return;
    }

    const step = ARROW_STEP[event.key];
    if (!step) return;
    // Stop at the ends. Paging would replace every row node, taking the anchor
    // peek walks from with it.
    const next = rows[peekedIndex + step];
    if (!next) return;
    event.preventDefault();
    setPeekedId(next.id);
  };

  return (
    <div className="space-y-6">
      <section>
        <Toolbar />

        {/* A failed refetch keeps the previous rows, so the failure has to show
            outside the empty state or the operator reads stale data as fresh. */}
        {isError && (
          <div className="text-muted-foreground mb-2 text-sm">
            Could not refresh organizations: {errorMessage(error)}
          </div>
        )}

        {/* Scoped here, not to the document: the handler has to reach the row
            links and the panel, and stay out of the toolbar's search box and
            account-type select above. The pager below stays full width, because
            it moves the list rather than the record the panel holds. */}
        <div className="flex items-start gap-4" onKeyDown={handleKeyDown}>
          <div className="min-w-0 flex-1 rounded-lg border">
            <TableActionBar table={table} />

            <div
              className={cn(
                "max-h-[60vh] overflow-auto",
                isPlaceholderData && "opacity-60",
              )}
            >
              <Table>
                <Table.Header table={table} />
                <Table.Body>
                  {rows.length === 0 ? (
                    <Table.NoResultsMessage>
                      <span className="text-muted-foreground text-sm">
                        {emptyStateMessage(isLoading, isError)}
                      </span>
                    </Table.NoResultsMessage>
                  ) : (
                    rows.map((row) => {
                      const isPeeked = row.id === peekedId;
                      return (
                        <Table.Row
                          key={row.id}
                          row={row}
                          ref={isPeeked ? peekedRow : undefined}
                          className={cn(isPeeked && "bg-muted")}
                          onClick={handleRowClick}
                        />
                      );
                    })
                  )}
                </Table.Body>
              </Table>
            </div>
          </div>

          {peeked ? (
            <PeekPanel
              org={peeked.original}
              onClose={closePeek}
              className="w-100 shrink-0"
            />
          ) : null}
        </div>

        {/* Placeholder rows belong to the previous filter, and so does the
            cursor beside them. Both controls wait for the real page. */}
        <div className="mt-3 flex items-center justify-end gap-2">
          <Button
            variant="ghost"
            size="xs"
            disabled={isPlaceholderData || pager.stack.length === 0}
            onClick={goPrev}
          >
            Previous
          </Button>
          <Button
            variant="ghost"
            size="xs"
            disabled={isPlaceholderData || !data?.next_cursor}
            onClick={goNext}
          >
            Next
          </Button>
        </div>
      </section>
    </div>
  );
}
