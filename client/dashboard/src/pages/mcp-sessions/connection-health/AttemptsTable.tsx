import { IdentityAvatar } from "@/components/identity-avatar";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Table, type Column } from "@/components/ui/Table";
import { cn } from "@/lib/utils";
import {
  ArrowDown,
  Ban,
  Check,
  CircleDashed,
  Globe,
  LoaderCircle,
  Undo2,
  X,
  type LucideIcon,
} from "lucide-react";
import {
  cloneElement,
  Fragment,
  useEffect,
  useMemo,
  useRef,
  type ReactElement,
  type ReactNode,
} from "react";
import {
  appLabel,
  dayKey,
  formatClock,
  formatClockMinutes,
  formatCount,
  formatDayHeading,
  outcomeLabel,
  plural,
  personLabel,
  serverLabel,
} from "./format";
import type { AttemptRow, Outcome } from "./types";

const OUTCOME_ICONS: Record<Outcome, LucideIcon> = {
  connected: Check,
  failed: X,
  blocked: Ban,
  declined: Undo2,
  didnt_finish: CircleDashed,
  in_progress: LoaderCircle,
};

/** An outcome as a monochrome icon and its word, with a ×N badge for repeats. */
export function OutcomeLabel({
  outcome,
  count = 1,
  className,
}: {
  outcome: Outcome;
  count?: number;
  className?: string;
}): JSX.Element {
  // The API may add an outcome before the dashboard knows it.
  const Icon = OUTCOME_ICONS[outcome] ?? CircleDashed;
  return (
    <span className={cn("inline-flex items-center gap-1.5", className)}>
      <Icon className="text-muted-foreground size-3.5 shrink-0" aria-hidden />
      <span>{outcomeLabel(outcome)}</span>
      {count > 1 ? (
        <span className="border-destructive text-destructive border px-1 font-mono text-[11px] leading-4">
          <span aria-hidden>×{formatCount(count)}</span>
          <span className="sr-only">{formatCount(count)} times</span>
        </span>
      ) : null}
    </span>
  );
}

/** Who made the attempt and with which app; the network when no one is known. */
function AttemptWho({ row }: { row: AttemptRow }): JSX.Element {
  const name = personLabel(row);
  const detail = row.person
    ? appLabel(row.app)
    : `Network ${row.network?.label ?? "unknown"} · ${appLabel(row.app)}`;
  return (
    <div className="flex min-w-0 items-start gap-2">
      {row.person ? (
        <IdentityAvatar label={name} className="mt-0.5 size-5" />
      ) : (
        <span className="text-muted-foreground mt-0.5 flex size-5 shrink-0 items-center justify-center">
          <Globe className="size-3.5" aria-hidden />
        </span>
      )}
      <div className="min-w-0">
        <div className="truncate" title={row.person?.email ?? name}>
          {name}
        </div>
        <div className="text-muted-foreground truncate text-xs">{detail}</div>
      </div>
    </div>
  );
}

/** The day heading each row that starts a new local day carries. */
function dayHeadings(rows: AttemptRow[], now: Date): Map<string, string> {
  const headings = new Map<string, string>();
  let previous: string | null = null;
  for (const row of rows) {
    const key = dayKey(row.time);
    if (key !== previous) headings.set(row.id, formatDayHeading(row.time, now));
    previous = key;
  }
  return headings;
}

/** Connection attempts, one row per attempt or folded repeat, grouped by day. */
export function AttemptsTable({
  rows,
  now,
  onOpen,
  rowActions,
  hasMore,
  onLoadMore,
  footer,
  className,
  selectedId,
}: {
  rows: AttemptRow[];
  now: Date;
  onOpen?: (row: AttemptRow) => void;
  rowActions?: (row: AttemptRow) => Action[];
  hasMore?: boolean;
  /** Loads the next page when the end of the list scrolls into view. */
  onLoadMore?: () => Promise<void>;
  footer?: ReactNode;
  className?: string;
  /** The row whose sheet is open. */
  selectedId?: string | null;
}): JSX.Element {
  const sentinel = useRef<HTMLDivElement>(null);
  const loading = useRef(false);
  useEffect(() => {
    const node = sentinel.current;
    if (!node || !hasMore || !onLoadMore) return;
    const observer = new IntersectionObserver((entries) => {
      if (!entries.some((e) => e.isIntersecting) || loading.current) return;
      loading.current = true;
      void onLoadMore().finally(() => {
        loading.current = false;
      });
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [hasMore, onLoadMore, rows.length]);
  const headings = useMemo(() => dayHeadings(rows, now), [rows, now]);
  const firstId = rows[0]?.id;
  const columns = useMemo<Column<AttemptRow>[]>(() => {
    const base: Column<AttemptRow>[] = [
      {
        key: "time",
        header: (
          <span className="inline-flex items-center gap-1">
            Time
            <ArrowDown className="size-3.5" aria-label="newest first" />
          </span>
        ),
        width: "130px",
        render: (row) => (
          <div className="font-mono text-xs leading-5 whitespace-nowrap">
            <div>{formatClock(row.time)}</div>
            {row.count > 1 ? (
              <div className="text-destructive">
                started {formatClockMinutes(row.firstStartedAt)}
              </div>
            ) : null}
          </div>
        ),
      },
      {
        key: "who",
        header: "Person · app",
        width: "1.2fr",
        render: (row) => <AttemptWho row={row} />,
      },
      {
        key: "server",
        header: "Server",
        width: "1fr",
        render: (row) => (
          <span className="truncate" title={serverLabel(row)}>
            {serverLabel(row) || "—"}
          </span>
        ),
      },
      {
        key: "outcome",
        header: "Outcome",
        width: "140px",
        render: (row) => (
          <OutcomeLabel outcome={row.outcome} count={row.count} />
        ),
      },
      {
        key: "message",
        header: "Message",
        width: "1.6fr",
        render: (row) => (
          <span
            className="line-clamp-2"
            title={[row.message, row.technicalMessage]
              .filter(Boolean)
              .join("\n")}
          >
            {row.message ?? ""}
          </span>
        ),
      },
    ];
    if (rowActions) {
      base.push({
        key: "actions",
        header: "",
        width: "56px",
        render: (row) => (
          // oxlint-disable-next-line jsx-a11y/no-static-element-interactions, jsx-a11y/click-events-have-key-events
          <span onClick={(event) => event.stopPropagation()}>
            <MoreActions
              actions={rowActions(row)}
              triggerIcon="ellipsis"
              triggerAriaLabel={`Actions for the attempt at ${formatClock(row.time)}`}
            />
          </span>
        ),
      });
    }
    return base;
  }, [rowActions]);

  return (
    <div className={className}>
      <Table
        columns={columns}
        data={rows}
        rowKey={(row) => row.id}
        onRowClick={onOpen}
        cellPadding="condensed"
        renderRow={(row, element) => {
          const rowElement =
            row.id === selectedId
              ? cloneElement(element as ReactElement<Record<string, unknown>>, {
                  className: "bg-accent",
                  "aria-current": true,
                })
              : element;
          const heading = headings.get(row.id);
          if (!heading) return rowElement;
          return (
            <Fragment key={row.id}>
              <tr className="bg-muted/40 [grid-column:1/-1] grid [grid-template-columns:subgrid] border-b">
                <td
                  colSpan={columns.length}
                  className="text-eyebrow [grid-column:1/-1] flex items-center justify-between px-4 py-2"
                >
                  <span>{heading}</span>
                  {row.id === firstId ? <span>Times are local</span> : null}
                </td>
              </tr>
              {rowElement}
            </Fragment>
          );
        }}
      />
      {footer}
      {hasMore && onLoadMore ? (
        <div ref={sentinel} aria-hidden className="h-px" />
      ) : null}
    </div>
  );
}

/** The strip under a list: how many, in what order, and whether there is more. */
export function AttemptsFooter({
  total,
  hasMore,
}: {
  total: number | undefined;
  hasMore: boolean;
}): JSX.Element {
  return (
    <div className="text-eyebrow flex items-center justify-between border border-t-0 px-4 py-3">
      <span>
        {total === undefined ? "" : `${plural(total, "attempt")} · `}
        newest first
      </span>
      <span>{hasMore ? "Scroll to load more" : "All loaded"}</span>
    </div>
  );
}

const SKELETON_ROWS = Array.from({ length: 6 }, (_, i) => ({ id: String(i) }));

function Bar({ className }: { className: string }): JSX.Element {
  return <div className={cn("bg-muted h-3 animate-pulse", className)} />;
}

/** Loading rows in the table's own grid, at a populated row's height. */
export function AttemptsSkeleton({
  label,
  withActions = true,
}: {
  label: string;
  withActions?: boolean;
}): JSX.Element {
  const columns = useMemo<Column<{ id: string }>[]>(() => {
    const base: Column<{ id: string }>[] = [
      {
        key: "time",
        header: "Time",
        width: "130px",
        render: () => <Bar className="my-1 w-16" />,
      },
      {
        key: "who",
        header: "Person · app",
        width: "1.2fr",
        render: (row) => (
          <div className="flex items-start gap-2">
            <div className="bg-muted mt-0.5 size-5 shrink-0 animate-pulse rounded-full" />
            <div className="w-full space-y-1.5 py-1">
              <Bar className={Number(row.id) % 2 ? "w-28" : "w-36"} />
              <Bar className="w-20" />
            </div>
          </div>
        ),
      },
      {
        key: "server",
        header: "Server",
        width: "1fr",
        render: () => <Bar className="my-1 w-24" />,
      },
      {
        key: "outcome",
        header: "Outcome",
        width: "140px",
        render: () => <Bar className="my-1 w-16" />,
      },
      {
        key: "message",
        header: "Message",
        width: "1.6fr",
        render: (row) => (
          <Bar className={cn("my-1", Number(row.id) % 3 ? "w-3/5" : "w-4/5")} />
        ),
      },
    ];
    if (withActions)
      base.push({
        key: "actions",
        header: "",
        width: "56px",
        render: () => null,
      });
    return base;
  }, [withActions]);
  return (
    <div role="status" aria-label={label}>
      <Table
        columns={columns}
        data={SKELETON_ROWS}
        rowKey={(row) => row.id}
        cellPadding="condensed"
        renderRow={(row, rowElement) =>
          row.id === "0" ? (
            <Fragment key={row.id}>
              <tr className="bg-muted/40 [grid-column:1/-1] grid [grid-template-columns:subgrid] border-b">
                <td className="[grid-column:1/-1] px-4 py-2.5">
                  <Bar className="w-32" />
                </td>
              </tr>
              {rowElement}
            </Fragment>
          ) : (
            rowElement
          )
        }
      />
    </div>
  );
}
