import type { RankedRow } from "@/components/observe/insights/RankedList";
import {
  completeMeasures,
  measureAlias,
  measureLabel,
  numericCell,
  queryDimensions,
  textCell,
  type ExploreSpec,
  type MeasureDraft,
  type ResultRow,
} from "./exploreModel";

/**
 * The measure a ranking is ranked by: the one the summary is ordered by, or
 * the first. A bar has one length, so the other measures stay in the table.
 */
export function rankedMeasure(spec: ExploreSpec): MeasureDraft | undefined {
  const measures = completeMeasures(spec.measures);
  return (
    measures.find((measure) => measureAlias(measure) === spec.orderBy) ??
    measures[0]
  );
}

/**
 * Summary rows as ranked bars, largest first: one bar per dimension tuple,
 * labelled by its values. The summary keeps the group order unless an order
 * is chosen, so the ranking sorts for itself.
 */
export function rankedRows(
  rows: ResultRow[],
  spec: ExploreSpec,
  measure: MeasureDraft,
): RankedRow[] {
  const dimensions = queryDimensions(spec);
  const alias = measureAlias(measure);
  return rows
    .map((row, index) => ({
      // Tuples are unique within a grouped result, but a label can repeat
      // once blanks read as a dash, so identity is the row's position.
      id: String(index),
      label:
        dimensions.length > 0
          ? dimensions.map((dimension) => textCell(row[dimension])).join(" · ")
          : measureLabel(measure),
      value: numericCell(row[alias]) ?? 0,
    }))
    .sort((a, b) => b.value - a.value);
}
