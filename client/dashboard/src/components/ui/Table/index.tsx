// TODO: https://linear.app/speakeasy/issue/SXF-170/table-component
import React, {
  forwardRef,
  PropsWithChildren,
  type ReactNode,
  useCallback,
  useMemo,
  useRef,
  useState,
} from "react";
import { cn } from "@/lib/utils";
import { ArrowDown, ArrowUp, ArrowUpDown, Loader2 } from "lucide-react";
import { isGroupOf } from "@/components/ui/lib/typeUtils";
import styles from "./styles.module.css";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { Button } from "@/components/ui/Button";
import { ExpandChevron } from "./expand-chevron";
import { TableProvider } from "./context/tableProvider";
import { useTable } from "./context/context";
import { getColumnSortId, isSortableColumn } from "./sorting";

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

export type RenderRow<T extends object> = (
  row: T,
  rowElement: React.ReactElement,
) => ReactNode;

type CellPadding = "normal" | "condensed" | "spacious";

type PropsWithChildrenAndClassName = PropsWithChildren<{ className?: string }>;

export type TableProps<T extends object> = {
  columns: Column<T>[];
  data: T[] | Group<T>[];
  rowKey: (row: T) => string | number;
  onRowClick?: (row: T) => void;
  /**
   * Wrap each data row. `rowElement` is the row's `<tr>` element, which
   * forwards refs and extra props, so it can back e.g. a Radix
   * `ContextMenuTrigger asChild`. Return `rowElement` (possibly wrapped)
   * to render the row.
   */
  renderRow?: RenderRow<T>;
  renderGroupHeader?: (group: Group<T>) => ReactNode;
  renderExpandedContent?: (row: T) => ReactNode;
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

function expandColumn<T extends object>(): Column<T> {
  return {
    key: "expand",
    header: "",
    width: `64px`, // 32px is padding, 32px is the width of the expand button
  };
}

function warnOnDuplicateSortableIds<T extends object>(columns: Column<T>[]) {
  if (process.env.NODE_ENV === "production") {
    return;
  }

  const seen = new Set<string>();

  for (const column of columns) {
    if (!isSortableColumn(column)) {
      continue;
    }

    const id = getColumnSortId(column);

    if (seen.has(id)) {
      console.warn(
        `Table sortable columns must have unique ids. Duplicate id: ${id}`,
      );
      return;
    }

    seen.add(id);
  }
}

type TableContainerProps = PropsWithChildrenAndClassName & {
  tableDepth: number;
  colWidths: string;
  cellPadding?: CellPadding;
  expandedRowKeys?: Set<string | number>;
};

const TableContainer = forwardRef<HTMLTableElement, TableContainerProps>(
  (
    {
      children,
      className,
      tableDepth,
      colWidths,
      cellPadding,
      expandedRowKeys,
    },
    ref,
  ) => {
    return (
      <TableProvider depth={tableDepth} expandedRowKeys={expandedRowKeys}>
        <table
          style={
            { "--grid-template-columns": colWidths } as React.CSSProperties
          }
          ref={ref}
          className={cn(
            styles.table,
            "relative grid w-full caption-bottom [border-collapse:separate] [border-spacing:0] [grid-template-columns:var(--grid-template-columns)] overflow-x-auto overflow-y-hidden border text-sm",
            tableDepth > 1 && "border-none",
            className,
          )}
          data-cell-padding={cellPadding}
        >
          {children}
        </table>
      </TableProvider>
    );
  },
);

function TableRoot<T extends object>(
  props: TableProps<T> | TableWrapperProps<T>,
): React.JSX.Element {
  const { depth } = useTable();
  const tableDepth = depth + 1;

  const tableBodyRef = useRef<HTMLTableSectionElement | null>(null);
  const tableRef = useRef<HTMLTableElement | null>(null);

  let columns = props.columns;

  // We add the expand column here so that all parts of the table know about it, particularly needed for widths
  if (
    !propsHasChildren<TableWrapperProps<T>, TableProps<T>>(props) &&
    props.renderExpandedContent
  ) {
    columns = [expandColumn(), ...columns];
  }

  warnOnDuplicateSortableIds(columns);

  const colWidths = useMemo(
    () => columns.map((column) => column.width ?? "1fr").join(" "),
    [columns],
  );

  // Hooks run before the wrapper-form early return so their order is stable
  // across both shapes of this component.
  const [isLoading, setIsLoading] = useState(false);

  const expandedRowKeys = useMemo(() => {
    if (propsHasChildren<TableWrapperProps<T>, TableProps<T>>(props)) return;

    const { data, rowKey } = props;
    if (isGroupOf<T>(data)) return;

    return new Set(
      data
        .filter((row) => "defaultExpanded" in row && row.defaultExpanded)
        .map((row) => rowKey(row as T)),
    );
  }, [props]);

  if (propsHasChildren<TableWrapperProps<T>, TableProps<T>>(props)) {
    return (
      <TableContainer
        className={props.className}
        colWidths={colWidths}
        tableDepth={tableDepth}
        cellPadding={props.cellPadding}
        ref={tableRef}
      >
        {props.children}
      </TableContainer>
    );
  }

  const {
    data,
    rowKey,
    onRowClick,
    renderRow,
    onLoadMore,
    hasMore,
    noResultsMessage,
    renderGroupHeader,
    renderExpandedContent,
    className,
    hideHeader,
    cellPadding,
    sort,
    onSortChange,
  } = props;

  const handleLoadMore = async () => {
    setIsLoading(true);
    await onLoadMore?.();
    setIsLoading(false);
  };

  return (
    <TableContainer
      className={className}
      colWidths={colWidths}
      tableDepth={tableDepth}
      cellPadding={cellPadding}
      ref={tableRef}
      expandedRowKeys={expandedRowKeys}
    >
      {!hideHeader && (
        <TableRoot.Header
          columns={columns}
          sort={sort}
          onSortChange={onSortChange}
        />
      )}
      <TableRoot.Body
        data={data}
        ref={tableBodyRef}
        columns={columns}
        rowKey={rowKey}
        hasMore={hasMore}
        noResultsMessage={noResultsMessage}
        renderGroupHeader={renderGroupHeader}
        renderExpandedContent={renderExpandedContent}
        handleLoadMore={handleLoadMore}
        isLoading={isLoading}
        onRowClick={onRowClick}
        renderRow={renderRow}
      />
    </TableContainer>
  );
}

type HeaderProps<T extends object> = {
  columns: Column<T>[];
  sort?: SortDescriptor | null;
  onSortChange?: (sort: SortDescriptor | null) => void;
  className?: string;
};

function HeaderContainer({
  className,
  children,
}: PropsWithChildrenAndClassName) {
  return (
    <thead
      className={cn(
        "[grid-column:1/-1] grid [grid-template-columns:subgrid]",
        className,
      )}
    >
      {children}
    </thead>
  );
}

function getSortDirection<T extends object>(
  column: Column<T>,
  sort: SortDescriptor | null | undefined,
) {
  return sort?.id === getColumnSortId(column) ? sort.direction : undefined;
}

function getSortLabel<T extends object>(column: SortableColumn<T>) {
  return (
    column.sortLabel ??
    (typeof column.header === "string"
      ? column.header
      : getColumnSortId(column))
  );
}

function getNextSort<T extends object>(
  column: SortableColumn<T>,
  sort: SortDescriptor | null | undefined,
): SortDescriptor | null {
  const direction = getSortDirection(column, sort);
  const id = getColumnSortId(column);

  if (direction === "asc") {
    return { id, direction: "desc" };
  }

  if (direction === "desc") {
    return null;
  }

  return { id, direction: "asc" };
}

function getSortButtonLabel<T extends object>(
  column: SortableColumn<T>,
  sort: SortDescriptor | null | undefined,
) {
  const label = getSortLabel(column);
  const direction = getSortDirection(column, sort);

  if (direction === "asc") {
    return `Sort by ${label} descending`;
  }

  if (direction === "desc") {
    return `Clear sort for ${label}`;
  }

  return `Sort by ${label} ascending`;
}

function SortableHeaderCell<T extends object>({
  column,
  sort,
  onSortChange,
}: {
  column: Column<T>;
  sort?: SortDescriptor | null;
  onSortChange?: (sort: SortDescriptor | null) => void;
}) {
  if (!isSortableColumn(column) || !onSortChange) {
    return <HeaderCell>{column.header}</HeaderCell>;
  }

  const direction = getSortDirection(column, sort);
  const isSorted = direction !== undefined;
  const Icon =
    direction === "asc"
      ? ArrowUp
      : direction === "desc"
        ? ArrowDown
        : ArrowUpDown;

  return (
    <HeaderCell
      aria-sort={
        direction === "asc"
          ? "ascending"
          : direction === "desc"
            ? "descending"
            : undefined
      }
    >
      <button
        type="button"
        className="group flex h-full w-full min-w-0 items-center gap-1 text-left"
        aria-label={getSortButtonLabel(column, sort)}
        onClick={() => onSortChange(getNextSort(column, sort))}
      >
        {/* The header cell's eyebrow styling reaches the button through
            inheritance except for text-transform, which the browser's form-
            control styling drops -- so a sortable header would read in
            sentence case beside its uppercase unsortable neighbours. */}
        <span className="min-w-0 truncate uppercase">{column.header}</span>
        <Icon
          aria-hidden="true"
          className={cn(
            "size-3.5 shrink-0 transition-colors",
            isSorted ? "text-body" : "text-body-muted group-hover:text-body",
          )}
        />
      </button>
    </HeaderCell>
  );
}

function Header<T extends object>(
  props: HeaderProps<T> | PropsWithChildrenAndClassName,
) {
  if (propsHasChildren<PropsWithChildrenAndClassName, HeaderProps<T>>(props)) {
    return (
      <HeaderContainer className={props.className}>
        {props.children}
      </HeaderContainer>
    );
  }

  return (
    <HeaderContainer className={props.className}>
      <tr className="table-header [grid-column:1/-1] grid [grid-template-columns:subgrid] border-b">
        {props.columns.map((column) => (
          <SortableHeaderCell
            key={`${column.key.toString()}-${column.id ?? ""}`}
            column={column}
            sort={props.sort}
            onSortChange={props.onSortChange}
          />
        ))}
      </tr>
    </HeaderContainer>
  );
}

type BodyProps<T extends object> = {
  columns: Column<T>[];
  data: T[] | Group<T>[];
  rowKey: (row: T) => string | number;
  onRowClick?: (row: T) => void;
  isRowClickable?: (row: T) => boolean;
  renderRow?: RenderRow<T>;
  noResultsMessage?: ReactNode;
  renderGroupHeader?: (group: Group<T>) => ReactNode;
  renderExpandedContent?: (row: T) => ReactNode;
  hasMore?: boolean;
  handleLoadMore?: () => void | Promise<void>;
  isLoading?: boolean;
  className?: string;
};

const BodyContainer = forwardRef<
  HTMLTableSectionElement,
  PropsWithChildrenAndClassName
>(({ className, children }, ref) => {
  return (
    <tbody
      ref={ref}
      className={cn(
        "relative [grid-column:1/-1] grid [grid-template-columns:subgrid]",
        className,
      )}
    >
      {children}
    </tbody>
  );
});

const Body = React.forwardRef(function Body<T extends object>(
  props: BodyProps<T> | PropsWithChildrenAndClassName,
  ref: React.ForwardedRef<HTMLTableSectionElement>,
) {
  if (propsHasChildren<PropsWithChildrenAndClassName, BodyProps<T>>(props)) {
    return (
      <BodyContainer ref={ref} className={props.className}>
        {props.children}
      </BodyContainer>
    );
  }

  const {
    data,
    columns,
    rowKey,
    hasMore,
    onRowClick,
    isRowClickable,
    renderRow,
    noResultsMessage,
    renderGroupHeader,
    handleLoadMore,
    isLoading,
    className,
    renderExpandedContent,
  } = props;

  const renderBodyRow = (row: T | Group<T>) => {
    if (isGroupOf<T>(row)) {
      return (
        <RowGroup
          group={row}
          columns={columns}
          rowKey={rowKey}
          renderGroupHeader={renderGroupHeader}
          key={row.key}
          onRowClick={onRowClick}
          isRowClickable={isRowClickable}
          renderRow={renderRow}
        />
      );
    } else if (renderExpandedContent) {
      return (
        <RowExpandable
          row={row}
          columns={columns}
          rowKey={rowKey}
          renderExpandedContent={renderExpandedContent}
          key={rowKey(row)}
          onClick={isRowClickable?.(row) === false ? undefined : onRowClick}
          renderRow={renderRow}
        />
      );
    } else {
      return (
        <Row
          row={row}
          key={rowKey(row)}
          columns={columns}
          onClick={isRowClickable?.(row) === false ? undefined : onRowClick}
          renderRow={renderRow}
        />
      );
    }
  };

  return (
    <BodyContainer ref={ref} className={cn(hasMore && "pb-16", className)}>
      {data.length === 0 ? (
        <NoResultsMessage>{noResultsMessage}</NoResultsMessage>
      ) : (
        data.map(renderBodyRow)
      )}
      {hasMore && handleLoadMore && (
        <LoadMore
          columns={columns}
          handleLoadMore={handleLoadMore}
          isLoading={isLoading}
        />
      )}
    </BodyContainer>
  );
}) as <T extends object>(
  props: {
    ref?: React.ForwardedRef<HTMLTableSectionElement>;
  } & (BodyProps<T> | PropsWithChildrenAndClassName),
) => React.ReactElement;

type RowProps<T extends object> = {
  row: T;
  onClick?: (row: T) => void;
  columns: Column<T>[];
  className?: string;
  renderRow?: RenderRow<T>;
};

type RowContainerProps = {
  onClick?: () => void;
} & PropsWithChildrenAndClassName &
  Omit<
    React.ComponentPropsWithoutRef<"tr">,
    "onClick" | "className" | "children"
  >;

const RowContainer = forwardRef<HTMLTableRowElement, RowContainerProps>(
  function RowContainer(
    { className, children, onClick, onKeyDown, tabIndex, ...rest },
    ref,
  ) {
    const isClickable = Boolean(onClick);

    const handleKeyDown = (event: React.KeyboardEvent<HTMLTableRowElement>) => {
      onKeyDown?.(event);
      if (event.defaultPrevented || !onClick) {
        return;
      }
      // Only the row itself activates on Enter/Space. Key events from a focused
      // nested control (button, link, input, …) must reach that control instead.
      if (event.target !== event.currentTarget) {
        return;
      }
      if (event.key !== "Enter" && event.key !== " ") {
        return;
      }
      event.preventDefault();
      onClick();
    };

    return (
      <tr
        ref={ref}
        {...rest}
        className={cn(
          // Hovers use the accent token, not muted: in dark mode muted equals
          // the card surface, so a muted hover paints invisibly.
          "-z-0 [grid-column:1/-1] grid max-w-full [grid-template-columns:subgrid] border-b transition-colors last:border-none hover:bg-accent/50 data-[state=selected]:bg-muted",
          // Clickable rows highlight at full accent so the hover reads as an
          // affordance, not just a scan aid.
          isClickable &&
            "cursor-pointer hover:bg-accent focus-visible:bg-accent focus-visible:ring-ring focus-visible:ring-2 focus-visible:ring-inset focus-visible:outline-none",
          className,
        )}
        onClick={onClick}
        onKeyDown={isClickable ? handleKeyDown : onKeyDown}
        tabIndex={isClickable ? 0 : tabIndex}
      >
        {children}
      </tr>
    );
  },
);

function Row<T extends object>(props: RowProps<T> | RowContainerProps) {
  if (propsHasChildren<RowContainerProps, RowProps<T>>(props)) {
    const { className, onClick, children, ...rest } = props;
    return (
      <RowContainer className={className} onClick={onClick} {...rest}>
        {children}
      </RowContainer>
    );
  }

  const { row, onClick, columns, className, renderRow } = props;
  const rowElement = (
    <RowContainer
      className={className}
      onClick={onClick ? () => onClick(row) : undefined}
    >
      {columns.map((column) => (
        <Cell key={column.key.toString()} column={column} row={row} />
      ))}
    </RowContainer>
  );
  return <>{renderRow ? renderRow(row, rowElement) : rowElement}</>;
}

function RowExpandable<T extends object>({
  row,
  onClick,
  columns,
  rowKey,
  renderExpandedContent,
  className,
  renderRow,
}: {
  row: T;
  columns: Column<T>[];
  rowKey: (row: T) => string | number;
  renderExpandedContent: (row: T) => ReactNode;
  onClick?: (row: T) => void;
  className?: string;
  renderRow?: RenderRow<T>;
}) {
  const { expandedRowKeys, toggleExpanded } = useTable();

  const isExpanded = expandedRowKeys.has(rowKey(row));

  const expand = useCallback(
    (e: React.MouseEvent<HTMLButtonElement>) => {
      e.stopPropagation();
      toggleExpanded(rowKey(row));
    },
    [row, rowKey, toggleExpanded],
  );

  const content = useMemo(
    () => renderExpandedContent(row),
    [renderExpandedContent, row],
  );

  const renderExpandCol = useCallback(() => {
    return content ? (
      <TooltipProvider>
        <Tooltip delayDuration={0}>
          <TooltipTrigger asChild>
            <div className="flex w-full justify-end">
              <Button
                onClick={expand}
                variant={"tertiary"}
                className={`h-6 w-6`}
              >
                <ExpandChevron isCollapsed={!isExpanded} />
              </Button>
            </div>
          </TooltipTrigger>
          <TooltipContent>{isExpanded ? "Collapse" : "Expand"}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    ) : null;
  }, [expand, isExpanded, content]);

  const expandCol = columns.find((column) => column.key === expandColumn().key);

  if (expandCol) {
    expandCol.render = renderExpandCol;
  }

  let onClickFn = onClick;

  // If there's some expanded content to show and onClick is not provided, let the row expand when clicked
  if (!onClick && content) {
    onClickFn = () => {
      toggleExpanded(rowKey(row));
    };
  }

  return (
    <>
      <Row
        row={row}
        onClick={onClickFn}
        columns={columns}
        className={className}
        renderRow={renderRow}
      />
      {/* This grid stuff is a cute way to make the height animate smoothly when expanding/collapsing */}
      <div
        className={cn(
          "[grid-column:1/-1] grid overflow-hidden transition-[grid-template-rows] duration-300",
          isExpanded ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
        )}
      >
        <div className="min-h-0 overflow-auto">{content}</div>
      </div>
    </>
  );
}

function RowGroup<T extends object>({
  group,
  columns,
  rowKey,
  renderGroupHeader,
  className,
  onRowClick,
  isRowClickable,
  renderRow,
}: {
  group: Group<T>;
  columns: Column<T>[];
  rowKey: (row: T) => string | number;
  renderGroupHeader?: (group: Group<T>) => ReactNode;
  className?: string;
  onRowClick?: (row: T) => void;
  isRowClickable?: (row: T) => boolean;
  renderRow?: RenderRow<T>;
}) {
  return (
    <div
      className={cn(
        "[grid-column:1/-1] grid [grid-template-columns:subgrid]",
        className,
      )}
    >
      <div className="[grid-column:1/-1]">{renderGroupHeader?.(group)}</div>
      {group.items.map((row) => (
        <Row
          row={row}
          key={rowKey(row)}
          columns={columns}
          onClick={isRowClickable?.(row) === false ? undefined : onRowClick}
          renderRow={renderRow}
        />
      ))}
    </div>
  );
}

type CellProps<T extends object> = {
  row: T;
  column: Column<T>;
  className?: string;
};

function CellContainer({ children, className }: PropsWithChildrenAndClassName) {
  return (
    <td
      className={cn(
        styles.tableCell,
        `flex max-w-full items-center`,
        className,
      )}
    >
      <SubtableIndendation />
      {children}
    </td>
  );
}

function Cell<T extends object>(
  props: CellProps<T> | PropsWithChildrenAndClassName,
) {
  if (propsHasChildren<PropsWithChildrenAndClassName, CellProps<T>>(props)) {
    return (
      <CellContainer className={props.className}>
        {props.children}
      </CellContainer>
    );
  }

  const { row, column, className } = props;
  const content = column.render
    ? column.render(row)
    : isKeyOfT<T>(column.key, row)
      ? String(row[column.key])
      : "";

  return <CellContainer className={className}>{content}</CellContainer>;
}

function NoResultsMessage({
  className,
  children,
}: PropsWithChildrenAndClassName) {
  // A row, not a bare div: this renders inside <tbody>, and anything else is
  // invalid markup that React reports as a hydration error.
  const Wrapper = ({ children, className }: PropsWithChildrenAndClassName) => (
    <tr
      className={cn(
        "[grid-column:1/-1] grid [grid-template-columns:subgrid]",
        className,
      )}
    >
      {children}
    </tr>
  );

  // Padded to the same inset as a data cell: the message sits in the grid
  // where a row would, so flush-left text reads as a broken row.
  const ContentWrapper = ({ children }: PropsWithChildren) => (
    <td className="text-muted-foreground [grid-column:1/-1] px-4 py-6 text-sm">
      {children}
    </td>
  );

  return (
    <Wrapper className={className}>
      <ContentWrapper>{children}</ContentWrapper>
    </Wrapper>
  );
}

function LoadMore<T extends object>({
  columns,
  handleLoadMore,
  isLoading,
}: {
  columns: Column<T>[];
  handleLoadMore: () => void | Promise<void>;
  isLoading?: boolean;
}) {
  const RowWrapper = ({
    children,
    className,
  }: PropsWithChildren<{ className?: string }>) => {
    const colWidths = columns.map((column) => column.width ?? "1fr").join(" ");
    return (
      <tr
        style={{ "--grid-template-columns": colWidths } as React.CSSProperties}
        className={cn(
          "absolute right-0 bottom-0 left-0 -z-0 [grid-column:1/-1] grid min-h-16 max-w-full cursor-pointer [grid-template-columns:var(--grid-template-columns)] items-center border-b opacity-30 transition-colors",
          className,
        )}
      >
        {children}
      </tr>
    );
  };

  const ButtonWrapper = ({ children }: PropsWithChildren) => (
    <div className="absolute right-0 bottom-0 left-0 z-10 flex min-h-14 w-full items-center justify-center py-4">
      {children}
    </div>
  );

  return (
    <>
      <RowWrapper
        className={cn(isLoading && "animate-pulse opacity-100 duration-[2.5s]")}
      >
        {columns.map((column) => (
          <Cell key={column.key.toString()}>
            <div className="h-4 w-full bg-muted" />
          </Cell>
        ))}
      </RowWrapper>
      <ButtonWrapper>
        <button
          className="inline-flex h-9 items-center justify-center gap-2 border border-input bg-background px-4 py-2 text-sm font-medium whitespace-nowrap normal-case transition-colors select-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0"
          onClick={() => {
            void handleLoadMore();
          }}
        >
          {isLoading ? (
            <>
              Loading
              <Loader2 className="animate-spin" />
            </>
          ) : (
            "Load more"
          )}
        </button>
      </ButtonWrapper>
    </>
  );
}

type HeaderCellProps = React.ThHTMLAttributes<HTMLTableCellElement> &
  PropsWithChildren<{
    className?: string;
  }>;

function HeaderCell({ className, children, ...props }: HeaderCellProps) {
  return (
    <th
      {...props}
      className={cn(
        styles.tableHeader,
        "text-eyebrow flex items-center align-middle whitespace-nowrap select-none",
        className,
      )}
    >
      <SubtableIndendation />
      {children}
    </th>
  );
}

// Has the effect of "indenting" subtables while still allowing them to occupy the full width of the parent table
function SubtableIndendation() {
  const { depth } = useTable();
  return depth > 1 ? (
    <div style={{ minWidth: `${16 * (depth - 1)}px` }} />
  ) : null;
}

function propsHasChildren<P extends PropsWithChildren, Q extends object>(
  props: P | Q,
): props is P {
  return "children" in props && props.children !== undefined;
}

function isKeyOfT<T extends object>(key: unknown, data: T): key is keyof T {
  return typeof key === "string" && Object.keys(data).includes(key);
}

// Compound members are attached by mutation rather than `Object.assign` so
// static analysis still sees `Table` as a component export.
Header.Cell = HeaderCell;
TableRoot.Header = Header;
TableRoot.Body = Body;
TableRoot.Row = Row;
TableRoot.Cell = Cell;
TableRoot.RowGroup = RowGroup;
TableRoot.NoResultsMessage = NoResultsMessage;

export { TableRoot as Table };
