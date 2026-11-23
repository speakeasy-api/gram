import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import {
  argsToString,
  buildTranscript,
  messageText,
} from "@/pages/chatLogs/transcript";
import {
  callOutcome,
  needsReview,
  secretExposure,
  toolDisplayName,
  type CallOutcome,
} from "./model";

/** One step of the session, in transcript order. */
export type SessionEvent =
  | {
      kind: "user" | "assistant";
      key: string;
      messageId: string;
      seq: number;
      occurredAt: Date;
      text: string;
    }
  | CallEvent
  | {
      kind: "tool_result";
      key: string;
      messageId: string;
      seq: number;
      occurredAt: Date;
      /** Key of the call this answers. */
      callKey: string;
      /** 0-based position of that call among the session's calls. */
      callIndex: number;
      output: string;
      lines: number;
      bytes: number;
    };

export type CallEvent = {
  kind: "tool_call";
  key: string;
  messageId: string;
  seq: number;
  occurredAt: Date;
  /** 0-based position among the session's calls. */
  callIndex: number;
  toolName: string;
  arguments: string;
  /** The analysis of this call; absent before Analyze or past its limit. */
  call?: IntentCallAssessment;
};

/** Which events the viewer steps through: all, calls only, or calls that need a person. */
export type EventFilter = "all" | "calls" | "review";

const callKey = (messageId: string, toolCallId: string) =>
  `${messageId}:${toolCallId}`;

/**
 * The session's events, with each call joined to its analysis when there is
 * one. Analyzed calls the transcript lacks still appear, so a failed transcript
 * load degrades to calls only.
 */
export function buildSessionEvents(
  messages: ChatMessage[],
  calls: IntentCallAssessment[],
): SessionEvent[] {
  const byKey = new Map<string, IntentCallAssessment>();
  const ambiguous = new Set<string>();
  for (const c of calls) {
    const k = callKey(c.messageId, c.toolCallId);
    if (byKey.has(k)) ambiguous.add(k);
    byKey.set(k, c);
  }
  const placed = new Set<string>();
  const events: SessionEvent[] = [];

  for (const row of buildTranscript(messages)) {
    if (row.kind === "message") {
      const { entryType, message } = row;
      if (entryType !== "user" && entryType !== "assistant") continue;
      events.push({
        kind: entryType,
        key: row.id,
        messageId: message.id,
        seq: message.seq,
        occurredAt: message.createdAt,
        text: messageText(message.content),
      });
      continue;
    }
    const { callMessage, toolCall, resultMessage } = row;
    if (!callMessage || !toolCall) continue;
    // The server keys id-less calls by an empty id; when several share a
    // message they can't be matched, so they stay unanalyzed.
    let key = callKey(callMessage.id, toolCall.id ?? "");
    const call = ambiguous.has(key) ? undefined : byKey.get(key);
    if (placed.has(key)) key = `${key}#${events.length}`;
    placed.add(key);
    events.push({
      kind: "tool_call",
      key,
      messageId: callMessage.id,
      seq: callMessage.seq,
      occurredAt: call?.occurredAt ?? callMessage.createdAt,
      callIndex: 0,
      toolName:
        call?.toolName ?? toolCall.function?.name ?? toolCall.name ?? "tool",
      arguments:
        call?.arguments ?? argsToString(toolCall.function?.arguments) ?? "{}",
      call,
    });
    if (resultMessage) {
      const output = messageText(resultMessage.content);
      events.push({
        kind: "tool_result",
        key: `${resultMessage.id}:result`,
        messageId: resultMessage.id,
        seq: resultMessage.seq,
        occurredAt: resultMessage.createdAt,
        callKey: key,
        callIndex: 0,
        output,
        lines: output ? output.split("\n").length : 0,
        bytes: new TextEncoder().encode(output).length,
      });
    }
  }

  for (const [key, call] of byKey) {
    if (placed.has(key)) continue;
    events.push({
      kind: "tool_call",
      key,
      messageId: call.messageId,
      seq: call.seq,
      occurredAt: call.occurredAt,
      callIndex: 0,
      toolName: call.toolName,
      arguments: call.arguments,
      call,
    });
  }

  // Stable, so a message's text stays ahead of the calls it made.
  events.sort((a, b) => a.seq - b.seq);
  const ordinal = new Map<string, number>();
  for (const e of events) {
    if (e.kind === "tool_call") {
      e.callIndex = ordinal.size;
      ordinal.set(e.key, e.callIndex);
    }
  }
  for (const e of events) {
    if (e.kind === "tool_result") e.callIndex = ordinal.get(e.callKey) ?? 0;
  }
  return events;
}

/** The call's decision, or null while it has none. */
export function eventOutcome(event: CallEvent): CallOutcome | null {
  return event.call ? callOutcome(event.call) : null;
}

/** The MCP function when the analysis names one, else the tool name. */
export function eventToolName(event: CallEvent): string {
  return event.call ? toolDisplayName(event.call) : event.toolName;
}

export function matchesFilter(
  event: SessionEvent,
  filter: EventFilter,
): boolean {
  if (filter === "all") return true;
  if (event.kind !== "tool_call") return false;
  return filter === "calls" || (event.call ? needsReview(event.call) : false);
}

/** First event needing review, else the first call, else the first event. */
export function defaultSelection(events: SessionEvent[]): number {
  const review = events.findIndex((e) => matchesFilter(e, "review"));
  if (review !== -1) return review;
  return Math.max(
    0,
    events.findIndex((e) => e.kind === "tool_call"),
  );
}

/** The next (or previous) event after `from` that the filter keeps. */
export function stepEvent(
  events: SessionEvent[],
  from: number,
  dir: 1 | -1,
  filter: EventFilter,
): number | undefined {
  for (let i = from + dir; i >= 0 && i < events.length; i += dir) {
    const event = events[i];
    if (event && matchesFilter(event, filter)) return i;
  }
  return undefined;
}

export type TurnGroup = {
  label: string;
  occurredAt: Date;
  /** Indexes into the event list, in order. */
  indexes: number[];
};

/**
 * Groups events into turns: a turn opens with a user prompt and holds
 * everything the agent does until the next one (its messages, calls, and
 * results). Events before the first prompt form their own leading turn.
 */
export function groupTurns(events: SessionEvent[]): TurnGroup[] {
  const groups: Omit<TurnGroup, "label">[] = [];
  events.forEach((event, i) => {
    const last = groups[groups.length - 1];
    if (!last || event.kind === "user") {
      groups.push({ occurredAt: event.occurredAt, indexes: [i] });
    } else {
      last.indexes.push(i);
    }
  });
  return groups.map((g, n) => {
    const calls = g.indexes.filter(
      (i) => events[i]?.kind === "tool_call",
    ).length;
    const detail =
      calls === 0 ? "no calls" : `${calls} ${calls === 1 ? "call" : "calls"}`;
    return { ...g, label: `Turn ${n + 1} · ${detail}` };
  });
}

/** 1-based turn number for each event. */
export function turnNumbers(
  events: SessionEvent[],
  groups: TurnGroup[],
): number[] {
  const numbers: number[] = Array.from({ length: events.length }, () => 0);
  groups.forEach((g, n) => {
    for (const i of g.indexes) numbers[i] = n + 1;
  });
  return numbers;
}

/** Where a secret enters the agent's context, and the risky calls after it. */
export function secretSinceEvent(
  events: SessionEvent[],
): { since: number; sinceCall: number; risky: Set<number> } | null {
  const analyzed = events.flatMap((e, i) =>
    e.kind === "tool_call" && e.call ? [{ i, call: e.call }] : [],
  );
  const exposure = secretExposure(analyzed.map((a) => a.call));
  const first = exposure ? analyzed[exposure.firstIndex] : undefined;
  const firstEvent = first ? events[first.i] : undefined;
  if (!exposure || !first || firstEvent?.kind !== "tool_call") return null;
  return {
    since: first.i,
    sinceCall: firstEvent.callIndex,
    risky: new Set(
      exposure.riskyIndexes.flatMap((r) => {
        const hit = analyzed[r];
        return hit ? [hit.i] : [];
      }),
    ),
  };
}

export type TranscriptWindow = { from: number; to: number };

/** Inclusive event range shown around the selection. */
export function transcriptWindow(
  total: number,
  selected: number,
  size: number,
  extraBefore: number,
  extraAfter: number,
): TranscriptWindow {
  return {
    from: Math.max(0, selected - size - extraBefore),
    to: Math.min(total - 1, selected + size + extraAfter),
  };
}

/** "6 earlier events · 2 calls" for a hidden range. */
export function describeGap(
  events: SessionEvent[],
  from: number,
  to: number,
  direction: "earlier" | "later",
): string {
  const hidden = events.slice(from, to + 1);
  const calls = hidden.filter((e) => e.kind === "tool_call").length;
  const n = hidden.length;
  const base = `${n} ${direction} ${n === 1 ? "event" : "events"}`;
  return calls > 0
    ? `${base} · ${calls} ${calls === 1 ? "call" : "calls"}`
    : base;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

export function resultMeta(event: { lines: number; bytes: number }): string {
  return `${event.lines} ${event.lines === 1 ? "line" : "lines"} · ${formatBytes(event.bytes)}`;
}

/** The `command` for shell calls, else `key: value` pairs, on one line. */
export function argsSummary(raw: string): string {
  try {
    const args: unknown = JSON.parse(raw);
    if (args && typeof args === "object" && !Array.isArray(args)) {
      const entries = Object.entries(args as Record<string, unknown>);
      const command = (args as { command?: unknown }).command;
      if (typeof command === "string") return command.replace(/\s+/g, " ");
      return entries
        .map(
          ([k, v]) => `${k}: ${typeof v === "string" ? v : JSON.stringify(v)}`,
        )
        .join("  ")
        .replace(/\s+/g, " ");
    }
  } catch {
    // Not JSON: show it raw.
  }
  return raw.replace(/\s+/g, " ");
}
