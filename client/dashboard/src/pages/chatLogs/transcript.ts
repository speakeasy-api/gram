import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
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

// Strip the injected `<message-context>…</message-context>` envelope (event id,
// timestamp, user id) and trailing whitespace the harness prepends to prompts —
// it's machine plumbing, not part of the conversation.
function cleanMessageText(raw: string): string {
  return raw
    .replace(/^\s*<message-context>[\s\S]*?<\/message-context>/i, "")
    .replace(/[ \t]+$/gm, "")
    .trim();
}

/** Render-time plain text of a message's content (string, multimodal text
 * parts, or JSON fallback). The single source of truth shared by the renderer
 * and the search-occurrence enumeration so counts and marks stay aligned. */
export function messageText(content: unknown): string {
  if (typeof content === "string") return cleanMessageText(content);
  if (Array.isArray(content)) {
    return cleanMessageText(
      content
        .map((part) =>
          part &&
          typeof part === "object" &&
          "text" in part &&
          typeof (part as { text: unknown }).text === "string"
            ? (part as { text: string }).text
            : "",
        )
        .filter(Boolean)
        .join("\n"),
    );
  }
  if (content == null) return "";
  return JSON.stringify(content, null, 2);
}

/** Render-time string of a tool call's arguments. */
export function argsToString(
  args: string | object | undefined,
): string | undefined {
  if (args === undefined) return undefined;
  return typeof args === "string" ? args : JSON.stringify(args, null, 2);
}

/** Case-insensitive, non-overlapping occurrences of `query` in `text`. The one
 * source of truth for both the search highlighter (which marks these ranges) and
 * the occurrence counter/navigator (which counts them) — they MUST agree or the
 * "n/total" position desyncs from the bright mark. */
export function findQueryRanges(
  text: string,
  query: string,
): Array<{ start: number; end: number }> {
  const q = query.trim();
  if (!q) return [];
  const re = new RegExp(q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "gi");
  const ranges: Array<{ start: number; end: number }> = [];
  for (let m = re.exec(text); m !== null; m = re.exec(text)) {
    if (m[0].length === 0) {
      re.lastIndex++; // defensive: never loop on a zero-length match
      continue;
    }
    ranges.push({ start: m.index, end: m.index + m[0].length });
  }
  return ranges;
}

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

/** Whether a row renders a message flagged with `is_risk` by a risk-windowed
 * load (a tool row spans its call + result message). The authorized counterpart
 * to rowIsFlagged: it reads the per-message flag off chat.load, so it needs
 * neither per-message risk results (the org-admin-only risk.results.list) nor
 * any exposed seq. */
export function rowHasRiskFlag(row: TranscriptRow): boolean {
  if (row.kind === "message") return row.message.isRisk === true;
  return row.callMessage?.isRisk === true || row.resultMessage?.isRisk === true;
}

/** A query-highlighted field within a row, in render order. A plain message has
 * one "text" field; a tool spans "name" (header), "args" (Arguments), and
 * "output" (Output). */
export type SearchFieldKey = "text" | "name" | "args" | "output";

export interface RowSearchField {
  key: SearchFieldKey;
  /** Number of query occurrences rendered in this field. */
  count: number;
}

/** The ordered, query-highlighted fields of a row with their occurrence counts —
 * the per-row contribution to the unified occurrence navigator. Mirrors EXACTLY
 * what the renderer highlights (see ChatTranscript): risk-flagged messages and
 * risk-flagged tool sections render the risk highlighter, not the query one, so
 * they contribute nothing here; system rows aren't query-highlighted either. If
 * this diverges from the render, next/prev desyncs from the visible marks. */
export function rowSearchFields(
  row: TranscriptRow,
  query: string,
  riskResultsByMessage: ReadonlyMap<string, readonly unknown[]>,
): RowSearchField[] {
  const q = query.trim();
  if (!q) return [];
  const flagged = (id: string) =>
    (riskResultsByMessage.get(id)?.length ?? 0) > 0;

  if (row.kind === "message") {
    // System rows render a collapsed <details> without query highlighting, and
    // flagged rows render the risk highlighter — neither contributes occurrences.
    if (row.entryType === "system" || flagged(row.message.id)) return [];
    const count = findQueryRanges(messageText(row.message.content), q).length;
    return count > 0 ? [{ key: "text", count }] : [];
  }

  const fields: RowSearchField[] = [];
  const name =
    row.toolCall?.function?.name || row.toolCall?.name || "Tool result";
  const nameCount = findQueryRanges(name, q).length;
  if (nameCount > 0) fields.push({ key: "name", count: nameCount });

  // A section that has risk findings renders the risk highlighter instead of the
  // search one, so it contributes no search occurrences.
  const callFlagged = row.callMessage ? flagged(row.callMessage.id) : false;
  const request = argsToString(row.toolCall?.function?.arguments);
  if (!callFlagged && request) {
    const c = findQueryRanges(request, q).length;
    if (c > 0) fields.push({ key: "args", count: c });
  }
  const resultFlagged = row.resultMessage
    ? flagged(row.resultMessage.id)
    : false;
  const result = row.resultMessage
    ? messageText(row.resultMessage.content)
    : undefined;
  if (!resultFlagged && result) {
    const c = findQueryRanges(result, q).length;
    if (c > 0) fields.push({ key: "output", count: c });
  }
  return fields;
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
      /** Timestamp of the turn's first message, shown in the divider. */
      createdAt?: Date;
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
        createdAt:
          row.kind === "message"
            ? row.message.createdAt
            : (row.callMessage?.createdAt ?? row.resultMessage?.createdAt),
        messageIds,
      });
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
