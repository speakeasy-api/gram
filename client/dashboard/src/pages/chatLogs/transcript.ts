import type { ChatMessage } from "@gram/client/models/components";
import {
  getTraceEntryType,
  parseToolCalls,
  type ToolCall,
  type TraceEntryType,
} from "./traceEntries";

type MessageEntryType = Extract<
  TraceEntryType,
  "user" | "assistant" | "system"
>;

/** A plain user / assistant / system turn rendered as a chat bubble. */
export interface MessageRow {
  kind: "message";
  id: string;
  entryType: MessageEntryType;
  message: ChatMessage;
  generation: number;
}

/** A tool invocation, pairing the assistant's tool_call with its tool result. */
export interface ToolRow {
  kind: "tool";
  id: string;
  toolCall?: ToolCall;
  callMessage?: ChatMessage;
  resultMessage?: ChatMessage;
  generation: number;
}

export type TranscriptRow = MessageRow | ToolRow;

function hasTextContent(content: unknown): boolean {
  if (typeof content === "string") return content.trim().length > 0;
  if (Array.isArray(content)) {
    return content.some(
      (part) =>
        !!part &&
        typeof part === "object" &&
        "text" in part &&
        typeof (part as { text: unknown }).text === "string" &&
        (part as { text: string }).text.trim().length > 0,
    );
  }
  return false;
}

/** A message with nothing left once the leading <message-context> envelope is
 * stripped — machine plumbing, so its row is hidden instead of rendered as an
 * empty bubble. Only applies to plain string content (arrays go through
 * hasTextContent). */
function hasNoVisibleText(content: unknown): boolean {
  if (typeof content !== "string") return false;
  return (
    content
      .replace(/^\s*<message-context>[\s\S]*?<\/message-context>/i, "")
      .trim().length === 0
  );
}

/**
 * Flatten the raw chat messages into chat rows. The key transform is pairing
 * each assistant tool_call with the separate tool-role message that carries its
 * result, so the two render as a single elements `<ToolUI>` (request + output)
 * instead of two disconnected entries.
 */
export function buildTranscript(messages: ChatMessage[]): TranscriptRow[] {
  const resultByToolCallId = new Map<string, ChatMessage>();
  for (const m of messages) {
    if (m.role === "tool" && m.toolCallId) {
      resultByToolCallId.set(m.toolCallId, m);
    }
  }

  const consumed = new Set<string>();
  const rows: TranscriptRow[] = [];

  for (const m of messages) {
    const toolCalls = parseToolCalls(m.toolCalls);
    const entryType = getTraceEntryType(m, toolCalls);

    if (entryType === "tool_result") {
      // Already shown inside its paired tool row; only surface orphans.
      if (consumed.has(m.id)) continue;
      rows.push({
        kind: "tool",
        id: m.id,
        resultMessage: m,
        generation: m.generation,
      });
      continue;
    }

    if (entryType === "tool_call" && toolCalls) {
      if (hasTextContent(m.content) && !hasNoVisibleText(m.content)) {
        rows.push({
          kind: "message",
          id: `${m.id}:text`,
          entryType: "assistant",
          message: m,
          generation: m.generation,
        });
      }
      toolCalls.forEach((toolCall, idx) => {
        const result = toolCall.id
          ? resultByToolCallId.get(toolCall.id)
          : undefined;
        if (result) consumed.add(result.id);
        rows.push({
          kind: "tool",
          id: `${m.id}:${toolCall.id ?? idx}`,
          toolCall,
          callMessage: m,
          resultMessage: result,
          generation: m.generation,
        });
      });
      continue;
    }

    // Hide rows that are only the injected <message-context> plumbing.
    if (hasNoVisibleText(m.content)) continue;

    rows.push({
      kind: "message",
      id: m.id,
      entryType: entryType as MessageEntryType,
      message: m,
      generation: m.generation,
    });
  }

  return rows;
}

/** Chat-message ids backing a row — used for risk lookups (one row can span an
 * assistant tool_call message and its tool-result message). */
function rowMessageIds(row: TranscriptRow): string[] {
  if (row.kind === "message") return [row.message.id];
  const ids: string[] = [];
  if (row.callMessage) ids.push(row.callMessage.id);
  if (row.resultMessage) ids.push(row.resultMessage.id);
  return ids;
}

export function rowIsFlagged(
  row: TranscriptRow,
  riskResultsByMessage: ReadonlyMap<string, readonly unknown[]>,
): boolean {
  return rowMessageIds(row).some(
    (id) => (riskResultsByMessage.get(id)?.length ?? 0) > 0,
  );
}

/** Whether a row renders the message with this `seq` (a tool row spans its
 * assistant call and the tool-result message). Used to locate a search match's
 * row so the transcript can scroll to it. */
export function rowMatchesSeq(row: TranscriptRow, seq: number): boolean {
  if (row.kind === "message") return row.message.seq === seq;
  return row.callMessage?.seq === seq || row.resultMessage?.seq === seq;
}

/** Coarse message-type bucket for the header transcript filter. System turns
 * fold into "assistant" (model-side output) so the filter stays a clean
 * user / assistant / tool triad rather than exposing a rare fourth chip. */
export type MessageCategory = "user" | "assistant" | "tool";

export function rowCategory(row: TranscriptRow): MessageCategory {
  if (row.kind === "tool") return "tool";
  return row.entryType === "user" ? "user" : "assistant";
}

/** Who owns a conversation turn. Tool rows belong to the assistant's turn, so
 * an assistant turn groups its text + any tool calls under one header. */
export type TurnAuthor = "user" | "assistant";

function rowTurnAuthor(row: TranscriptRow): TurnAuthor {
  return rowCategory(row) === "user" ? "user" : "assistant";
}

export type DisplayItem =
  | { type: "divider"; id: string; generation: number }
  /** Avatar + name above each turn, with a divider rule separating turns. */
  | {
      type: "turnHeader";
      id: string;
      author: TurnAuthor;
      userId?: string;
      /** Every message in the turn (the assistant turn spans text + tool rows),
       * so the header can surface the turn's risk badge + exclusion actions. */
      messageIds: string[];
      first: boolean;
    }
  | { type: "row"; id: string; row: TranscriptRow }
  /** Keyset-pagination affordance at the top/bottom of the loaded window. */
  | { type: "loadMore"; id: string; dir: "older" | "newer" }
  /** Un-loaded span between two disjoint risk segments (risk-focused view). */
  | { type: "serverGap"; id: string; afterSeq: number };

/** The row's display-order anchor seq. Gap anchors are message seqs; for a tool
 * row use the assistant call's seq (its position in the list), not the
 * tool-result's — the result can sit much later in seq order and would break the
 * monotonic progression the gap placement relies on. */
function rowAnchorSeq(row: TranscriptRow): number {
  if (row.kind === "message") return row.message.seq;
  return row.callMessage?.seq ?? row.resultMessage?.seq ?? -1;
}

/**
 * Produce the ordered render list from the currently-loaded rows: keyset
 * "load older/newer" affordances at the edges, generation dividers, and — in the
 * risk-focused view — "load in-between" markers at the un-loaded gaps the server
 * left between disjoint risk segments. Risk windowing is server-side now, so
 * this no longer collapses context client-side.
 */
export function buildDisplayItems({
  rows,
  hasMoreBefore = false,
  hasMoreAfter = false,
  gaps,
}: {
  rows: TranscriptRow[];
  hasMoreBefore?: boolean;
  hasMoreAfter?: boolean;
  gaps?: ReadonlySet<number>;
}): DisplayItem[] {
  const items: DisplayItem[] = [];
  if (hasMoreBefore)
    items.push({ type: "loadMore", id: "load-older", dir: "older" });

  const multiGen =
    rows.length > 0 &&
    rows[rows.length - 1]!.generation !== rows[0]!.generation;
  const gapAnchors = gaps ? [...gaps].sort((a, b) => a - b) : [];
  let gi = 0;
  let lastGeneration: number | null = null;
  let lastAuthor: TurnAuthor | null = null;
  let firstTurn = true;

  for (let i = 0; i < rows.length; i++) {
    const row = rows[i]!;
    if (multiGen && row.generation !== lastGeneration) {
      items.push({
        type: "divider",
        id: `divider-${row.generation}-${i}`,
        generation: row.generation,
      });
      lastGeneration = row.generation;
    }

    // Open a new turn whenever authorship flips (user ↔ assistant), grouping an
    // assistant's text + tool calls under one header.
    const author = rowTurnAuthor(row);
    if (author !== lastAuthor) {
      // Collect every message in this turn (look ahead until authorship flips)
      // so the header can aggregate the turn's findings + exclusion actions.
      const messageIds: string[] = [];
      for (
        let j = i;
        j < rows.length && rowTurnAuthor(rows[j]!) === author;
        j++
      ) {
        messageIds.push(...rowMessageIds(rows[j]!));
      }
      items.push({
        type: "turnHeader",
        id: `turn-${row.id}`,
        author,
        userId:
          row.kind === "message"
            ? (row.message.externalUserId ?? row.message.userId)
            : undefined,
        messageIds,
        // No divider rule above the very first turn (unless older pages sit
        // above it, in which case the load-older affordance already separates).
        first: firstTurn && !hasMoreBefore,
      });
      firstTurn = false;
      lastAuthor = author;
    }
    items.push({ type: "row", id: row.id, row });

    // Place each pending gap marker after the last row whose seq is <= the
    // anchor (survives even if the exact boundary row is a paired tool row).
    const seq = rowAnchorSeq(row);
    const nextSeq = i + 1 < rows.length ? rowAnchorSeq(rows[i + 1]!) : Infinity;
    while (gi < gapAnchors.length && gapAnchors[gi]! < nextSeq) {
      if (gapAnchors[gi]! >= seq) {
        items.push({
          type: "serverGap",
          id: `gap-${gapAnchors[gi]}`,
          afterSeq: gapAnchors[gi]!,
        });
      }
      gi++;
    }
  }

  if (hasMoreAfter)
    items.push({ type: "loadMore", id: "load-newer", dir: "newer" });
  return items;
}
