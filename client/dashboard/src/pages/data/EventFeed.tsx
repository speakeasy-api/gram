import { unixNanoToDate } from "@/components/chart/chartUtils";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { LogWorkbench } from "@/components/log-workbench";
import { Page } from "@/components/page-layout";
import { WorkbenchPage } from "@/components/page-templates";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Skeleton } from "@/components/ui/Skeleton";
import { getPresetRange } from "@/elements";
import { formatPlatform } from "@/lib/formatPlatform";
import { cn } from "@/lib/utils";
import { otelGetEventFacets } from "@gram/client/funcs/otelGetEventFacets";
import { otelGetEventVolume } from "@gram/client/funcs/otelGetEventVolume";
import { otelListEventLog } from "@gram/client/funcs/otelListEventLog";
import {
  EventLogEntryKind,
  type EventLogEntry,
} from "@gram/client/models/components/eventlogentry.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { format } from "date-fns";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { EventDetailSheet } from "./EventDetailSheet";
import { EventKindBadge, EventSourceIcon } from "./event-display";
import { EventVolumeChart } from "./EventVolumeChart";

// Column tracks shared by the header row and every data row. Body gets the
// widest track and a min-width-0 cell so it truncates responsively.
const EVENT_FEED_GRID =
  "grid grid-cols-[130px_64px_minmax(0,0.9fr)_minmax(0,1fr)_minmax(0,2fr)] gap-3";

const PAGE_SIZE = 50;

// Strongly-typed filter schema for the event feed. Everything is pinned: the
// feed is read along exactly these axes (time window, signal kind, source,
// name), so no dimension belongs behind "More filters".
const EVENT_FEED_FILTERS = defineFilters([
  {
    id: "date",
    label: "Date range",
    kind: "daterange",
    pinned: true,
    defaultPreset: "1d",
  },
  {
    id: "kind",
    label: "Kind",
    kind: "multiselect",
    pinned: true,
    allLabel: "All kinds",
  },
  { id: "source", label: "Source", kind: "multiselect", pinned: true },
  { id: "name", label: "Name", kind: "multiselect", pinned: true },
]);

const KIND_OPTIONS = [
  { label: "Logs", value: EventLogEntryKind.Log },
  { label: "Spans", value: EventLogEntryKind.Span },
];

function isEventKind(value: string): value is EventLogEntryKind {
  return value === EventLogEntryKind.Log || value === EventLogEntryKind.Span;
}

export default function EventFeed(): JSX.Element {
  // The scope gate wraps the data-owning component so its queries never fire
  // for unauthorized users.
  return (
    <WorkbenchPage scope="org:read">
      <EventFeedWorkbench />
    </WorkbenchPage>
  );
}

function EventFeedWorkbench(): JSX.Element {
  const client = useGramContext();
  const containerRef = useRef<HTMLDivElement>(null);

  const { values, setValue, clearValue, clearAll } =
    useFilterState(EVENT_FEED_FILTERS);
  const [search, setSearch] = useState("");
  const [selectedEvent, setSelectedEvent] = useState<EventLogEntry | null>(
    null,
  );

  const kinds = useMemo(() => values.kind.filter(isEventKind), [values.kind]);
  const sources = values.source;
  const names = values.name;
  const searchTerm = search.trim();

  // Bumped on manual refresh so relative presets re-anchor to "now" instead
  // of serving the window computed when the filter was last touched.
  const [rangeEpoch, setRangeEpoch] = useState(0);

  // The date range always resolves to a concrete window (the API requires
  // from/to): a custom range as picked, otherwise the preset (default 24h).
  const { from, to } = useMemo(() => {
    void rangeEpoch;
    const d = values.date;
    if (d.customRange) return d.customRange;
    return getPresetRange(d.preset ?? "1d");
  }, [values.date, rangeEpoch]);
  const fromIso = from.toISOString();
  const toIso = to.toISOString();
  const timeRangeMs = to.getTime() - from.getTime();

  const facetsQuery = useQuery({
    queryKey: ["event-feed", "facets", fromIso, toIso, kinds],
    queryFn: () =>
      unwrapAsync(
        otelGetEventFacets(client, {
          getEventFacetsPayload: {
            from,
            to,
            kinds: kinds.length > 0 ? kinds : undefined,
          },
        }),
      ),
    throwOnError: false,
  });

  const volumeQuery = useQuery({
    queryKey: [
      "event-feed",
      "volume",
      fromIso,
      toIso,
      kinds,
      sources,
      names,
      searchTerm,
    ],
    queryFn: () =>
      unwrapAsync(
        otelGetEventVolume(client, {
          getEventVolumePayload: {
            from,
            to,
            kinds: kinds.length > 0 ? kinds : undefined,
            sources: sources.length > 0 ? sources : undefined,
            names: names.length > 0 ? names : undefined,
            search: searchTerm || undefined,
          },
        }),
      ),
    throwOnError: false,
  });

  const eventsQuery = useInfiniteQuery({
    queryKey: [
      "event-feed",
      "events",
      fromIso,
      toIso,
      kinds,
      sources,
      names,
      searchTerm,
    ],
    queryFn: ({ pageParam }) =>
      unwrapAsync(
        otelListEventLog(client, {
          listEventLogPayload: {
            from,
            to,
            kinds: kinds.length > 0 ? kinds : undefined,
            sources: sources.length > 0 ? sources : undefined,
            names: names.length > 0 ? names : undefined,
            search: searchTerm || undefined,
            limit: PAGE_SIZE,
            cursor: pageParam,
          },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    throwOnError: false,
  });

  const events = useMemo(
    () => eventsQuery.data?.pages.flatMap((p) => p.events) ?? [],
    [eventsQuery.data],
  );
  const totalCount = eventsQuery.data?.pages[0]?.totalCount;
  const totalCountCapped =
    eventsQuery.data?.pages[0]?.totalCountCapped ?? false;

  const hasActiveFilters =
    kinds.length > 0 ||
    sources.length > 0 ||
    names.length > 0 ||
    searchTerm.length > 0;

  // Reset the list to the top whenever a filter changes, so users don't stay
  // at a stale scroll offset and miss the newly filtered results.
  const filterSignature = [
    fromIso,
    toIso,
    kinds.join(","),
    sources.join(","),
    names.join(","),
    searchTerm,
  ].join("|");
  useEffect(() => {
    containerRef.current?.scrollTo({ top: 0 });
  }, [filterSignature]);

  const optionsById: OptionsById = useMemo(
    () => ({
      kind: KIND_OPTIONS,
      source: (facetsQuery.data?.sources ?? []).map((source) => ({
        label: formatPlatform(source),
        value: source,
      })),
      name: (facetsQuery.data?.names ?? []).map((name) => ({
        label: name,
        value: name,
      })),
    }),
    [facetsQuery.data],
  );

  const { refetch: refetchEvents } = eventsQuery;
  const { refetch: refetchVolume } = volumeQuery;
  const { refetch: refetchFacets } = facetsQuery;
  const hasCustomRange = !!values.date.customRange;
  const refetchAll = useCallback(() => {
    if (hasCustomRange) {
      // Custom windows are fixed, so the query keys don't move: refetch them.
      void refetchEvents();
      void refetchVolume();
      void refetchFacets();
      return;
    }
    // Relative presets re-anchor to "now"; the key change refetches all
    // three queries against the advanced window.
    setRangeEpoch((epoch) => epoch + 1);
  }, [hasCustomRange, refetchEvents, refetchVolume, refetchFacets]);

  const handleScroll = useCallback(
    (e: React.UIEvent<HTMLDivElement>) => {
      const container = e.currentTarget;
      const distanceFromBottom =
        container.scrollHeight - (container.scrollTop + container.clientHeight);

      if (eventsQuery.isFetchingNextPage || eventsQuery.isFetching) return;
      if (!eventsQuery.hasNextPage) return;

      if (distanceFromBottom < 200) {
        void eventsQuery.fetchNextPage();
      }
    },
    [eventsQuery],
  );

  const countLabel =
    totalCount !== undefined
      ? `${events.length.toLocaleString()} of ${totalCountCapped ? "10,000+" : totalCount.toLocaleString()} events`
      : null;

  return (
    <LogWorkbench
      eyebrow="Data"
      title="Event Feed"
      stage="preview"
      description="Every OpenTelemetry log record and span ingested through Speakeasy's /otel/v1 endpoints across this organization."
      filters={
        <Page.Toolbar>
          <Page.Toolbar.Search
            value={search}
            onChange={setSearch}
            debounceMs={300}
            placeholder="Search events"
          />
          <Page.Toolbar.Filters
            schema={EVENT_FEED_FILTERS}
            values={values}
            optionsById={optionsById}
            onChange={setValue as (id: string, value: FilterValue) => void}
            onClear={clearValue as (id: string) => void}
            onClearAll={clearAll}
          />
          {countLabel ? (
            <Page.Toolbar.Actions>
              <span className="text-eyebrow whitespace-nowrap">
                {countLabel}
              </span>
            </Page.Toolbar.Actions>
          ) : null}
          <Page.Toolbar.Refresh
            onRefresh={refetchAll}
            isRefreshing={eventsQuery.isFetching}
          />
        </Page.Toolbar>
      }
      summary={
        <EventVolumeChart
          buckets={volumeQuery.data?.buckets ?? []}
          timeRangeMs={timeRangeMs}
          isLoading={volumeQuery.isPending}
          isError={volumeQuery.isError}
        />
      }
      status={
        eventsQuery.isFetching && events.length > 0 ? (
          <div className="bg-primary/20 h-1 shrink-0">
            <div className="bg-primary h-full animate-pulse" />
          </div>
        ) : null
      }
      header={<EventFeedHeaderRow />}
      footer={
        eventsQuery.hasNextPage ? (
          <EventFeedFooter
            isFetchingNextPage={eventsQuery.isFetchingNextPage}
            onLoadMore={() => void eventsQuery.fetchNextPage()}
          />
        ) : null
      }
      detail={
        <EventDetailSheet
          event={selectedEvent}
          open={!!selectedEvent}
          onOpenChange={(open) => {
            if (!open) setSelectedEvent(null);
          }}
        />
      }
      scrollRef={containerRef}
      onScroll={handleScroll}
    >
      <EventFeedRows
        error={eventsQuery.error}
        isLoading={eventsQuery.isPending}
        events={events}
        hasActiveFilters={hasActiveFilters}
        isFetchingNextPage={eventsQuery.isFetchingNextPage}
        onSelect={setSelectedEvent}
        onRetry={() => void eventsQuery.fetchNextPage()}
      />
    </LogWorkbench>
  );
}

function EventFeedHeaderRow() {
  return (
    <div
      className={cn(
        EVENT_FEED_GRID,
        "text-eyebrow bg-muted/30 shrink-0 items-center border-b px-5 py-2.5 whitespace-nowrap",
      )}
    >
      <div className="min-w-0">Time</div>
      <div className="min-w-0">Kind</div>
      <div className="min-w-0">Source</div>
      <div className="min-w-0">Name</div>
      <div className="min-w-0">Body</div>
    </div>
  );
}

function EventFeedRows({
  error,
  isLoading,
  events,
  hasActiveFilters,
  isFetchingNextPage,
  onSelect,
  onRetry,
}: {
  error: Error | null;
  isLoading: boolean;
  events: EventLogEntry[];
  hasActiveFilters: boolean;
  isFetchingNextPage: boolean;
  onSelect: (event: EventLogEntry) => void;
  onRetry: () => void;
}) {
  // The full-page error view is reserved for an initial load failure; when a
  // later page fails, the rows already loaded stay visible and the error
  // renders as an inline retry strip below them.
  if (error && events.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 py-12">
        <div className="bg-destructive/10 flex size-12 items-center justify-center">
          <Icon name="circle-alert" className="text-destructive size-6" />
        </div>
        <span className="text-foreground font-medium">
          Error loading events
        </span>
        <span className="text-muted-foreground max-w-sm text-center text-sm">
          {error.message}
        </span>
      </div>
    );
  }

  if (isLoading) {
    return <EventFeedSkeletonRows />;
  }

  if (events.length === 0) {
    return (
      <div className="p-4">
        <InlineEmptyState
          icon="inbox"
          heading={hasActiveFilters ? "No matching events" : "No events yet"}
          description={
            hasActiveFilters
              ? "Try adjusting your filters or time range."
              : "Events appear here as OpenTelemetry logs and spans are ingested through Speakeasy."
          }
        />
      </div>
    );
  }

  return (
    <>
      {events.map((event, i) => (
        // Events have no server-side identity (at-least-once ingestion can
        // even surface duplicates), so key by position: the list only ever
        // appends as pages load, keeping index keys stable.
        <EventFeedRow
          key={`${event.timeUnixNano}-${i}`}
          event={event}
          onSelect={onSelect}
        />
      ))}

      {isFetchingNextPage && (
        <div className="text-muted-foreground flex items-center justify-center gap-2 border-t py-4">
          <Icon name="loader-circle" className="size-4 animate-spin" />
          <span className="text-sm">Loading more events...</span>
        </div>
      )}

      {error && !isFetchingNextPage && (
        <div className="flex items-center justify-center gap-3 border-t py-4">
          <Icon name="circle-alert" className="text-destructive size-4" />
          <span className="text-muted-foreground text-sm">
            Failed to load more events.
          </span>
          <Button variant="tertiary" size="sm" onClick={onRetry}>
            Retry
          </Button>
        </div>
      )}
    </>
  );
}

function EventFeedSkeletonRows() {
  return (
    <div aria-label="Loading events" role="status">
      {Array.from({ length: 10 }, (_, i) => (
        <div
          key={i}
          className={cn(
            EVENT_FEED_GRID,
            "items-center border-b px-5 py-2.5 last:border-b-0",
          )}
        >
          <Skeleton className="h-3 w-24" />
          <Skeleton className="h-4 w-10" />
          <Skeleton className="h-3 w-28" />
          <Skeleton className="h-3 w-32" />
          <Skeleton className="h-3 w-full max-w-md" />
        </div>
      ))}
    </div>
  );
}

function EventFeedRow({
  event,
  onSelect,
}: {
  event: EventLogEntry;
  onSelect: (event: EventLogEntry) => void;
}) {
  const timestamp = unixNanoToDate(event.timeUnixNano);

  return (
    <button
      type="button"
      onClick={() => onSelect(event)}
      className={cn(
        EVENT_FEED_GRID,
        "hover:bg-muted/30 w-full cursor-pointer items-center border-b px-5 py-2.5 text-left text-sm transition-colors",
      )}
    >
      <div
        className="text-muted-foreground min-w-0 font-mono text-xs tabular-nums"
        title={timestamp.toLocaleString()}
      >
        {format(timestamp, "MMM d HH:mm:ss")}
      </div>

      <div className="min-w-0">
        <EventKindBadge kind={event.kind} />
      </div>

      <div className="flex min-w-0 items-center gap-2">
        <EventSourceIcon source={event.source} className="size-4 shrink-0" />
        <span className="text-foreground min-w-0 truncate font-mono text-xs">
          {formatPlatform(event.source)}
        </span>
      </div>

      <div className="text-foreground min-w-0 truncate font-mono text-xs font-medium">
        {event.name || "\u2014"}
      </div>

      <div className="text-muted-foreground min-w-0 truncate text-xs">
        {event.bodyPreview || "\u2014"}
      </div>
    </button>
  );
}

function EventFeedFooter({
  isFetchingNextPage,
  onLoadMore,
}: {
  isFetchingNextPage: boolean;
  onLoadMore: () => void;
}) {
  return (
    <div className="bg-muted/30 text-muted-foreground flex shrink-0 items-center justify-between gap-4 border-t px-5 py-3 text-sm">
      <span>Scroll to load more</span>
      <Button
        variant="tertiary"
        size="sm"
        disabled={isFetchingNextPage}
        onClick={onLoadMore}
      >
        {isFetchingNextPage ? "Loading..." : "Load More"}
      </Button>
    </div>
  );
}
