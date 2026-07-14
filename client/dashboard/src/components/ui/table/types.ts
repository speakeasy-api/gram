import type { PropsWithChildren, ReactNode } from "react";

export type SortDirection = "asc" | "desc";

export type SortValue = string | number | boolean | Date | null | undefined;

type ColumnKey<T extends object> = keyof T | string;

export type SortDescriptor = {
  id: string;
  direction: SortDirection;
};

type BaseColumn<T extends object> = {
  key: ColumnKey<T>;
  id?: string;
  header: ReactNode;
  render?: (row: T) => ReactNode;
  width?: `${number}fr` | `${number}px` | "auto" | undefined;
};

export type SortableColumn<T extends object> = BaseColumn<T> & {
  sortable: true;
  sortLabel?: string;
  sortValue: (row: T) => SortValue;
  sortCompare?: (a: T, b: T) => number;
};

type UnsortableColumn<T extends object> = BaseColumn<T> & {
  sortable?: false;
  sortLabel?: never;
  sortValue?: never;
  sortCompare?: never;
};

export type Column<T extends object> = SortableColumn<T> | UnsortableColumn<T>;

export type Group<T extends object> = {
  key: string;
  items: T[];
  [k: string]: unknown;
};

export type CellPadding = "normal" | "condensed" | "spacious";

export type PropsWithChildrenAndClassName = PropsWithChildren<{
  className?: string;
}>;

export type TableProps<T extends object> = {
  columns: Column<T>[];
  data: T[] | Group<T>[];
  rowKey: (row: T) => string | number;
  onRowClick?: (row: T) => void;
  renderGroupHeader?: (group: Group<T>) => ReactNode;
  renderExpandedContent?: (row: T) => ReactNode;
  /**
   * Only mount the expanded content while a row is actually open, and always
   * show the expand chevron (rather than probing `renderExpandedContent` for
   * every row up front to decide whether a chevron is warranted). Use this when
   * the expanded content is expensive to mount — e.g. it fires a network
   * request that should run only on expand. Every row is assumed expandable.
   */
  lazyExpandedContent?: boolean;
  onLoadMore?: () => Promise<void> | (() => void);
  hasMore?: boolean;
  noResultsMessage?: ReactNode;
  className?: string;
  cellPadding?: CellPadding;
  hideHeader?: boolean;
  sort?: SortDescriptor | null;
  onSortChange?: (sort: SortDescriptor | null) => void;
};

export type TableWrapperProps<T extends object> =
  PropsWithChildrenAndClassName & {
    columns: Column<T>[];
    cellPadding?: CellPadding;
  };
