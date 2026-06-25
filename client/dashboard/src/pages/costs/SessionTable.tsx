import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import type { SessionSummary } from "@gram/client/models/components";
import { formatDistanceToNow } from "date-fns";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { Fragment, useEffect, useMemo, useState } from "react";
import { formatDurationFromNanos } from "../chatLogs/claudeUsage";
import { HookSourceIcon } from "../hooks/HookSourceIcon";
import { ModelIcon } from "./CostTable";
import {
  Gutter,
  type SortDir,
  SortHeader,
  SUBGRID_ROW_CLASS,
} from "./gridTable";

const PAGE_SIZE = 10;

// The list arrives ranked by the server's sortBy (cost) and capped at this many
// rows; surfaced so the footer can flag when the slice is truncated.
const DEFAULT_LIMIT = 100;

function formatCost(value: number): string {
  return `$${value.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

function displayOrDash(value: string | undefined): string {
  return value && value.length > 0 ? value : "—";
}

// Unix-nanosecond string → epoch millis, 0 on a malformed value. Shared by the
// sort comparator and the relative-time label so neither throws on bad input.
function nanosToMillis(unixNano: string): number {
  try {
    return Number(BigInt(unixNano) / 1_000_000n);
  } catch {
    return 0;
  }
}

// Unix-nanosecond string → "3 hours ago". Blank on a malformed/zero timestamp
// so a bad row degrades gracefully rather than rendering the epoch.
function relativeTime(unixNano: string): string {
  const millis = nanosToMillis(unixNano);
  if (millis === 0) return "";
  return formatDistanceToNow(new Date(millis), { addSuffix: true });
}

function durationLabel(session: SessionSummary): string {
  return (
    formatDurationFromNanos(
      session.startTimeUnixNano,
      session.endTimeUnixNano,
    ) ?? `${Math.round(session.durationSeconds)}s`
  );
}

type SortKey =
  | "session"
  | "cost"
  | "tokens"
  | "tools"
  | "messages"
  | "duration";
type Sort = { key: SortKey; dir: SortDir };

function sortValue(session: SessionSummary, key: SortKey): number {
  switch (key) {
    case "session":
      return nanosToMillis(session.startTimeUnixNano);
    case "cost":
      return session.totalCost;
    case "tokens":
      return session.totalTokens;
    case "tools":
      return session.toolCallCount;
    case "messages":
      return session.messageCount;
    case "duration":
      return session.durationSeconds;
  }
}

const NUM_CELL = "text-left tabular-nums whitespace-nowrap";

// Every column the table can show, in display order. One descriptor drives the
// grid track template, the header, and each row cell — so hiding a column (see
// hiddenColumns) can never desync the three. `track` is the column's grid sizing
// (see gridTable.tsx); `sortKey` marks a column sortable.
export type SessionColumnId =
  | "session"
  | "user"
  | "agent"
  | "model"
  | "cost"
  | "tokens"
  | "tools"
  | "messages"
  | "duration";

type SessionColumn = {
  id: SessionColumnId;
  header: string;
  // Grid track sizing. `minmax(max-content,1fr)` never goes narrower than its
  // content but grows equally to fill the row; the email column is capped at
  // 24rem so long addresses truncate instead of dominating.
  track: string;
  sortKey?: SortKey;
  render: (session: SessionSummary) => JSX.Element;
};

const SESSION_COLUMNS: SessionColumn[] = [
  {
    id: "session",
    header: "Session",
    track: "minmax(max-content,1fr)",
    sortKey: "session",
    render: (s) => (
      <div className="flex min-w-0 flex-col">
        <span className="truncate font-mono text-xs font-medium">
          {s.gramChatId.slice(0, 8)}
        </span>
        <span className="text-muted-foreground text-xs">
          {relativeTime(s.startTimeUnixNano)}
        </span>
      </div>
    ),
  },
  {
    id: "user",
    header: "User",
    track: "minmax(120px,24rem)",
    render: (s) => (
      <div className="flex min-w-0 items-center">
        <span className="truncate">{displayOrDash(s.userEmail)}</span>
      </div>
    ),
  },
  {
    id: "agent",
    header: "Agent",
    track: "minmax(max-content,1fr)",
    render: (s) => (
      <div className="flex min-w-0 items-center gap-2">
        {s.hookSource && (
          <HookSourceIcon source={s.hookSource} className="size-4 shrink-0" />
        )}
        <span className="truncate">{displayOrDash(s.hookSource)}</span>
      </div>
    ),
  },
  {
    id: "model",
    header: "Model",
    track: "minmax(max-content,1fr)",
    render: (s) => (
      <div className="flex min-w-0 items-center gap-2">
        {s.model && <ModelIcon model={s.model} className="size-4 shrink-0" />}
        <span className="truncate">{displayOrDash(s.model)}</span>
      </div>
    ),
  },
  {
    id: "cost",
    header: "Cost",
    track: "minmax(max-content,1fr)",
    sortKey: "cost",
    render: (s) => (
      <span className={cn(NUM_CELL, "font-medium")}>
        {formatCost(s.totalCost)}
      </span>
    ),
  },
  {
    id: "tokens",
    header: "Tokens",
    track: "minmax(max-content,1fr)",
    sortKey: "tokens",
    render: (s) => (
      <span className={NUM_CELL}>{s.totalTokens.toLocaleString()}</span>
    ),
  },
  {
    id: "tools",
    header: "Tool calls",
    track: "minmax(max-content,1fr)",
    sortKey: "tools",
    render: (s) => (
      <span className={NUM_CELL}>{s.toolCallCount.toLocaleString()}</span>
    ),
  },
  {
    id: "messages",
    header: "Messages",
    track: "minmax(max-content,1fr)",
    sortKey: "messages",
    render: (s) => (
      <span className={NUM_CELL}>{s.messageCount.toLocaleString()}</span>
    ),
  },
  {
    id: "duration",
    header: "Duration",
    track: "minmax(max-content,1fr)",
    sortKey: "duration",
    render: (s) => (
      <span className={cn(NUM_CELL, "text-muted-foreground")}>
        {durationLabel(s)}
      </span>
    ),
  },
];

export type SessionTableProps = {
  sessions: SessionSummary[];
  isLoading: boolean;
  isError: boolean;
  // Open one session's detail (the chatLogs ChatDetailSheet, keyed by chat id).
  onOpen: (gramChatId: string) => void;
  // Columns to drop because the current drill context pins that dimension to a
  // single value (e.g. drilled into one agent → "agent" is redundant). The
  // remaining columns auto-size to reclaim the freed width.
  hiddenColumns?: SessionColumnId[];
};

/**
 * Per-session list for one cost slice. Deliberately a separate component from
 * {@link CostTable} (which groups by taxonomy dimension): a session is a leaf,
 * so rows open a detail view instead of drilling deeper, and the columns are
 * session-shaped (model, agent, status, duration). It shares CostTable's
 * subgrid layout primitives ({@link SUBGRID_ROW}, {@link Gutter},
 * {@link SortHeader}) so columns auto-size to fit the container. The dedicated
 * session drilldown page is still to be designed — for now a row opens the
 * existing ChatDetailSheet via {@link onOpen}.
 */
export function SessionTable({
  sessions,
  isLoading,
  isError,
  onOpen,
  hiddenColumns,
}: SessionTableProps): JSX.Element {
  // Server already ranks by cost; mirror that as the default header indicator.
  const [sort, setSort] = useState<Sort>({ key: "cost", dir: "desc" });
  const [page, setPage] = useState(0);

  const onSort = (key: SortKey) => {
    setPage(0);
    setSort((s) =>
      s.key === key
        ? { key, dir: s.dir === "asc" ? "desc" : "asc" }
        : { key, dir: "desc" },
    );
  };

  // Reset to the first page whenever the underlying data changes (drill, range).
  useEffect(() => setPage(0), [sessions]);

  const columns = useMemo(() => {
    const hidden = new Set(hiddenColumns);
    return SESSION_COLUMNS.filter((c) => !hidden.has(c.id));
  }, [hiddenColumns]);

  // gutter | …visible column tracks… | gutter
  const gridTemplate = useMemo(
    () => ["8px", ...columns.map((c) => c.track), "8px"].join(" "),
    [columns],
  );

  const sorted = useMemo(() => {
    const arr = [...sessions];
    arr.sort((a, b) => {
      const cmp = sortValue(a, sort.key) - sortValue(b, sort.key);
      return sort.dir === "asc" ? cmp : -cmp;
    });
    return arr;
  }, [sessions, sort]);

  const totalPages = Math.ceil(sorted.length / PAGE_SIZE);
  const safePage = Math.min(page, Math.max(totalPages - 1, 0));
  const pageItems = sorted.slice(
    safePage * PAGE_SIZE,
    (safePage + 1) * PAGE_SIZE,
  );

  if (isLoading) return <SkeletonTable />;
  if (isError) {
    return (
      <Type className="text-muted-foreground">Failed to load sessions.</Type>
    );
  }

  return (
    <div
      className="border-border divide-border grid gap-x-3 gap-y-0 divide-y overflow-x-auto rounded-lg border"
      style={{ gridTemplateColumns: gridTemplate }}
    >
      <div
        className={cn(
          "text-muted-foreground grid items-center py-3.5 text-sm font-medium",
          SUBGRID_ROW_CLASS,
        )}
      >
        <Gutter />
        {columns.map((c) => (
          <span key={c.id} className="flex">
            {c.sortKey ? (
              <SortHeader
                label={c.header}
                active={sort.key === c.sortKey}
                dir={sort.dir}
                onClick={() => onSort(c.sortKey!)}
              />
            ) : (
              c.header
            )}
          </span>
        ))}
        <Gutter />
      </div>

      {sorted.length === 0 ? (
        <div
          className="px-5 py-10 text-center"
          style={{ gridColumn: "1 / -1" }}
        >
          <Type className="text-muted-foreground">
            No sessions in this slice.
          </Type>
        </div>
      ) : (
        pageItems.map((s, i) => (
          <button
            key={s.gramChatId}
            type="button"
            onClick={() => onOpen(s.gramChatId)}
            className={cn(
              "hover:bg-muted grid w-full cursor-pointer items-center py-4 text-left text-sm transition-colors",
              SUBGRID_ROW_CLASS,
              (safePage * PAGE_SIZE + i) % 2 === 1 && "bg-muted/25",
            )}
          >
            <Gutter />
            {columns.map((c) => (
              <Fragment key={c.id}>{c.render(s)}</Fragment>
            ))}
            <Gutter />
          </button>
        ))
      )}

      {sessions.length >= DEFAULT_LIMIT && (
        <div className="px-5 py-3" style={{ gridColumn: "1 / -1" }}>
          <Type small className="text-muted-foreground">
            Showing the {DEFAULT_LIMIT} most expensive sessions in this slice.
          </Type>
        </div>
      )}

      {totalPages > 1 && (
        <div
          className="flex items-center justify-between px-5 py-3"
          style={{ gridColumn: "1 / -1" }}
        >
          <p className="text-muted-foreground text-sm">
            {safePage * PAGE_SIZE + 1}–
            {Math.min((safePage + 1) * PAGE_SIZE, sorted.length)} of{" "}
            {sorted.length.toLocaleString()}
          </p>
          <div className="flex items-center gap-1">
            <button
              type="button"
              aria-label="Previous page"
              onClick={() => setPage((p) => p - 1)}
              disabled={safePage === 0}
              className="hover:bg-muted inline-flex size-8 items-center justify-center rounded-md transition-colors disabled:pointer-events-none disabled:opacity-40"
            >
              <ChevronLeft className="size-4" />
            </button>
            <button
              type="button"
              aria-label="Next page"
              onClick={() => setPage((p) => p + 1)}
              disabled={safePage >= totalPages - 1}
              className="hover:bg-muted inline-flex size-8 items-center justify-center rounded-md transition-colors disabled:pointer-events-none disabled:opacity-40"
            >
              <ChevronRight className="size-4" />
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
