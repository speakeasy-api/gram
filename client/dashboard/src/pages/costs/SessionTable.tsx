import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import type { SessionSummary } from "@gram/client/models/components";
import {
  type Column,
  type SortDescriptor,
  sortTableData,
  Table,
} from "@speakeasy-api/moonshine";
import { formatDistanceToNow } from "date-fns";
import { useMemo, useState } from "react";
import { HookSourceIcon } from "../hooks/HookSourceIcon";
import { formatDurationFromNanos } from "../chatLogs/claudeUsage";
import { ModelIcon } from "./CostTable";

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

// Unix-nanosecond string → "3 hours ago". Returns "" on a malformed timestamp
// so a bad row degrades to blank rather than throwing.
function relativeTime(unixNano: string): string {
  try {
    const millis = Number(BigInt(unixNano) / 1_000_000n);
    return formatDistanceToNow(new Date(millis), { addSuffix: true });
  } catch {
    return "";
  }
}

function durationLabel(session: SessionSummary): string {
  return (
    formatDurationFromNanos(
      session.startTimeUnixNano,
      session.endTimeUnixNano,
    ) ?? `${Math.round(session.durationSeconds)}s`
  );
}

export type SessionTableProps = {
  sessions: SessionSummary[];
  isLoading: boolean;
  isError: boolean;
  // Open one session's detail (the chatLogs ChatDetailSheet, keyed by chat id).
  onOpen: (gramChatId: string) => void;
};

/**
 * Per-session list for one cost slice. Deliberately a separate component from
 * {@link CostTable} (which groups by taxonomy dimension): a session is a leaf,
 * so rows open a detail view instead of drilling deeper, and the columns are
 * session-shaped (model, agent, status, duration). The dedicated session
 * drilldown page is still to be designed — for now a row opens the existing
 * ChatDetailSheet via {@link onOpen}.
 */
export function SessionTable({
  sessions,
  isLoading,
  isError,
  onOpen,
}: SessionTableProps): JSX.Element {
  const [sort, setSort] = useState<SortDescriptor | null>(null);
  const [page, setPage] = useState(0);

  // Server already ranks by cost; mirror that as the default header indicator.
  const effectiveSort = useMemo<SortDescriptor>(
    () => sort ?? { id: "cost", direction: "desc" },
    [sort],
  );

  const columns = useMemo<Column<SessionSummary>[]>(
    () => [
      {
        key: "gramChatId",
        id: "session",
        header: "Session",
        sortable: true,
        sortValue: (s) => Number(BigInt(s.startTimeUnixNano) / 1_000_000n),
        width: "1.4fr",
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
        key: "userEmail",
        header: "User",
        width: "1.4fr",
        render: (s) => (
          <span className="truncate text-sm">{displayOrDash(s.userEmail)}</span>
        ),
      },
      {
        key: "hookSource",
        header: "Agent",
        width: "1fr",
        render: (s) => (
          <div className="flex min-w-0 items-center gap-2">
            {s.hookSource && (
              <HookSourceIcon
                source={s.hookSource}
                className="size-4 shrink-0"
              />
            )}
            <span className="truncate text-sm">
              {displayOrDash(s.hookSource)}
            </span>
          </div>
        ),
      },
      {
        key: "model",
        header: "Model",
        width: "1fr",
        render: (s) => (
          <div className="flex min-w-0 items-center gap-2">
            {s.model && (
              <ModelIcon model={s.model} className="size-4 shrink-0" />
            )}
            <span className="truncate text-sm">{displayOrDash(s.model)}</span>
          </div>
        ),
      },
      {
        key: "totalCost",
        id: "cost",
        header: "Cost",
        sortable: true,
        sortValue: (s) => s.totalCost,
        width: "0.8fr",
        render: (s) => (
          <span className="font-medium tabular-nums">
            {formatCost(s.totalCost)}
          </span>
        ),
      },
      {
        key: "totalTokens",
        header: "Tokens",
        sortable: true,
        sortValue: (s) => s.totalTokens,
        width: "0.8fr",
        render: (s) => (
          <span className="tabular-nums">{s.totalTokens.toLocaleString()}</span>
        ),
      },
      {
        key: "toolCallCount",
        header: "Tool calls",
        sortable: true,
        sortValue: (s) => s.toolCallCount,
        width: "0.8fr",
        render: (s) => (
          <span className="tabular-nums">
            {s.toolCallCount.toLocaleString()}
          </span>
        ),
      },
      {
        key: "messageCount",
        header: "Messages",
        sortable: true,
        sortValue: (s) => s.messageCount,
        width: "0.8fr",
        render: (s) => (
          <span className="tabular-nums">
            {s.messageCount.toLocaleString()}
          </span>
        ),
      },
      {
        key: "durationSeconds",
        header: "Duration",
        sortable: true,
        sortValue: (s) => s.durationSeconds,
        width: "0.8fr",
        render: (s) => (
          <span className="text-muted-foreground tabular-nums">
            {durationLabel(s)}
          </span>
        ),
      },
    ],
    [],
  );

  const sorted = useMemo(
    () => sortTableData(sessions, columns, effectiveSort) as SessionSummary[],
    [sessions, columns, effectiveSort],
  );

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
    <div className="flex flex-col gap-3">
      <Table
        columns={columns}
        data={pageItems}
        rowKey={(s) => s.gramChatId}
        onRowClick={(s) => onOpen(s.gramChatId)}
        sort={sort}
        onSortChange={(nextSort) => {
          setSort(nextSort);
          setPage(0);
        }}
        noResultsMessage="No sessions in this slice."
      />
      {sessions.length >= DEFAULT_LIMIT && (
        <Type small className="text-muted-foreground">
          Showing the {DEFAULT_LIMIT} most expensive sessions in this slice.
        </Type>
      )}
      {totalPages > 1 && (
        <div className="flex items-center justify-between border-t px-4 py-3">
          <Type small className="text-muted-foreground">
            {safePage * PAGE_SIZE + 1}–
            {Math.min((safePage + 1) * PAGE_SIZE, sorted.length)} of{" "}
            {sorted.length.toLocaleString()}
          </Type>
          <div className="flex items-center gap-1">
            <button
              type="button"
              className="hover:bg-muted rounded p-1 px-2 text-sm disabled:opacity-40"
              onClick={() => setPage((p) => p - 1)}
              disabled={safePage === 0}
            >
              Prev
            </button>
            <button
              type="button"
              className="hover:bg-muted rounded p-1 px-2 text-sm disabled:opacity-40"
              onClick={() => setPage((p) => p + 1)}
              disabled={safePage >= totalPages - 1}
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
