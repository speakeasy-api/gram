import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { useTable, type ColumnVisibilityState } from "@tanstack/react-table";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type JSX,
  type KeyboardEvent,
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
import { PEEK_TRIGGER_SELECTOR, PeekProvider } from "./PeekTrigger";
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

// Appended to every other announcement so an unchanged sentence still changes
// the text node. Zero-width, so nothing is spoken and nothing takes up space.
const ZERO_WIDTH_SPACE = "\u200b";

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

  // An id and a name, not the record: a page or filter change replaces every
  // row, and the panel renders from the live row. The name is carried only to
  // word the announcement for a record that has already left the list.
  const [peek, setPeek] = useState<{ id: string; name: string }>();
  const peekedId = peek?.id;
  const peekedRow = useRef<HTMLTableRowElement>(null);
  const scrollBox = useRef<HTMLDivElement>(null);

  // Mounted for the life of the page. A live region that arrives in the same
  // commit as its text is not reliably announced: the element has to be in the
  // accessibility tree first.
  //
  // The count rides along because a region is announced when its text changes.
  // Organization names are not unique, so the same sentence can be set twice
  // running; React bails on an equal string, the DOM text never moves, and the
  // operator hears nothing while the panel visibly swaps records.
  const [announcement, setAnnouncement] = useState({ text: "", count: 0 });

  // Raised while rendering, read by the effect that rescues the keyboard.
  const [peekedRecordLeft, setPeekedRecordLeft] = useState(false);

  // Raised by an arrow move that started on a peek control, read by the effect
  // that follows the peek to the next row's control.
  const [peekTookTheKeyboard, setPeekTookTheKeyboard] = useState(false);

  const announce = useCallback((text: string): void => {
    setAnnouncement((previous) => ({ text, count: previous.count + 1 }));
  }, []);

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

  // Name carries the row's only anchor, which peek closes back onto. Forcing it
  // also keeps peek's own hiding from emptying the table.
  const effectiveVisibility = useMemo(
    () =>
      peekedId
        ? { ...columnVisibility, ...PEEK_HIDDEN_COLUMNS, name: true }
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
  if (peek && !peeked) {
    setPeek(undefined);
    announce(`Peek closed. ${peek.name} is no longer in the list.`);
    setPeekedRecordLeft(true);
  }

  useEffect(() => {
    peekedRow.current?.scrollIntoView({ block: "nearest" });
  }, [peekedId]);

  useEffect(() => {
    if (!peekedRecordLeft) return;
    setPeekedRecordLeft(false);
    // Only where the panel took its focus down with it. An operator who paged
    // or filtered the record away is already on a live control, and taking
    // their place in the page is worse than the bug this rescues.
    if (document.activeElement === document.body) {
      scrollBox.current?.focus();
    }
  }, [peekedRecordLeft]);

  // After the commit, because the row the peek moved to is drawn in it and the
  // ref only points at that row once it is. Same lookup the close path makes,
  // through the peeked row rather than across the page.
  //
  // A screen reader announces the control focus lands on, which repeats what
  // the live region is politely saying at the same moment. The repeat is
  // wanted: the two carry the same organization name, so whichever one the
  // reader drops, the operator still hears where the panel went.
  useEffect(() => {
    if (!peekTookTheKeyboard) return;
    setPeekTookTheKeyboard(false);
    peekedRow.current
      ?.querySelector<HTMLElement>(PEEK_TRIGGER_SELECTOR)
      ?.focus();
  }, [peekTookTheKeyboard]);

  const closePeek = useCallback((): void => {
    const trigger = peekedRow.current?.querySelector<HTMLElement>(
      PEEK_TRIGGER_SELECTOR,
    );
    setPeek(undefined);
    announce("Peek closed.");
    trigger?.focus();
  }, [announce]);

  const openPeek = useCallback(
    (org: AdminOrganization): void => {
      setPeek({ id: org.id, name: org.name });
      announce(`Peeking at ${org.name}.`);
    },
    [announce],
  );

  const togglePeek = useCallback(
    (org: AdminOrganization): void => {
      if (peekedId === org.id) {
        closePeek();
        return;
      }
      openPeek(org);
    },
    [peekedId, closePeek, openPeek],
  );

  const peekControls = useMemo(
    () => ({ peekedId, togglePeek }),
    [peekedId, togglePeek],
  );

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>): void => {
    if (!peeked || event.defaultPrevented) return;

    // Every row's peek control sits under this handler. An arrow key pressed
    // on one of them is a reflex to scroll, and answering it moves the panel
    // to a row the operator is not on, eats the scroll, and leaves them
    // standing on a control that still reports itself closed. Escape there
    // would drag the keyboard back to the peeked row. The peeked row's own
    // control is exempt: that one is the panel's control.
    const fromTrigger =
      event.target instanceof HTMLElement
        ? event.target.closest(PEEK_TRIGGER_SELECTOR)
        : null;
    if (fromTrigger && !peekedRow.current?.contains(fromTrigger)) return;

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
    openPeek(next.original);
    // Only where the operator was already on a control. The peek moves out
    // from under this one, and the exemption above is written in terms of the
    // peeked row, so leaving the keyboard behind would strand it on a control
    // that answers neither the arrow keys nor Escape.
    //
    // Focus stays put for anyone arrowing from inside the panel. That is the
    // panel's own navigation, and pulling the keyboard out to a row control
    // would take the operator off the record they are reading.
    if (fromTrigger) setPeekTookTheKeyboard(true);
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

        {/* The only thing that speaks when the arrow keys swap the record under
            a panel that already holds the focus.

            `aria-live` is written out as well as implied by the role: a few
            screen readers only honour the attribute. The zero-width space
            alternates with the count so that a sentence set twice running
            still reaches the accessibility tree as a change. It is not
            announced, and it is not rendered anywhere a sighted operator
            reads. */}
        <div role="status" aria-live="polite" className="sr-only">
          {announcement.text}
          {announcement.count % 2 === 1 ? ZERO_WIDTH_SPACE : ""}
        </div>

        <PeekProvider value={peekControls}>
          {/* Stretch, so the panel takes its height from the row. */}
          <div className="flex min-h-0 flex-1 gap-4" onKeyDown={handleKeyDown}>
            <div className="flex min-h-0 min-w-0 flex-1 flex-col">
              <div className="flex min-h-0 flex-1 flex-col rounded-lg border">
                <TableActionBar table={table} />

                <div
                  ref={scrollBox}
                  // Named, because this is where the keyboard lands when the
                  // peeked record leaves the list with the panel holding the
                  // focus. An unnamed div would put the operator somewhere
                  // their screen reader cannot describe.
                  role="region"
                  aria-label="Organizations table"
                  // Programmatic only: -1 takes the box out of the tab order
                  // and still lets that rescue focus it. The focus ring stays,
                  // because nothing else on screen moves when focus arrives
                  // here and the ring is the only sign that it did.
                  tabIndex={-1}
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
                              onClick={openOrganization}
                            />
                          );
                        })
                      )}
                    </Table.Body>
                  </Table>
                </div>
              </div>

              {/* Inside the table's column, so it does not run under the panel. */}
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
        </PeekProvider>
      </section>
    </div>
  );
}
