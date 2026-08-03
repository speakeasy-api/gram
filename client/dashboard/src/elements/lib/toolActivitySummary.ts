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
 * Openers users put in front of the actual ask. Dropping them turns the request
 * into the object of a doing-phrase: "Show me token spend over 30 days" becomes
 * "Investigating token spend over 30 days" rather than "Investigating show me
 * token spend over 30 days".
 */
const REQUEST_LEAD_INS = [
  "can you please",
  "can you",
  "could you please",
  "could you",
  "please",
  "i want to know",
  "i'd like to know",
  "i need",
  "tell me about",
  "tell me",
  "show me",
  "give me",
  "help me",
  "let me see",
  "look at",
  "look into",
  "find out",
  "figure out",
  "scan",
  "search",
  "check",
  "review",
  "analyze",
  "analyse",
  "audit",
  "summarize",
  "summarise",
  "investigate",
  "explain",
  "list",
  "show",
  "pull",
  "fetch",
  "get",
];

/**
 * describeRequest turns the user's prompt into the object of an activity label
 * — "Scan recent agent conversations for leaked secrets" → "recent agent
 * conversations for leaked secrets". Returns "" when nothing usable is left.
 */
export function describeRequest(userMessage: string | undefined): string {
  const firstSentence = (userMessage ?? "")
    .trim()
    .split(/[.?!\n]/)[0]
    ?.trim();
  if (!firstSentence) return "";

  let text = firstSentence;
  const lower = text.toLowerCase();
  for (const lead of REQUEST_LEAD_INS) {
    if (lower.startsWith(`${lead} `)) {
      text = text.slice(lead.length + 1);
      break;
    }
  }

  // The whole sentence, not a word-capped slice: cutting mid-phrase produced
  // labels that trailed off ("…the heaviest end users this…") and read as a
  // rendering bug rather than a summary.
  const phrase = text.trim().replace(/\s+/g, " ");
  if (phrase === "") return "";
  // Lowercase a leading capital that only exists because it started a sentence,
  // but leave acronyms and proper nouns ("MCP", "Slack") alone.
  const head = phrase.split(" ")[0] ?? "";
  const decapitalized =
    head.length > 1 && head === head[0] + head.slice(1).toLowerCase()
      ? phrase[0]!.toLowerCase() + phrase.slice(1)
      : phrase;
  return decapitalized.replace(/[,;:]+$/, "");
}

/**
 * describeToolActivity produces an instant, human-readable label for a turn's
 * tool activity — no model call. It's the fallback shown immediately (and
 * whenever a richer LLM summary is unavailable), so it favors being fast and
 * always sensible over being clever.
 *
 * Present tense while the tools are running, past tense once they've completed.
 */
export function describeToolActivity(
  toolCalls: HeuristicToolCall[],
  inProgress: boolean,
  userMessage?: string,
): string {
  const names = toolCalls.map((call) => call.name).filter(Boolean);

  if (names.length === 0) {
    return inProgress ? "Working…" : "Done";
  }

  // Only scaffolding tools — naming them ("Calling Compose…") says nothing, and
  // a generic "Worked on your request" says less. The request itself is the only
  // real signal available without a model call, so describe that.
  if (isGenericToolActivity(names)) {
    const request = describeRequest(userMessage);
    if (request) {
      return inProgress
        ? `Investigating ${request}…`
        : `Investigated ${request}`;
    }
    return inProgress ? "Working on it…" : "Worked on your request";
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
