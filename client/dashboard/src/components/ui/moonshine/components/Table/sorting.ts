import { isGroupOf } from "@/components/ui/moonshine/lib/typeUtils";
import type {
  Column,
  Group,
  SortableColumn,
  SortDescriptor,
  SortDirection,
  SortValue,
} from "./types";

export function isSortableColumn<T extends object>(
  column: Column<T>,
): column is SortableColumn<T> {
  return column.sortable === true;
}

export function getColumnSortId<T extends object>(column: Column<T>): string {
  return column.id ?? column.key.toString();
}

function isGroupedData<T extends object>(
  data: T[] | Group<T>[],
): data is Group<T>[] {
  return data.every((item) => isGroupOf<T>(item));
}

function normalizeSortValue(value: Exclude<SortValue, null | undefined>) {
  return value instanceof Date ? value.getTime() : value;
}

function compareSortValues(a: SortValue, b: SortValue): number {
  if (a == null && b == null) return 0;
  if (a == null) return 1;
  if (b == null) return -1;

  const normalizedA = normalizeSortValue(a);
  const normalizedB = normalizeSortValue(b);

  if (typeof normalizedA === "string" && typeof normalizedB === "string") {
    return normalizedA.localeCompare(normalizedB, undefined, {
      numeric: true,
      sensitivity: "base",
    });
  }

  if (normalizedA === normalizedB) return 0;

  return normalizedA > normalizedB ? 1 : -1;
}

function sortRows<T extends object>(
  rows: T[],
  column: SortableColumn<T>,
  direction: SortDirection,
): T[] {
  return rows
    .map((row, index) => ({ row, index }))
    .sort((a, b) => {
      const aValue = column.sortValue(a.row);
      const bValue = column.sortValue(b.row);

      if (aValue == null && bValue != null) return 1;
      if (bValue == null && aValue != null) return -1;

      const comparison = column.sortCompare
        ? column.sortCompare(a.row, b.row)
        : compareSortValues(aValue, bValue);

      if (comparison === 0) {
        return a.index - b.index;
      }

      return direction === "asc" ? comparison : -comparison;
    })
    .map(({ row }) => row);
}

/**
 * Sorts table data with the same sort descriptors emitted by `Table`.
 *
 * `Table` only renders sortable headers and calls `onSortChange`; it never
 * reorders `data` internally. Use this helper when the caller wants simple
 * client-side sorting for rows already loaded in memory.
 *
 * For grouped data, group order is preserved and only each group's `items`
 * array is sorted. If `sort` is `null` or references a missing/non-sortable
 * column, the original `data` reference is returned unchanged.
 *
 * @example
 * const [sort, setSort] = useState<SortDescriptor | null>(null)
 * const sortedData = sortTableData(data, columns, sort)
 *
 * return (
 *   <Table
 *     columns={columns}
 *     data={sortedData}
 *     rowKey={(row) => row.id}
 *     sort={sort}
 *     onSortChange={setSort}
 *   />
 * )
 */
export function sortTableData<T extends object>(
  data: T[] | Group<T>[],
  columns: Column<T>[],
  sort: SortDescriptor | null | undefined,
): T[] | Group<T>[] {
  if (!sort) {
    return data;
  }

  const column = columns.find(
    (candidate) =>
      getColumnSortId(candidate) === sort.id && isSortableColumn(candidate),
  );

  if (!column || !isSortableColumn(column)) {
    return data;
  }

  if (isGroupedData(data)) {
    return data.map((group) => ({
      ...group,
      items: sortRows(group.items, column, sort.direction),
    }));
  }

  return sortRows(data, column, sort.direction);
}
