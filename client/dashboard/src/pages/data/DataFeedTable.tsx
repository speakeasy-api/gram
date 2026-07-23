import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { Column, Table } from "@speakeasy-api/moonshine";
import { evaluateQuality, type DataEvent } from "./data-events";
import { KindBadge, OriginBadge, QualityPill } from "./DataEventBadges";

const columns: Column<DataEvent>[] = [
  {
    key: "timestamp",
    header: "Time",
    width: "130px",
    render: (event) => (
      <Type muted small className="whitespace-nowrap">
        <HumanizeDateTime date={event.timestamp} />
      </Type>
    ),
  },
  {
    key: "project",
    header: "Project",
    width: "130px",
    render: (event) => (
      <Type muted small className="font-mono">
        {event.project}
      </Type>
    ),
  },
  {
    key: "kind",
    header: "Kind",
    width: "90px",
    render: (event) => <KindBadge kind={event.kind} />,
  },
  {
    key: "origin",
    header: "Origin",
    width: "140px",
    render: (event) => <OriginBadge origin={event.origin} />,
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
    key: "producer",
    header: "Producer",
    width: "150px",
    render: (event) => <Type small>{event.producer}</Type>,
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
