import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import { Link } from "react-router";

export type RankedRow = {
  /** Stable identity; also the react key. */
  id: string;
  label: string;
  value: number;
  /** Shown to the right of the value, e.g. a failure rate. */
  secondary?: string;
  /** Renders the row as a link into the logs for this slice. */
  href?: string;
};

/**
 * A ranked breakdown: label, proportional bar, count.
 *
 * Rendered as DOM rather than a canvas chart, which is what lets each row be a
 * real link — the thing a reader wants from a "top N" list is to click the
 * entry and see what is behind it, and a canvas row cannot hold an anchor.
 */
export function RankedList({
  rows,
  color,
  onRowHover,
  loading = false,
  emptyMessage = "No data in this period",
  maxRows = 5,
  className,
}: {
  rows: RankedRow[];
  /**
   * The card's hue, as a CSS color — each card owns one so the board reads as
   * six answers rather than one long grey list. A function instead gives each
   * row its own, for the cards whose subject is also plotted on the chart and
   * should carry the same colour there.
   */
  color: string | ((row: RankedRow) => string);
  /** Hovering a row, for cards that highlight their subject elsewhere. */
  onRowHover?: (row: RankedRow | null) => void;
  loading?: boolean;
  emptyMessage?: string;
  maxRows?: number;
  className?: string;
}): JSX.Element {
  if (loading) {
    return (
      <div className={cn("flex flex-col gap-2", className)}>
        {Array.from({ length: 3 }).map((_, index) => (
          <Skeleton key={index} className="h-6 w-full" />
        ))}
      </div>
    );
  }

  if (rows.length === 0) {
    return (
      <p className={cn("text-muted-foreground py-4 text-sm", className)}>
        {emptyMessage}
      </p>
    );
  }

  const visible = rows.slice(0, maxRows);
  // Scale against the top row, not the total: the leader fills the track and
  // the rest read as a share of it, which is the comparison being made.
  const max = Math.max(...visible.map((row) => row.value), 1);

  return (
    <ul className={cn("flex flex-col", className)}>
      {visible.map((row, index) => {
        // Rank is graded into the fill — strongest at the top, faintest at the
        // bottom — so the order survives even where two rows are close enough
        // in length to look alike. Hover then lands a step stronger than any
        // resting shade, which one flat fill could not do.
        const depth = visible.length > 1 ? index / (visible.length - 1) : 0;
        // A row carrying its own colour is already distinct, so it does not
        // need the rank ramp to tell it apart from its neighbours.
        const perRow = typeof color === "function";
        const rowColor = perRow ? color(row) : color;
        const opacity = perRow ? 0.3 : 0.22 - depth * 0.14;
        const width = `${Math.max((row.value / max) * 100, 2)}%`;

        const content = (
          <>
            <span
              aria-hidden
              className="absolute inset-y-0 left-0 z-0"
              style={{ width, backgroundColor: rowColor, opacity }}
            />
            <span
              aria-hidden
              className="absolute inset-y-0 left-0 z-0 opacity-0 transition-opacity duration-150 group-hover/row:opacity-[0.18]"
              style={{ width, backgroundColor: rowColor }}
            />
            <span
              className="relative z-10 min-w-0 flex-1 truncate"
              title={row.label}
            >
              {row.label}
            </span>
            <span className="relative z-10 shrink-0 font-mono text-xs tabular-nums">
              {row.value.toLocaleString()}
            </span>
            {row.secondary && (
              <span className="text-destructive-default relative z-10 w-12 shrink-0 text-right font-mono text-xs tabular-nums">
                {row.secondary}
              </span>
            )}
          </>
        );

        const rowClass =
          "group/row relative flex items-center gap-3 overflow-hidden px-2 py-1.5 text-sm";

        return (
          <li
            key={row.id}
            onMouseEnter={() => onRowHover?.(row)}
            onMouseLeave={() => onRowHover?.(null)}
            // A linked row is focusable, so the same relationship has to be
            // reachable without a pointer.
            onFocus={() => onRowHover?.(row)}
            onBlur={() => onRowHover?.(null)}
          >
            {row.href ? (
              <Link
                to={row.href}
                className={cn(rowClass, "text-foreground no-underline")}
              >
                {content}
              </Link>
            ) : (
              <div className={rowClass}>{content}</div>
            )}
          </li>
        );
      })}
    </ul>
  );
}
