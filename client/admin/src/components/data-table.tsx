// oxlint-disable react/only-export-components -- compound component (Object.assign) pattern
import {
  columnVisibilityFeature,
  createColumnHelper,
  FlexRender,
  metaHelper,
  rowSelectionFeature,
  tableFeatures,
  type DisplayColumnDef,
  type ReactTable,
  type Row,
  type RowData,
} from "@tanstack/react-table";
import * as React from "react";

import { cn } from "@/lib/utils";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

/**
 * Presentational wrapper around the table primitives, driven by a TanStack
 * table instance.
 *
 * Compose it: `DataTable.Header`, `DataTable.Body`, `DataTable.Row`. A page
 * keeps its own per-row classes and handlers that way.
 *
 * The `ui/table` primitives are stock shadcn, so the padding scale lives here
 * rather than in the primitive.
 */

/**
 * Per-column classes, because the header and the body cell are rendered here
 * rather than by the page, and the name a column lists itself under when its
 * header is not a string.
 */
export type DataTableColumnMeta = {
  headClassName?: string;
  cellClassName?: string;
  label?: string;
};

/**
 * The feature registry every admin table shares.
 *
 * Column visibility gates `row.getVisibleCells`, `column.getIsVisible` and
 * `column.getCanHide`, which this wrapper and its header both call. A table
 * with no Columns control still registers it for that reason.
 *
 * Row selection is registered here and drawn nowhere: it adds no column and no
 * state until a table asks for one by putting `selectColumn` in its columns.
 */
export const dataTableFeatures = tableFeatures({
  columnVisibilityFeature,
  rowSelectionFeature,
  columnMeta: metaHelper<DataTableColumnMeta>(),
});

export type DataTableFeatures = typeof dataTableFeatures;

export type DataTableInstance<T extends RowData> = ReactTable<
  DataTableFeatures,
  T
>;

export const SELECT_COLUMN_ID = "select";

/**
 * The opt-in select column: a checkbox per row and one in the header that ticks
 * and unticks the rows the table is currently holding.
 *
 * Opt in by putting it first in a page's columns. A page that leaves it out
 * gets no checkbox anywhere, which is why this is a column rather than
 * something the wrapper draws on its own.
 *
 * Both labels are the caller's, because a checkbox has no text of its own and
 * "Select row" repeated down a table tells a screen reader nothing about which
 * record it is on.
 */
export function selectColumn<T extends RowData>({
  allLabel,
  rowLabel,
}: {
  allLabel: string;
  rowLabel: (row: T) => string;
}): DisplayColumnDef<DataTableFeatures, T, unknown> {
  const column = createColumnHelper<DataTableFeatures, T>();
  return column.display({
    id: SELECT_COLUMN_ID,
    meta: { label: "Select" },
    // Hiding it would strand a selection the operator could no longer see or
    // undo, the same reason the row controls opt out.
    enableHiding: false,
    header: ({ table }) => (
      <Checkbox
        aria-label={allLabel}
        // Mixed rather than unchecked while only some rows are ticked. It is
        // the state ARIA has for this, and the next press clears rather than
        // extends, which an unchecked box would misstate.
        checked={
          table.getIsAllPageRowsSelected() ||
          (table.getIsSomePageRowsSelected() && "indeterminate")
        }
        onCheckedChange={(checked) => {
          table.toggleAllPageRowsSelected(checked === true);
        }}
      />
    ),
    cell: ({ row }) => (
      <Checkbox
        aria-label={rowLabel(row.original)}
        checked={row.getIsSelected()}
        onCheckedChange={(checked) => {
          row.toggleSelected(checked === true);
        }}
      />
    ),
  });
}

export type TableCellPadding = "condensed" | "normal" | "spacious";

// Applied to the table root so the scale reaches every th and td, including
// the ones a page composes by hand.
const cellPaddingClasses: Record<TableCellPadding, string> = {
  condensed: "[&_th]:h-8 [&_th]:px-2 [&_td]:px-2 [&_td]:py-1",
  normal: "",
  spacious: "[&_th]:h-12 [&_th]:px-4 [&_td]:px-4 [&_td]:py-3",
};

function DataTableRoot({
  cellPadding = "normal",
  className,
  children,
}: {
  cellPadding?: TableCellPadding;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    // The stock table container sets `overflow-x: auto`, which also turns it
    // into a vertical scroll box. The sticky header would then pin to that box
    // instead of the scroll box the page provides. Turn the scroll box off, so
    // the page keeps both the scrolling and the pinned header.
    <div className="[&>[data-slot=table-container]]:overflow-visible">
      <Table className={cn(cellPaddingClasses[cellPadding], className)}>
        {children}
      </Table>
    </div>
  );
}

function DataTableHeader<T extends RowData>({
  table,
  className,
}: {
  table: DataTableInstance<T>;
  className?: string;
}) {
  return (
    <TableHeader className={cn("bg-muted sticky top-0 z-10", className)}>
      {table.getHeaderGroups().map((group) => (
        <TableRow key={group.id}>
          {group.headers.map((header) => (
            // A placeholder header and a colSpan above 1 both appear only when
            // columns are grouped. Handling one and not the other would leave a
            // group heading sitting over a single column.
            <TableHead
              key={header.id}
              colSpan={header.colSpan}
              className={header.column.columnDef.meta?.headClassName}
            >
              {header.isPlaceholder ? null : <FlexRender header={header} />}
            </TableHead>
          ))}
        </TableRow>
      ))}
    </TableHeader>
  );
}

function DataTableRow<T extends RowData>({
  row,
  onClick,
  onAltClick,
  className,
  ref,
}: {
  row: Row<DataTableFeatures, T>;
  onClick?: (row: T, event: React.MouseEvent<HTMLTableRowElement>) => void;
  onAltClick?: (row: T, event: React.MouseEvent<HTMLTableRowElement>) => void;
  className?: string;
  ref?: React.Ref<HTMLTableRowElement>;
}) {
  // The row itself stays a plain row: it takes no focus and it holds no
  // `button` role, because either one breaks the table structure the
  // assistive technology walks. A clickable row instead carries a real link
  // in one of its cells, and that link owns the keyboard path and the
  // accessible name. This handler only widens the mouse target.
  const handleClick =
    onClick || onAltClick
      ? (event: React.MouseEvent<HTMLTableRowElement>) => {
          // The link in the cell already navigates, and it also lets the
          // operator open the record in a new tab.
          const control = (event.target as HTMLElement).closest(
            "a,button,input,label",
          );

          const onRowOrLink = control === null || control.matches("a");

          // Alt turns a link's default into "save link", never a navigation,
          // so a row that claims the gesture cancels that download whatever
          // else is held down. Peeking is the stricter case: Alt on its own,
          // because Alt with a second modifier is a gesture nobody aimed here.
          if (onAltClick && event.altKey) {
            if (onRowOrLink) {
              event.preventDefault();
              if (!event.ctrlKey && !event.metaKey && !event.shiftKey) {
                onAltClick(row.original, event);
              }
            }
            return;
          }

          // Open-in-tab and open-in-window belong to the link in the name
          // cell. Answering them anywhere else in the row would navigate this
          // tab out from under the one the operator is opening.
          if (event.ctrlKey || event.metaKey || event.shiftKey) return;

          if (!onClick || control) return;
          onClick(row.original, event);
        }
      : undefined;

  return (
    <TableRow
      ref={ref}
      className={cn(onClick && "cursor-pointer", className)}
      onClick={handleClick}
    >
      {row.getVisibleCells().map((cell) => (
        <TableCell
          key={cell.id}
          className={cell.column.columnDef.meta?.cellClassName}
        >
          <FlexRender cell={cell} />
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
