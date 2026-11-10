import { Children, cloneElement, isValidElement } from "react";
import { cn } from "@/lib/utils";
import "./skeleton.css";
import { Stack } from "../Stack";
import { Table, type Column } from "../Table";

export interface SkeletonProps {
  /**
   * The children to display in the skeleton.
   * The width and content of each child will be used to determine the width of the skeleton.
   *
   * @example
   * <Skeleton>
   *   <div>foo</div>
   *   <div>bar</div>
   * </Skeleton>
   *
   * You can also provide no content and use classNames to style the skeleton items:
   *
   * @example
   * <Skeleton>
   *   <div className="h-5 w-48" />
   *   <div className="h-5 w-96" />
   *   <div className="h-5 w-48" />
   * </Skeleton>
   */
  children?: React.ReactNode;
  /**
   * The class name to apply to each child.
   */
  className?: string;
  /**
   * What is loading. Set it when the skeleton stands in for content, so screen
   * readers announce it as a loading status.
   */
  label?: string;
}

function statusProps(label: string | undefined) {
  return label ? { role: "status", "aria-label": label } : {};
}

export function Skeleton({
  children,
  className,
  label,
}: SkeletonProps): React.JSX.Element {
  // Childless usage is the common case in app code — a single bar sized purely
  // by `className`. Rendering it without the flex column wrapper keeps it
  // usable inline (table cells, button rows) rather than stretching to fill.
  if (children === undefined) {
    return (
      <div
        {...statusProps(label)}
        className={cn(
          "skeleton block h-5 text-transparent select-none",
          className,
        )}
      />
    );
  }

  return (
    <div
      {...statusProps(label)}
      className="flex w-full flex-col items-start gap-2.5 select-none"
    >
      {Children.toArray(children).map((child, index) => {
        if (typeof child === "string") {
          return (
            <div
              key={index}
              className="skeleton h-5 max-w-max min-w-36 text-transparent"
            >
              {child}
            </div>
          );
        }

        if (isValidElement<{ className?: string }>(child))
          return cloneElement(child, {
            className: cn(
              "skeleton h-5 max-w-full text-transparent",
              className,
              child.props.className,
            ),
          });
      })}
    </div>
  );
}

/**
 * A five-row placeholder shaped like a data table. Pass `columns` (one grid
 * width per column) to match the table it stands in for.
 */
export function SkeletonTable({
  className,
  label,
  columns: widths = ["0.25fr", "0.5fr", "1fr"],
}: {
  className?: string;
  columns?: Column<{ a: string }>["width"][];
} & Pick<SkeletonProps, "label"> = {}): React.JSX.Element {
  const columns: Column<{ a: string }>[] = widths.map((width, i) => ({
    header: "",
    key: String(i),
    render: () => (
      <Skeleton>
        <div className="h-6 w-full" />
      </Skeleton>
    ),
    width,
  }));

  return (
    <div {...statusProps(label)}>
      <Table
        columns={columns}
        data={[{ a: "a" }, { a: "b" }, { a: "c" }, { a: "d" }, { a: "e" }]}
        rowKey={(row) => row.a}
        hideHeader
        className={className}
      />
    </div>
  );
}

/** Placeholder for a block of prose; the last line is deliberately short. */
export function SkeletonParagraph({
  lines = 3,
  label,
}: {
  lines?: number;
} & Pick<SkeletonProps, "label">): React.JSX.Element {
  return (
    <Skeleton label={label}>
      {Array.from({ length: lines - 1 }).map((_, i) => (
        <div key={i} className="h-4 w-full" />
      ))}
      <div className="h-4 w-[200px]" />
    </Skeleton>
  );
}

/** Placeholder for a code block: gutter numbers plus varied line lengths. */
export function SkeletonCode({
  lines = 24,
  label,
}: {
  lines?: number;
} & Pick<SkeletonProps, "label">): React.JSX.Element {
  const importLines = Math.floor(lines / 4);
  const codeLines = lines - importLines;

  const LineNumber = () => (
    <Skeleton className="mr-4 h-5 w-6 flex-shrink-0">
      <div />
    </Skeleton>
  );

  const CodeLine = ({ width }: { width: string }) => (
    <div className="flex">
      <LineNumber />
      <Skeleton>
        <div className={cn("h-5", width)} />
      </Skeleton>
    </div>
  );

  const widthForIndex = (i: number) => {
    switch (i % 9) {
      case 0:
      case 8:
        return null; // blank line
      case 2:
      case 7:
        return "w-8"; // a lone bracket
      case 3:
      case 5:
        return "w-3/4";
      default:
        return "w-1/2";
    }
  };

  return (
    <div {...statusProps(label)} className="border p-4">
      <Stack gap={2}>
        {Array.from({ length: importLines }).map((_, i) => (
          <CodeLine key={`import-${i}`} width="w-36" />
        ))}

        <LineNumber key="spacer" />

        {Array.from({ length: codeLines }).map((_, i) => {
          const width = widthForIndex(i);
          return width ? (
            <CodeLine key={`code-${i}`} width={width} />
          ) : (
            <LineNumber key={`empty-${i}`} />
          );
        })}
      </Stack>
    </div>
  );
}
