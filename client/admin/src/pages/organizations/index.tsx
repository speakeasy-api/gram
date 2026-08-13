import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { useCallback, useMemo, useState, type JSX } from "react";

import { DataTable as Table } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { organizationsListQuery } from "@/lib/adminQueries";
import { errorMessage, type AdminOrganization } from "@/lib/gramAdminApi";
import { cn } from "@/lib/utils";

import { ORG_COLUMNS } from "./columns";
import { useOpenOrganization } from "./rowActions";
import { TableActionBar, Toolbar } from "./Toolbar";

const ROUTE_ID = "/organizations/";
const PAGE_SIZE = 50;
const NO_ORGS: AdminOrganization[] = [];

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
  const [hiddenColumns, setHiddenColumns] = useState<ReadonlySet<string>>(
    () => new Set(),
  );

  const filters = JSON.stringify([
    search.q,
    search.type,
    search.trial,
    search.disabled,
  ]);
  const [pager, setPager] = useState<Pager>({ filters, stack: [] });
  // Reset while rendering, so the query below never asks for the stale cursor.
  // An effect would run after the request had already gone out.
  if (pager.filters !== filters) {
    setPager({ filters, stack: [] });
  }

  const { data, isLoading, isError, error, isPlaceholderData } = useQuery({
    ...organizationsListQuery({
      q: search.q,
      account_type: search.type?.[0],
      include_disabled: search.disabled,
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

  const toggleColumn = useCallback((key: string) => {
    setHiddenColumns((current) => {
      const next = new Set(current);
      if (!next.delete(key)) next.add(key);
      return next;
    });
  }, []);

  const visibleColumns = useMemo(
    () =>
      ORG_COLUMNS.filter((column) => !hiddenColumns.has(String(column.key))),
    [hiddenColumns],
  );

  const orgs = data?.organizations ?? NO_ORGS;

  const rows = useMemo(
    () =>
      orgs.map((org) => (
        <Table.Row
          key={org.id}
          row={org}
          columns={visibleColumns}
          onClick={openOrganization}
        />
      )),
    [orgs, visibleColumns, openOrganization],
  );

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

        <div className="rounded-lg border">
          <TableActionBar
            columns={ORG_COLUMNS}
            hiddenColumns={hiddenColumns}
            onToggleColumn={toggleColumn}
          />

          <div
            className={cn(
              "max-h-[60vh] overflow-auto",
              isPlaceholderData && "opacity-60",
            )}
          >
            <Table columns={visibleColumns}>
              <Table.Header columns={visibleColumns} />
              <Table.Body>
                {orgs.length === 0 ? (
                  <Table.NoResultsMessage>
                    <span className="text-muted-foreground text-sm">
                      {emptyStateMessage(isLoading, isError)}
                    </span>
                  </Table.NoResultsMessage>
                ) : (
                  rows
                )}
              </Table.Body>
            </Table>
          </div>
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
