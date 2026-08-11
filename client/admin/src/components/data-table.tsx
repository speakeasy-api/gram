// oxlint-disable react/only-export-components -- compound component (Object.assign) pattern
import * as React from "react";

import { cn } from "@/lib/utils";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

/**
 * Column-driven wrapper around the table primitives.
 *
 * Two ways to use it:
 *
 *   - pass `data` + `rowKey` and let it render the header, the rows and the
 *     empty state;
 *   - pass children and compose `DataTable.Header` / `DataTable.Body` /
 *     `DataTable.Row` yourself when a page needs per-row classes or handlers.
 *
 * The `ui/table` primitives are stock shadcn, so the column sizing and the
 * padding scale live here rather than in the primitive.
 */

export type TableCellPadding = "condensed" | "normal" | "spacious";

/**
 * `'auto'` sizes a column to its content. Everything else takes the width
 * as given.
 */
export type ColumnWidth = "auto" | `${number}px` | `${number}%`;

export type Column<T extends object> = {
  key: keyof T | string;
  header: React.ReactNode;
  render?: (row: T) => React.ReactNode;
  width?: ColumnWidth;
};

// Applied to the table root so the scale reaches every th and td, including
// the ones a page composes by hand.
const cellPaddingClasses: Record<TableCellPadding, string> = {
  condensed: "[&_th]:h-8 [&_th]:px-2 [&_td]:px-2 [&_td]:py-1",
  normal: "",
  spacious: "[&_th]:h-12 [&_th]:px-4 [&_td]:px-4 [&_td]:py-3",
};

function columnStyle<T extends object>(
  column: Column<T>,
): React.CSSProperties | undefined {
  if (!column.width) return undefined;
  // A width of 1% collapses the column onto its content under `table-layout:
  // auto`, which is what `'auto'` meant on the old grid table.
  return { width: column.width === "auto" ? "1%" : column.width };
}

type SharedProps<T extends object> = {
  columns: Column<T>[];
  cellPadding?: TableCellPadding;
  className?: string;
};

type WrapperProps<T extends object> = SharedProps<T> & {
  children: React.ReactNode;
};

type DataProps<T extends object> = SharedProps<T> & {
  data: T[];
  rowKey: (row: T) => string | number;
  onRowClick?: (row: T) => void;
  noResultsMessage?: React.ReactNode;
};

function isWrapperProps<T extends object>(
  props: WrapperProps<T> | DataProps<T>,
): props is WrapperProps<T> {
  return "children" in props && props.children !== undefined;
}

function cellContent<T extends object>(row: T, column: Column<T>) {
  if (column.render) {
    return column.render(row);
  }

  return column.key in row ? String(row[column.key as keyof T]) : "";
}

function DataTableRoot<T extends object>(
  props: WrapperProps<T> | DataProps<T>,
) {
  const { columns, cellPadding = "normal", className } = props;

  const content = isWrapperProps(props) ? (
    props.children
  ) : (
    <>
      <DataTableHeader columns={columns} />
      <TableBody>
        {props.data.length === 0 ? (
          <DataTableNoResultsMessage>
            {props.noResultsMessage}
          </DataTableNoResultsMessage>
        ) : (
          props.data.map((row) => (
            <DataTableRow
              key={props.rowKey(row)}
              row={row}
              columns={columns}
              onClick={props.onRowClick}
            />
          ))
        )}
      </TableBody>
    </>
  );

  return (
    // The stock table container sets `overflow-x: auto`, which also turns it
    // into a vertical scroll box. The sticky header would then pin to that box
    // instead of the scroll box the page provides. Turn the scroll box off, so
    // the page keeps both the scrolling and the pinned header.
    <div className="[&>[data-slot=table-container]]:overflow-visible">
      <Table className={cn(cellPaddingClasses[cellPadding], className)}>
        {content}
      </Table>
    </div>
  );
}

function DataTableHeader<T extends object>({
  columns,
  className,
}: {
  columns: Column<T>[];
  className?: string;
}) {
  return (
    <TableHeader className={cn("bg-muted sticky top-0 z-10", className)}>
      <TableRow>
        {columns.map((column) => (
          <TableHead key={String(column.key)} style={columnStyle(column)}>
            {column.header}
          </TableHead>
        ))}
      </TableRow>
    </TableHeader>
  );
}

function DataTableRow<T extends object>({
  row,
  columns,
  onClick,
  className,
}: {
  row: T;
  columns: Column<T>[];
  onClick?: (row: T) => void;
  className?: string;
}) {
  const handleKeyDown = onClick
    ? (event: React.KeyboardEvent<HTMLTableRowElement>) => {
        // Buttons and links inside a cell handle their own keys and bubble the
        // event up, so only act when the row itself is the focused element.
        if (event.target !== event.currentTarget) return;
        if (event.key !== "Enter" && event.key !== " ") return;
        // Space would otherwise scroll the page.
        event.preventDefault();
        onClick(row);
      }
    : undefined;

  return (
    <TableRow
      className={cn(
        onClick &&
          "cursor-pointer focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring",
        className,
      )}
      // A row keeps its implicit `row` role: `button` would break the table
      // structure the assistive technology walks.
      tabIndex={onClick ? 0 : undefined}
      onClick={onClick ? () => onClick(row) : undefined}
      onKeyDown={handleKeyDown}
    >
      {columns.map((column) => (
        <TableCell key={String(column.key)}>
          {cellContent(row, column)}
        </TableCell>
      ))}
    </TableRow>
  );
}

function DataTableNoResultsMessage({
  className,
  children,
  ...props
}: React.ComponentProps<"td">) {
  return (
    <TableRow className="hover:bg-transparent">
      {/* The row spans the table, and callers do not know the column count.
          1000 is the maximum colspan the HTML specification allows. */}
      <TableCell
        colSpan={1000}
        className={cn("h-24 text-center", className)}
        {...props}
      >
        {children}
      </TableCell>
    </TableRow>
  );
}

export const DataTable = Object.assign(DataTableRoot, {
  Header: DataTableHeader,
  Body: TableBody,
  Row: DataTableRow,
  Cell: TableCell,
  NoResultsMessage: DataTableNoResultsMessage,
});
