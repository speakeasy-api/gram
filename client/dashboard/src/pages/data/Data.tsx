import {
  defineFilters,
  useFilterState,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
import { Page } from "@/components/page-layout";
import { useDeferredValue, useMemo, useState } from "react";
import {
  buildMockEvents,
  evaluateQuality,
  eventUrn,
  EVENT_ORIGINS,
  MOCK_PROJECTS,
  ORIGIN_LABELS,
  type DataEvent,
} from "./data-events";
import { DataEventSheet } from "./DataEventSheet";
import { DataFeedTable } from "./DataFeedTable";

const DATA_FILTERS = defineFilters([
  {
    id: "project",
    label: "Project",
    kind: "multiselect",
    pinned: true,
    allLabel: "All",
  },
  { id: "kind", label: "Kind", kind: "select", pinned: true, allLabel: "All" },
  {
    id: "origin",
    label: "Origin",
    kind: "multiselect",
    pinned: true,
    allLabel: "All",
  },
  { id: "quality", label: "Quality", kind: "select", allLabel: "Any" },
]);

const FILTER_OPTIONS: OptionsById = {
  project: MOCK_PROJECTS.map((project) => ({
    value: project,
    label: project,
  })),
  kind: [
    { value: "log", label: "Log" },
    { value: "metric", label: "Metric" },
  ],
  origin: EVENT_ORIGINS.map((origin) => ({
    value: origin,
    label: ORIGIN_LABELS[origin],
  })),
  quality: [
    { value: "complete", label: "Complete" },
    { value: "partial", label: "Missing attributes" },
    { value: "unclassified", label: "Unclassified" },
  ],
};

function matchesSearch(event: DataEvent, search: string): boolean {
  if (search === "") return true;
  const haystack =
    `${event.type} ${event.producer} ${event.project} ${event.body} ${eventUrn(event)}`.toLowerCase();
  return haystack.includes(search);
}

function DataFeed(): JSX.Element {
  // Static fixtures: this page is a UI prototype and does not query the
  // backend yet. Timestamps are relative to page load so the feed reads as
  // freshly ingested.
  const [events] = useState(() => buildMockEvents());
  const [selected, setSelected] = useState<DataEvent | null>(null);
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);
  const filters = useFilterState(DATA_FILTERS);
  const { project, kind, origin, quality } = filters.values;

  const visibleEvents = useMemo(() => {
    const normalizedSearch = deferredSearch.trim().toLowerCase();

    return events.filter((event) => {
      if (!matchesSearch(event, normalizedSearch)) return false;
      if (project.length > 0 && !project.includes(event.project)) return false;
      if (kind && event.kind !== kind) return false;
      if (origin.length > 0 && !origin.includes(event.origin)) return false;
      if (quality && evaluateQuality(event).grade !== quality) return false;
      return true;
    });
  }, [events, deferredSearch, project, kind, origin, quality]);

  return (
    <div className="space-y-4">
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={search}
          onChange={setSearch}
          debounceMs={150}
          placeholder="Search events"
        />
        <Page.Toolbar.Filters
          schema={DATA_FILTERS}
          values={filters.values}
          optionsById={FILTER_OPTIONS}
          onChange={
            filters.setValue as (id: string, value: FilterValue) => void
          }
          onClear={filters.clearValue as (id: string) => void}
          onClearAll={filters.clearAll}
        />
        <Page.Toolbar.Count>
          {visibleEvents.length} of {events.length} events
        </Page.Toolbar.Count>
      </Page.Toolbar>

      <DataFeedTable events={visibleEvents} onSelect={setSelected} />

      <DataEventSheet event={selected} onClose={() => setSelected(null)} />
    </div>
  );
}

export function DataRoot(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title stage="preview">Event Feed</Page.Section.Title>
          <Page.Section.Description>
            Every event ingested across all projects in your organization,
            newest first. Narrow by physical layout — origin, kind, and type —
            to debug ingest and spot data-quality gaps.
          </Page.Section.Description>
          <Page.Section.Body>
            <DataFeed />
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}
