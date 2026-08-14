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

const PEEK_HIDDEN_COLUMNS: ColumnVisibilityState = {
  member_count: false,
  workos_id: false,
  disabled_at: false,
  free_trial_ends_at: false,
  created_at: false,
};

// Mac reports "Mac" here and labels the key Option; every other platform calls
// it Alt. Naming the wrong one makes the hint worse than no hint.
const PEEK_KEY = navigator.userAgent.includes("Mac") ? "\u2325 Option" : "Alt";

function PeekHint(): JSX.Element {
  return (
    <span className="text-muted-foreground hidden items-center gap-1 text-xs sm:flex">
      <kbd className="bg-muted rounded border px-1.5 py-0.5 font-sans text-[11px] leading-none">
        {PEEK_KEY}
      </kbd>
      + click a row to peek
    </span>
  );
}

const ARROW_STEP: Record<string, number | undefined> = {
  ArrowDown: 1,
  ArrowUp: -1,
};

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

  // An id, not the record: a page or filter change replaces every row.
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

  // findIndex, not table.getRow: getRow throws once a page change drops the id.
  const peekedIndex = peekedId ? rows.findIndex((r) => r.id === peekedId) : -1;
  const peeked = peekedIndex === -1 ? undefined : rows[peekedIndex];

  // During render, not in an effect: an effect paints a dropped record first.
  if (peekedId && !peeked) {
    setPeekedId(undefined);
  }

  useEffect(() => {
    peekedRow.current?.scrollIntoView({ block: "nearest" });
  }, [peekedId]);

  const closePeek = (): void => {
    const link = peekedRow.current?.querySelector("a");
    setPeekedId(undefined);
    link?.focus();
  };

  const handleRowClick = (
    org: AdminOrganization,
    event: MouseEvent<HTMLTableRowElement>,
  ): void => {
    // Alt, not Meta (the browser's open-in-new-tab) and not Shift (range select).
    if (event.altKey) {
      setPeekedId(org.id);
      return;
    }
    openOrganization(org);
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>): void => {
    if (!peeked || event.defaultPrevented) return;

    if (event.key === "Escape") {
      event.preventDefault();
      closePeek();
      return;
    }

    const step = ARROW_STEP[event.key];
    if (!step) return;
    // Stop at the ends: paging replaces the row nodes the anchor depends on.
    const next = rows[peekedIndex + step];
    if (!next) return;
    event.preventDefault();
    setPeekedId(next.id);
  };

  return (
    <div className="flex h-full flex-col">
      <section className="flex min-h-0 flex-1 flex-col">
        <Toolbar />

        {/* A failed refetch keeps the previous rows, so the failure has to show
            outside the empty state or the operator reads stale data as fresh. */}
        {isError && (
          <div className="text-muted-foreground mb-2 text-sm">
            Could not refresh organizations: {errorMessage(error)}
          </div>
        )}

        {/* Stretch, not items-start: the panel takes its height from the row so
            it lines up with the table without naming a height of its own. */}
        <div className="flex min-h-0 flex-1 gap-4" onKeyDown={handleKeyDown}>
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            <div className="flex min-h-0 flex-1 flex-col rounded-lg border">
              <TableActionBar table={table} hint={<PeekHint />} />

              <div
                className={cn(
                  "min-h-0 flex-1 overflow-auto",
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

            {/* Inside the table's column, not beside it: the pager moves the
                list, and the panel holds one record the list may not carry. */}
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
          </div>

          {peeked ? (
            <PeekPanel
              org={peeked.original}
              onClose={closePeek}
              className="w-100 shrink-0"
            />
          ) : null}
        </div>
      </section>
    </div>
  );
}
