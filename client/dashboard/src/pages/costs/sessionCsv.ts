import type { SessionSummary } from "@gram/client/models/components/sessionsummary.js";
import { costMeasureLabel } from "@/components/estimated-cost-utils";
import { formatDurationFromNanos } from "../chatLogs/claudeUsage";
import { toCsv } from "./csv";
import type { SessionColumnId } from "./SessionTable";

// CSV export for the cost explorer's session list. Lives outside SessionTable so
// that view file only exports components (the react-refresh lint rule) — the
// same reason taxonomy.ts sits beside EntityProfile.

// The export's columns, in order, mirroring what SessionTable renders. The cost
// header is resolved per call so it follows the table's metered/estimated swap.
const CSV_COLUMNS: { id: SessionColumnId; header: string }[] = [
  { id: "session", header: "Session" },
  { id: "user", header: "User" },
  { id: "agent", header: "Agent" },
  { id: "model", header: "Model" },
  { id: "cost", header: "Est. cost" },
  { id: "tokens", header: "Tokens" },
  { id: "tools", header: "Tool calls" },
  { id: "messages", header: "Messages" },
  { id: "duration", header: "Duration" },
];

/**
 * Serialize the session list to CSV — the same columns the table shows, honoring
 * the same `hiddenColumns` so the file matches what's on screen.
 */
export function buildSessionCsv(
  sessions: SessionSummary[],
  hiddenColumns: SessionColumnId[] = [],
  billingMode?: string,
): string {
  const hidden = new Set(hiddenColumns);
  const columns = CSV_COLUMNS.filter((c) => !hidden.has(c.id));
  const header = columns.map((c) =>
    c.id === "cost" ? costMeasureLabel(billingMode) : c.header,
  );
  const body = sessions.map((s) => columns.map((c) => csvValue(c.id, s)));
  return toCsv(header, body);
}

// One session's value for a column, as raw data rather than the table's JSX —
// numbers unformatted so the file is spreadsheet-ready, and the session titled
// by its name, falling back to the chat id when it has none.
function csvValue(id: SessionColumnId, s: SessionSummary): string | number {
  switch (id) {
    case "session":
      return s.title?.trim() || s.gramChatId;
    case "user":
      return s.userEmail ?? "";
    case "agent":
      return s.hookSource ?? "";
    case "model":
      return s.model ?? "";
    case "cost":
      return s.totalCost.toFixed(2);
    case "tokens":
      return s.totalTokens;
    case "tools":
      return s.toolCallCount;
    case "messages":
      return s.messageCount;
    case "duration":
      return (
        formatDurationFromNanos(s.startTimeUnixNano, s.endTimeUnixNano) ??
        `${Math.round(s.durationSeconds)}s`
      );
  }
}
