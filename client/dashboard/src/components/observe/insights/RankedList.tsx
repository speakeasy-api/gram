import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router";

export type RankedRow = {
  /** Stable identity; also the react key. */
  id: string;
  label: string;
  /**
   * The label as drawn, for a subject a string cannot say — an agent, which
   * carries its own face. `label` stays the plain text, so the row's tooltip
   * and its width still read off one string.
   */
  content?: ReactNode;
  value: number;
  /** Shown to the right of the value, e.g. a failure rate. */
  secondary?: string;
  /** Renders the row as a link into the logs for this slice. */
  href?: string;
  /**
   * Splits the bar into parts that sum to `value`, each with its own fill,
   * for a card whose count has a breakdown worth seeing on the bar itself.
   */
  segments?: RankedRowSegment[];
};

type RankedRowSegment = {
  value: number;
  color: string;
  /** Resting fill strength; the parts tell apart by it, not by hue alone. */
  opacity: number;
};

/** A row's height before one is drawn to measure: `py-1.5` around `text-sm`. */
const FALLBACK_ROW_HEIGHT = 32;

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
  step = 10,
  fill = false,
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
  /** Rows shown before the reader asks for more. */
  maxRows?: number;
  /** How many more each "Show more" reveals. */
  step?: number;
  /**
   * Take the container's height and show as many rows as it holds before
   * "Show more", in place of `maxRows`: for a card the reader sized, such as
   * a dashboard's, where five rows would leave the rest of it blank. The
   * container must give the list a definite height.
   */
  fill?: boolean;
  className?: string;
}): JSX.Element {
  // A card answers its question in five rows, or in as many as its height
  // holds; the long tail is a second question ("who else?") and is revealed
  // a page at a time rather than dumped, so the board keeps its shape until
  // the reader asks otherwise.
  const [revealed, setRevealed] = useState(0);
  const [fitRows, setFitRows] = useState<number | null>(null);
  const [list, setList] = useState<HTMLUListElement | null>(null);
  const baseRows = fill ? (fitRows ?? maxRows) : maxRows;
  const shown = baseRows + revealed;

  // A new filter or range answers a different question, so the expansion the
  // reader asked of the old answer does not carry over — otherwise a card
  // narrowed to three servers still opens scrolled to row twenty of nothing.
  const rowsKey = rows.map((row) => row.id).join("\u0000");
  const lastRowsKey = useRef(rowsKey);
  if (lastRowsKey.current !== rowsKey) {
    lastRowsKey.current = rowsKey;
    if (revealed !== 0) setRevealed(0);
  }

  // The list's height is the space its card leaves it, whatever rows it
  // holds, so measuring it again on every resize settles: the "Show more"
  // row taking or giving back its line cannot flip how many rows fit.
  useEffect(() => {
    if (!fill || !list || typeof ResizeObserver === "undefined") return;
    const measure = () => {
      const rowHeight =
        list.firstElementChild?.getBoundingClientRect().height ||
        FALLBACK_ROW_HEIGHT;
      setFitRows(Math.max(1, Math.floor(list.clientHeight / rowHeight)));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(list);
    return () => observer.disconnect();
  }, [fill, list]);

  // The revealed rows scroll inside the card rather than growing it: these
  // cards sit in a grid row, so one card growing would stretch its neighbours
  // into tall empty boxes. Follow the new rows down so "show more" visibly
  // does something.
  useEffect(() => {
    if (!list || revealed === 0) return;
    list.scrollTo({ top: list.scrollHeight, behavior: "smooth" });
  }, [list, revealed]);

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

  const visible = rows.slice(0, shown);
  const remaining = rows.length - visible.length;
  // Scale against the top row, not the total: the leader fills the track and
  // the rest read as a share of it, which is the comparison being made.
  const max = Math.max(...visible.map((row) => row.value), 1);

  return (
    <div className={cn("flex flex-col", fill && "min-h-0 flex-1", className)}>
      <ul
        ref={setList}
        // Roughly the five rows a card shows before it is asked for more, or
        // the card's own height when it fills one, so expanding one card
        // never changes the height of the row it sits in.
        className={cn(
          "flex flex-col overflow-y-auto",
          fill ? "min-h-0 flex-1" : "max-h-[10.5rem]",
        )}
      >
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
              {/* Segmented rows skip the rank ramp: their fill strength already
                  tells the parts apart, so order reads from bar length alone. */}
              {row.segments ? (
                <span
                  aria-hidden
                  className="absolute inset-y-0 left-0 z-0 flex"
                  style={{ width }}
                >
                  {row.segments.map((segment, segmentIndex) => (
                    <span
                      key={segmentIndex}
                      className="h-full"
                      style={{
                        width: `${row.value > 0 ? (segment.value / row.value) * 100 : 0}%`,
                        backgroundColor: segment.color,
                        opacity: segment.opacity,
                      }}
                    />
                  ))}
                </span>
              ) : (
                <span
                  aria-hidden
                  className="absolute inset-y-0 left-0 z-0"
                  style={{ width, backgroundColor: rowColor, opacity }}
                />
              )}
              <span
                aria-hidden
                className="absolute inset-y-0 left-0 z-0 opacity-0 transition-opacity duration-150 group-hover/row:opacity-[0.18]"
                style={{ width, backgroundColor: rowColor }}
              />
              <span
                className="relative z-10 min-w-0 flex-1 truncate"
                title={row.label}
              >
                {row.content ?? row.label}
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
      {(remaining > 0 || revealed > 0) && (
        // No collapse control: the rows scroll inside a fixed-height card, so
        // an expanded list costs the board nothing and re-collapsing it only
        // takes away what the reader just asked for.
        <div className="text-eyebrow flex items-center gap-3 px-2 pt-2">
          {remaining > 0 && (
            <button
              type="button"
              onClick={() => setRevealed(revealed + step)}
              className="hover:text-foreground transition-colors"
            >
              Show {Math.min(step, remaining)} more
            </button>
          )}
          <span className="text-muted-foreground/70 ml-auto normal-case">
            {visible.length} of {rows.length}
          </span>
        </div>
      )}
    </div>
  );
}
