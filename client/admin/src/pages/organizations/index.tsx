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
import { WriteReportProvider } from "./OrganizationActions";
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
  trial_state: false,
  created_at: false,
};

// Every column peek overrides while it is open, and the value it forces. The
// table's visibility and the Columns menu's guard both read this one map, so
// they cannot disagree about the set: a guard naming only the hidden half
// answers only half the writes the menu cannot satisfy, and the other half land
// and take effect later, when the peek closes.
const PEEK_COLUMN_OVERRIDES: ColumnVisibilityState = {
  ...PEEK_HIDDEN_COLUMNS,
  name: true,
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
  const peekPanel = useRef<HTMLElement>(null);
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

  // A write that failed with no dialog of its own to report in. Re-enable is
  // the only one, and without this the whole account of it on the page is a
  // sentence inside `sr-only`: the row is unchanged, the banner above covers
  // the list query alone, and a sighted operator presses Re-enable, sees
  // nothing happen and is told nothing about why.
  const [writeFailure, setWriteFailure] = useState<string | null>(null);

  const announce = useCallback((text: string): void => {
    setAnnouncement((previous) => ({ text, count: previous.count + 1 }));
  }, []);

  // One object, memoised: it is a context value, and a fresh one on every
  // render would re-render every row's actions on every keystroke in the
  // filter box.
  const writeReporter = useMemo(
    () => ({ announce, showFailure: setWriteFailure }),
    [announce],
  );

  // One object is the source of both the request and the signature below. Two
  // hand-written lists drift: a slice that adds a filter to the request would
  // otherwise have to remember to add it to the reset as well.
  //
  // Every value arrives validated, so nothing is normalised a second time here.
  const listParams: ListOrganizationsParams = {
    q: search.q,
    account_types: search.type,
    trial_states: search.trial,
    disabled_states: search.disabled,
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
        ? { ...columnVisibility, ...PEEK_COLUMN_OVERRIDES }
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

  // Peek overrides these columns, so a write the menu makes to one of them is a
  // write nothing on screen answers: the checkbox snaps back with no column, no
  // refusal and no reason. Toggling a column is an unambiguous request about
  // that column, so the panel gives way rather than the request. The column
  // arrives in the commit the panel leaves in, which is the whole explanation,
  // and reopening peek is one click.
  //
  // Both directions, because peek forces some columns off and Name on. The
  // operator unchecking Name is asking to hide it, and refusing that one is the
  // worse half: the write lands under the override and detonates later, when
  // the peek closes and the column the row is anchored on disappears at a
  // moment the operator has no reason to connect to the click.
  //
  // The keyboard is left where it is. The operator is in the Columns menu, and
  // Radix puts them back on its trigger as it closes.
  const handleColumnToggled = useCallback(
    (columnId: string, label: string): void => {
      if (!peekedId || !Object.hasOwn(PEEK_COLUMN_OVERRIDES, columnId)) return;
      setPeek(undefined);
      // Which way the operator was asking. Peek forcing the column visible
      // means the request was to hide it, and one wording for both is false
      // half the time.
      const wasForcedVisible = PEEK_COLUMN_OVERRIDES[columnId] === true;
      announce(
        `Peek closed to ${wasForcedVisible ? "hide" : "show"} the ${label} column.`,
      );
    },
    [peekedId, announce],
  );

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>): void => {
    // Ahead of both allow-lists below. Radix calls preventDefault when it
    // handles Escape itself, so a tooltip or a popover opened from inside the
    // panel takes the first Escape and the panel does not close underneath it.
    if (!peeked || event.defaultPrevented) return;

    // Nothing else under this handler answers either key. The pager and another
    // row's peek control keep their own, and the two keys below are scoped
    // separately because they fail in different ways.
    const fromTrigger =
      event.target instanceof HTMLElement
        ? event.target.closest(PEEK_TRIGGER_SELECTOR)
        : null;
    const fromPeekedTrigger =
      fromTrigger !== null && peekedRow.current?.contains(fromTrigger) === true;

    if (event.key === "Escape") {
      // The whole panel, its contents included. Escape has no reflex of its own
      // for the panel to steal, and it is the dismiss gesture for whichever
      // surface holds focus, so a button in the panel body has to answer it.
      // Closing does not drop the keyboard: closePeek moves it to the peeked
      // row's control, off the subtree that is about to unmount.
      const insidePanel =
        event.target instanceof HTMLElement &&
        peekPanel.current?.contains(event.target) === true;
      if (!fromPeekedTrigger && !insidePanel) return;
      event.preventDefault();
      closePeek();
      return;
    }

    const step = ARROW_STEP[event.key];
    if (!step) return;
    // The panel container only, not its contents. An arrow key on a button in
    // the panel body is a reflex to scroll, and answering it would move the
    // panel to a record the operator is not on and eat the scroll on the way.
    const fromPanel = event.target === peekPanel.current;
    if (!fromPeekedTrigger && !fromPanel) return;
    // Stop at the ends: paging replaces the row nodes the anchor depends on.
    const next = rows[peekedIndex + step];
    if (!next) return;
    event.preventDefault();
    openPeek(next.original);
    // Only where the operator was already on a control. The peek moves out
    // from under this one, and the arrow allow-list above is written in terms
    // of the peeked row, so leaving the keyboard behind would strand it on a
    // control that answers neither the arrow keys nor Escape.
    //
    // Focus stays put for anyone arrowing from the panel. That is the panel's
    // own navigation, and pulling the keyboard out to a row control would take
    // the operator off the record they are reading.
    //
    // Reading only: the arrow allow-list already turned every other trigger
    // away, and the panel is not one, so a non-null fromTrigger here is the
    // peeked row's. Named for the reader rather than to change the set.
    if (fromPeekedTrigger) setPeekTookTheKeyboard(true);
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

        {/* A write that has no dialog to fail in reports here, where it can be
            read as well as heard, and stays until it is dismissed or a later
            write succeeds. */}
        {writeFailure && (
          <div
            role="alert"
            className="mb-2 flex items-start justify-between gap-2 rounded-md border border-destructive/40 px-3 py-2 text-destructive text-sm"
          >
            <span>{writeFailure}</span>
            <Button
              variant="ghost"
              size="xs"
              aria-label="Dismiss the failure"
              onClick={() => setWriteFailure(null)}
            >
              Dismiss
            </Button>
          </div>
        )}

        {/* The only thing that speaks when the arrow keys swap the record under
            a panel that already holds the focus.

            `aria-live` is written out as well as implied by the role, and it is
            load-bearing rather than belt and braces: an open Radix modal hides
            the rest of the page with `aria-hidden`, and the one exemption that
            package makes is for elements carrying this attribute by name.
            Without it the region goes down with the app container and a write
            that fails behind a dialog is announced to nobody. The zero-width
            alternates with the count so that a sentence set twice running
            still reaches the accessibility tree as a change. It is not
            announced, and it is not rendered anywhere a sighted operator
            reads. */}
        <div role="status" aria-live="polite" className="sr-only">
          {announcement.text}
          {announcement.count % 2 === 1 ? ZERO_WIDTH_SPACE : ""}
        </div>

        {/* Both providers wrap the panel as well as the table: the row menu and
            the panel footer are the same actions, and they report through the
            one region above. */}
        <WriteReportProvider value={writeReporter}>
          <PeekProvider value={peekControls}>
            {/* Stretch, so the panel takes its height from the row. */}
            <div
              className="flex min-h-0 flex-1 gap-4"
              onKeyDown={handleKeyDown}
            >
              <div className="flex min-h-0 min-w-0 flex-1 flex-col">
                <div className="flex min-h-0 flex-1 flex-col rounded-lg border">
                  <TableActionBar
                    table={table}
                    onColumnToggled={handleColumnToggled}
                  />

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
                                // The pinned cell inherits the row's colour and
                                // paints it again, so a translucent row doubles
                                // up and shows the scrolled columns through the
                                // pin. Overriding the two half-alpha states is
                                // a twMerge collapse, not a cascade win: the
                                // emitted CSS puts the `/50` rule last at equal
                                // specificity, so it wins any tie.
                                className={cn(
                                  "bg-background hover:bg-muted has-aria-expanded:bg-muted",
                                  isPeeked && "bg-muted",
                                )}
                                onClick={openOrganization}
                                onAltClick={togglePeek}
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
                  ref={peekPanel}
                  org={peeked.original}
                  onClose={closePeek}
                  className="w-100 shrink-0"
                />
              ) : null}
            </div>
          </PeekProvider>
        </WriteReportProvider>
      </section>
    </div>
  );
}
