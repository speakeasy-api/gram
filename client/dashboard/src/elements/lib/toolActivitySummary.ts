import { humanizeToolName } from "@/elements/lib/humanize";

/** The minimal shape the heuristic needs from a tool call. */
export interface HeuristicToolCall {
  name: string;
}

/**
 * describeToolActivity produces an instant, human-readable label for a turn's
 * tool activity from the tool names alone — no model call. It's the fallback
 * shown immediately (and whenever a richer LLM summary is unavailable), so it
 * favors being fast and always sensible over being clever.
 *
 * Present tense while the tools are running, past tense once they've completed.
 */
export function describeToolActivity(
  toolCalls: HeuristicToolCall[],
  inProgress: boolean,
): string {
  const names = toolCalls.map((call) => call.name).filter(Boolean);

  if (names.length === 0) {
    return inProgress ? "Working…" : "Done";
  }

  if (names.length === 1) {
    const label = humanizeToolName(names[0]!);
    return inProgress ? `Calling ${label}…` : `Used ${label}`;
  }

  const distinct = new Set(names);
  if (distinct.size === 1) {
    const label = humanizeToolName(names[0]!);
    return inProgress
      ? `Calling ${label}…`
      : `Used ${label} ${names.length} times`;
  }

  return inProgress
    ? `Working across ${names.length} tools…`
    : `Used ${names.length} tools`;
}
