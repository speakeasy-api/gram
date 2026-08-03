import { humanizeToolName } from "@/elements/lib/humanize";

/** The minimal shape the heuristic needs from a tool call. */
export interface HeuristicToolCall {
  name: string;
}

/**
 * Tools that describe the agent's own scaffolding rather than the work the user
 * asked for. A turn made up only of these produces a label that says nothing
 * ("Calling Compose…"), so it degrades to a neutral phrase instead — and the
 * server-side summarizer is told to fall back to the user's request.
 */
const GENERIC_TOOL_NAMES = new Set(["compose", "think", "plan", "respond"]);

/** True when every call is a scaffolding tool, so the names carry no signal. */
export function isGenericToolActivity(names: string[]): boolean {
  return (
    names.length > 0 &&
    names.every((name) => GENERIC_TOOL_NAMES.has(name.toLowerCase()))
  );
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

  // No names, or only scaffolding tools — naming them would tell the user
  // nothing about their task.
  if (names.length === 0 || isGenericToolActivity(names)) {
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
