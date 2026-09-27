import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { JSX } from "react";
import {
  completeMeasures,
  formatMeasureValue,
  isRowsMode,
  measureAlias,
  measureLabel,
  measureUnit,
  numericCell,
  queryDimensions,
  textCell,
  TIME_COLUMN,
  type ExploreSpec,
  type ResultRow,
} from "./exploreModel";

// Result rows are plain maps with no identity of their own, so each is
// carried with its position for the table's row key.
interface Entry {
  row: ResultRow;
  index: number;
}

/**
 * Whole-window figures as a table: one row per dimension tuple with a column
 * per measure, or, when nothing is measured, the rows the query projected,
 * time first.
 */
export function ResultTable({
  dataset,
  spec,
  rows,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  rows: ResultRow[];
}): JSX.Element {
  const columns = isRowsMode(spec)
    ? projectionColumns(rows)
    : groupedColumns(dataset, spec);
  const entries = rows.map((row, index) => ({ row, index }));
  return (
    <Table
      columns={columns}
      data={entries}
      rowKey={(entry) => entry.index}
      noResultsMessage={<Text>No rows</Text>}
    />
  );
}

function groupedColumns(
  dataset: AnalyticsDataset | undefined,
  spec: ExploreSpec,
): Column<Entry>[] {
  const dimensions = queryDimensions(spec).map((dimension): Column<Entry> => ({
    key: dimension,
    header: dimension,
    render: (entry) => (
      <Text className="font-medium">{textCell(entry.row[dimension])}</Text>
    ),
  }));
  const measures = completeMeasures(spec.measures).map(
    (measure): Column<Entry> => {
      const alias = measureAlias(measure);
      const unit = measureUnit(dataset, measure);
      return {
        key: alias,
        header: measureLabel(measure),
        render: (entry) => (
          <Text className="font-mono">
            {formatCell(entry.row[alias], unit)}
          </Text>
        ),
      };
    },
  );
  return [...dimensions, ...measures];
}

// The projection is whatever the server returned, which is the requested
// dimensions plus the time it adds; time leads so the stream reads in order.
function projectionColumns(rows: ResultRow[]): Column<Entry>[] {
  const keys = new Set<string>();
  for (const row of rows) {
    for (const key of Object.keys(row)) keys.add(key);
  }
  const ordered = [
    ...(keys.has(TIME_COLUMN) ? [TIME_COLUMN] : []),
    ...[...keys].filter((key) => key !== TIME_COLUMN),
  ];
  return ordered.map((key) => ({
    key,
    header: key,
    render: (entry: Entry) => (
      <Text className={key === TIME_COLUMN ? "font-mono" : undefined}>
        {key === TIME_COLUMN
          ? formatEventTime(entry.row[key])
          : textCell(entry.row[key])}
      </Text>
    ),
  }));
}

function formatCell(value: unknown, unit: string): string {
  const number = numericCell(value);
  return number === null ? textCell(value) : formatMeasureValue(number, unit);
}

function formatEventTime(value: unknown): string {
  if (typeof value !== "string") return textCell(value);
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}
