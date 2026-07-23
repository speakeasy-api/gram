import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import { Button, Column, Table } from "@speakeasy-api/moonshine";
import { useEffect, useState } from "react";
import { evaluateQuality, type DataEvent } from "./data-events";
import { KindBadge, QualityPill, SourceBadge } from "./DataEventBadges";

const PAGE_SIZE = 25;

const columns: Column<DataEvent>[] = [
  {
    key: "timestamp",
    header: "Time",
    width: "185px",
    // Full timestamps, like a standard log table: the feed is a stream of
    // events, and relative times can't be compared or correlated.
    render: (event) => (
      <Type muted small className="font-mono whitespace-nowrap">
        {dateTimeFormatters.logTimestamp.format(event.timestamp)}
      </Type>
    ),
  },
  {
    key: "kind",
    header: "Kind",
    width: "110px",
    render: (event) => <KindBadge kind={event.kind} />,
  },
  {
    key: "source",
    header: "Source",
    width: "160px",
    render: (event) => <SourceBadge event={event} />,
  },
  {
    key: "type",
    header: "Type",
    width: "180px",
    render: (event) => (
      <Type small className="font-mono">
        {event.type}
      </Type>
    ),
  },
  {
    key: "body",
    header: "Summary",
    render: (event) => (
      <Type muted small className="block max-w-md truncate font-mono">
        {event.body}
      </Type>
    ),
  },
  {
    key: "quality",
    header: "Quality",
    width: "140px",
    render: (event) => <QualityPill quality={evaluateQuality(event)} />,
  },
];

export function DataFeedTable({
  events,
  onSelect,
}: {
  events: DataEvent[];
  onSelect: (event: DataEvent) => void;
}): JSX.Element {
  const [page, setPage] = useState(0);

  // Reset to the first page when the underlying data changes (filters or
  // search narrowed the feed) so the pager never points past the end.
  useEffect(() => {
    setPage(0);
  }, [events]);

  const totalPages = Math.max(1, Math.ceil(events.length / PAGE_SIZE));
  const clampedPage = Math.min(page, totalPages - 1);
  const pageStart = clampedPage * PAGE_SIZE;
  const visibleRows = events.slice(pageStart, pageStart + PAGE_SIZE);

  return (
    <div>
      <Table
        columns={columns}
        data={visibleRows}
        rowKey={(event) => event.id}
        onRowClick={onSelect}
        noResultsMessage={
          <Type muted>No events match the current filters</Type>
        }
      />
      {totalPages > 1 && (
        <div className="flex items-center justify-between border-t px-4 py-3">
          <Type className="text-muted-foreground text-sm">
            {pageStart + 1}-{Math.min(pageStart + PAGE_SIZE, events.length)} of{" "}
            {events.length}
          </Type>
          <div className="flex items-center gap-1">
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => setPage((current) => current - 1)}
              disabled={clampedPage === 0}
            >
              Previous
            </Button>
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => setPage((current) => current + 1)}
              disabled={clampedPage >= totalPages - 1}
            >
              Next
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
