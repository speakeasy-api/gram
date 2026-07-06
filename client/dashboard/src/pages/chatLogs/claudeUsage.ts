import type {
  ChatMessage,
  ClaudeToolUsage,
  ClaudeTurnUsage,
} from "@gram/client/models/components";

export type ClaudeUsageMatch = {
  turn: ClaudeTurnUsage;
  match: "exact" | "ordered";
};

export function formatUsageCost(cost: number): string {
  if (cost === 0) return "$0.00";
  if (Math.abs(cost) < 0.0001) return `$${cost.toFixed(6)}`;
  return `$${cost.toFixed(4)}`;
}

export function formatTokenCount(tokens: number): string {
  return new Intl.NumberFormat(undefined, {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(tokens);
}

export function formatByteCount(bytes: number): string {
  if (bytes < 1024) return `${bytes.toLocaleString()} BYTES`;

  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes / 1024;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }

  return `${new Intl.NumberFormat(undefined, {
    maximumFractionDigits: value >= 10 ? 0 : 1,
  }).format(value)} ${units[unitIndex]}`;
}

export function formatDurationFromNanos(
  start: string,
  end: string,
): string | null {
  try {
    const diffNanos = BigInt(end) - BigInt(start);
    if (diffNanos < 0n) return null;
    const millis = Number(diffNanos / 1_000_000n);
    if (millis < 1000) return `${millis}ms`;
    if (millis < 10_000) return `${(millis / 1000).toFixed(1)}s`;
    const roundedSeconds = Math.round(millis / 1000);
    if (roundedSeconds < 60) return `${roundedSeconds}s`;
    const minutes = Math.floor(roundedSeconds / 60);
    const remainder = roundedSeconds % 60;
    return `${minutes}m ${remainder}s`;
  } catch {
    return null;
  }
}

export function buildClaudeUsageByMessageId({
  messages,
  turns,
}: {
  messages: ChatMessage[];
  turns: ClaudeTurnUsage[];
}): Map<string, ClaudeUsageMatch> {
  const result = new Map<string, ClaudeUsageMatch>();
  const usedPromptIds = new Set<string>();
  const turnsByPromptId = new Map(turns.map((turn) => [turn.promptId, turn]));

  for (const message of messages) {
    if (message.role !== "user" || !message.promptId) continue;
    const turn = turnsByPromptId.get(message.promptId);
    if (!turn) continue;
    result.set(message.id, { turn, match: "exact" });
    usedPromptIds.add(turn.promptId);
  }

  const unmatchedTurns = turns.filter(
    (turn) => !usedPromptIds.has(turn.promptId),
  );
  let nextTurn = 0;
  for (const message of messages) {
    if (message.role !== "user" || message.promptId || result.has(message.id))
      continue;
    const turn = unmatchedTurns[nextTurn];
    if (!turn) break;
    result.set(message.id, { turn, match: "ordered" });
    nextTurn += 1;
  }

  return result;
}

export function buildClaudeToolUsageByToolUseId(
  tools: ClaudeToolUsage[],
): Map<string, ClaudeToolUsage> {
  return new Map(tools.map((tool) => [tool.toolUseId, tool]));
}
