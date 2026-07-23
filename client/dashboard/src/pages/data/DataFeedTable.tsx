import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import { Column, Table } from "@speakeasy-api/moonshine";
import { evaluateQuality, type DataEvent } from "./data-events";
import { KindBadge, QualityPill, SourceBadge } from "./DataEventBadges";

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
    width: "100px",
    render: (event) => <KindBadge kind={event.kind} />,
  },
  {
    key: "source",
    header: "Source",
    width: "150px",
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
    width: "130px",
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
  return (
    <Table
      columns={columns}
      data={events}
      rowKey={(event) => event.id}
      onRowClick={onSelect}
      noResultsMessage={<Type muted>No events match the current filters</Type>}
    />
  );
}
