import type { ToolUIMetaRow } from "@/elements";
import type { ClaudeToolUsage } from "@gram/client/models/components/claudetoolusage.js";
import type { ClaudeTurnUsage } from "@gram/client/models/components/claudeturnusage.js";
import { formatByteCount, formatUsageCost } from "./claudeUsage";

// The API reports payload size per tool call but cost only per turn (a turn
// covers every tool it called), so the cost row is labelled accordingly.
export function toolMetaRows({
  usage,
  turn,
}: {
  usage: ClaudeToolUsage | undefined;
  turn: ClaudeTurnUsage | undefined;
}): ToolUIMetaRow[] {
  if (!usage) return [];
  const total = usage.inputSizeBytes + usage.resultSizeBytes;
  const rows: ToolUIMetaRow[] = [];
  if (total > 0) {
    rows.push(
      { label: "Arguments size", value: formatByteCount(usage.inputSizeBytes) },
      { label: "Output size", value: formatByteCount(usage.resultSizeBytes) },
      { label: "Total size", value: formatByteCount(total) },
    );
  }
  if (turn) {
    rows.push({ label: "Turn cost", value: formatUsageCost(turn.costUsd) });
  }
  return rows;
}

// Two letters for the avatar fallback: the first two name parts of an email
// local-part (jane.doe → JD), else the first two characters.
export function userInitials(id: string | undefined): string {
  const normalized = id?.trim();
  if (!normalized) return "?";
  const handle = normalized.includes("@")
    ? normalized.slice(0, normalized.indexOf("@"))
    : normalized;
  const parts = handle.split(/[._\-\s]+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0]![0]! + parts[1]![0]!).toUpperCase();
  return handle.slice(0, 2).toUpperCase();
}

export function userDisplayName(id: string | undefined): string {
  return id && id.trim().length > 0 ? id : "User";
}
