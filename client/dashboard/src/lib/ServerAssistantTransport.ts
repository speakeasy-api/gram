import { assistantsSendMessage } from "@gram/client/funcs/assistantsSendMessage";
import { chatLoad } from "@gram/client/funcs/chatLoad";
import type { GramCore } from "@gram/client/core";
import { sleep, type ElementsTransportContext } from "@gram-ai/elements";
import {
  type ChatTransport,
  createUIMessageStream,
  type UIMessage,
  type UIMessageStreamWriter,
} from "ai";

const DEFAULT_POLL_INTERVAL_MS = 1500;
const DEFAULT_POLL_TIMEOUT_MS = 600_000;
const MAX_CONSECUTIVE_POLL_FAILURES = 3;

// Client-side streaming emulation. The server reply lands as one blob after
// polling (there is no SSE endpoint), so we slice it into many `text-delta`
// events to reproduce the token-by-token feel a real stream would have. Tuned
// to feel responsive without dragging: short replies get a readable typing
// cadence; long replies are paced to finish within the time budget instead of
// crawling. assistant-ui can't tell these deltas apart from a real stream.
const STREAM_BUDGET_MS = 1400; // target wall-clock to stream a whole reply
const STREAM_TICK_MS = 22; // upper bound on delay between chunks
const STREAM_MIN_CHARS = 12; // below this, just emit in one shot
const STREAM_MAX_TICKS = 350; // cap on delta events per reply (huge messages)

// Adaptive polling: a short turn (a one-line answer, no tool calls) often lands
// within a couple of seconds, where a flat 1.5s interval adds up to a full
// poll's worth of dead air. Poll quickly for the first few iterations to catch
// those fast turns, then ramp toward the steady-state interval so long,
// tool-heavy turns don't hammer chat.load. The ceiling is the configured
// `pollIntervalMs`, so callers can still tune the upper bound.
const FAST_POLL_INTERVAL_MS = 350;
const POLL_BACKOFF_FACTOR = 1.6;

function nextPollDelay(attempt: number, ceilingMs: number): number {
  const delay = FAST_POLL_INTERVAL_MS * POLL_BACKOFF_FACTOR ** attempt;
  return Math.min(Math.round(delay), ceilingMs);
}

export interface ServerAssistantTransportDeps {
  /** SDK client (from useGramContext) — already authenticated. */
  client: GramCore;
  /** The project's managed assistant. */
  assistantId: string;
  /** Project slug for the Gram-Project header on sendMessage. */
  projectSlug: string;
  /** Optional poll tuning. */
  pollIntervalMs?: number;
  pollTimeoutMs?: number;
}

interface Snapshot {
  ids: Set<string>;
  /**
   * The chat's `max_generation` at snapshot time. Pinning subsequent polls to
   * this value keeps the loop on the same transcript even if a concurrent
   * compaction or edit opens a new generation mid-turn.
   */
  generation: number;
}

/**
 * Builds an Elements transport factory that routes the conversation through the
 * project's server-side assistant. `sendMessages` posts the user's message via
 * `assistants.sendMessage` — for a new conversation it omits the chat id and the
 * server mints one, which we adopt via the bind closure returned by
 * `ctx.adoptChatId()` so Elements' thread list/history resolve to it. It then
 * polls `chat.load` for the assistant's reply and surfaces it. History, the
 * conversation list, and titles are owned by Elements' RemoteThreadListAdapter
 * — this only does send + reply reflection.
 */
export function createServerAssistantTransport(
  deps: ServerAssistantTransportDeps,
): (ctx: ElementsTransportContext) => ChatTransport<UIMessage> {
  // At most one poll loop alive per dock: each send aborts the previous
  // turn's poller. Without this, a turn that never reaches a terminal row
  // (e.g. a stuck runtime) leaves a zombie chat.load loop running until the
  // poll timeout — nothing aborts the stream when the provider that started
  // it unmounts. The controller lives in factory scope so a send from a
  // remounted provider still reaps the previous instance's poller. Do NOT
  // abort on factory invocation itself: Elements re-invokes the factory
  // whenever its transport memo dependencies change (e.g. MCP tool discovery
  // settling just after a cold open), which would cancel an in-flight send.
  let activePoll: AbortController | null = null;
  return (ctx) => ({
    async sendMessages({ messages, abortSignal }) {
      let latest: UIMessage | undefined;
      for (let i = messages.length - 1; i >= 0; i--) {
        if (messages[i]!.role === "user") {
          latest = messages[i];
          break;
        }
      }
      const text =
        latest?.parts
          .filter((p): p is { type: "text"; text: string } => p.type === "text")
          .map((p) => p.text)
          .join("")
          .trim() ?? "";
      if (!text) {
        throw new Error("No user message to send.");
      }

      // The stream is created up front and `execute` does all the async work —
      // send + poll — writing chunks as new assistant rows are discovered so
      // assistant-ui surfaces per-row updates in real time rather than a single
      // dump after the turn completes.
      return createUIMessageStream<UIMessage>({
        originalMessages: messages,
        execute: async ({ writer }) => {
          // Reap the previous turn's poll loop (see `activePoll` above), then
          // arm a fresh controller for THIS turn. `pollSignal` guards ONLY the
          // poll loop — never the send or snapshot. The reaper fires when the
          // next send starts; if it also guarded `assistantsSendMessage`, a
          // rapid follow-up send (or a remount) would abort the earlier send's
          // in-flight POST and silently drop that user turn. Send + snapshot
          // use the per-turn `abortSignal` alone (turn-cancel only).
          activePoll?.abort();
          const poll = new AbortController();
          activePoll = poll;
          const pollSignal = abortSignal
            ? AbortSignal.any([abortSignal, poll.signal])
            : poll.signal;

          writer.write({ type: "start" });

          let chatId = ctx.getChatId();
          // Bind the local thread identity at send-start so a server-minted
          // chat id is reconciled with THIS thread even if a parallel send on
          // another thread (or a user thread-switch) shifts the runtime's
          // active thread before the bind call lands. `adopt` closes over the
          // captured thread; calling it later attaches the server-minted id to
          // THIS conversation.
          const adopt = ctx.adoptChatId();

          // Snapshot the assistant rows already on the server before sending —
          // the poll's "new" baseline. Elements' optimistic `messages` carry
          // local UI ids that don't match server `chat_messages` ids, so
          // without this we'd re-surface every prior-turn assistant row on
          // each follow-up. Sequenced before the send: if these ran in
          // parallel, the runtime could persist a fresh assistant row before
          // chat.load returned, baking it into the baseline and causing the
          // poll to wait forever for a reply it has already classified as
          // "already seen". New conversations skip the snapshot — there's
          // nothing to baseline against.
          const snapshot = chatId
            ? await snapshotAssistantIds(deps.client, chatId, abortSignal)
            : null;
          const sent = await assistantsSendMessage(
            deps.client,
            {
              gramProject: deps.projectSlug,
              sendMessageRequestBody: {
                assistantId: deps.assistantId,
                message: text,
                chatId: chatId ?? undefined,
                idempotencyKey: latest?.id,
              },
            },
            undefined,
            { fetchOptions: { signal: abortSignal } },
          );
          if (!sent.ok) {
            throw sent.error;
          }
          if (!chatId) {
            // New conversation: adopt the server-minted id so the thread, its
            // history, and the conversation list all resolve to the same chat.
            chatId = sent.value.chatId;
            adopt(chatId);
          }

          await pollForReplies({
            deps,
            chatId,
            snapshot,
            writer,
            abortSignal: pollSignal,
          });

          writer.write({ type: "finish" });
        },
      });
    },

    async reconnectToStream() {
      // The server assistant is poll-based; there is no stream to reconnect to.
      return null;
    },
  });
}

/**
 * Polls chat.load until the assistant's turn ends, emitting text and tool-call
 * chunks to the writer as soon as each new row appears. A tool-using turn
 * writes multiple assistant rows interleaved with tool rows; assistant rows
 * surface their narrative text and tool calls (so Elements renders which tool
 * is running), tool rows resolve those calls with their output, and we keep
 * polling until the latest assistant row carries finish_reason "stop" with no
 * pending tool_calls.
 */
async function pollForReplies(args: {
  deps: ServerAssistantTransportDeps;
  chatId: string;
  snapshot: Snapshot | null;
  writer: UIMessageStreamWriter<UIMessage>;
  abortSignal?: AbortSignal;
}): Promise<void> {
  const { deps, chatId, snapshot, writer, abortSignal } = args;
  const {
    client,
    pollIntervalMs = DEFAULT_POLL_INTERVAL_MS,
    pollTimeoutMs = DEFAULT_POLL_TIMEOUT_MS,
  } = deps;
  const deadline = Date.now() + pollTimeoutMs;
  const seen = new Set<string>(snapshot?.ids ?? []);
  // Pin the poll to the generation captured at snapshot time so a concurrent
  // compaction/edit that opens a new generation can't surface its summary as a
  // reply. For new conversations we don't know the generation yet — leave it
  // undefined for the first iteration, then pin to the response's generation.
  let pinnedGeneration: number | undefined = snapshot?.generation;
  // Tool calls surfaced this turn that still await their tool-row output.
  // Gating outputs on delete-from-this-set does double duty: prior-turn tool
  // rows (whose calls were never emitted to this stream) produce no orphan
  // output chunks, and tool rows re-scanned on later polls — they are not
  // tracked in `seen` — don't emit twice. Transcript order guarantees an
  // assistant row precedes its tool rows, so the input chunk always lands
  // before the matching output.
  const pendingToolCalls = new Set<string>();
  let consecutiveFailures = 0;
  let attempt = 0;
  // Tracks whether a `start-step` is open and awaiting its `finish-step`. Each
  // server completion (assistant row) is bracketed as its own step so the
  // turn's final, text-only row lands in a step with no tool calls — see the
  // step writes below.
  let stepOpen = false;

  // The `finally` closes whatever step is still open however the loop exits —
  // terminal return, timeout, poll failure, or abort — so the stream never ends
  // mid-step.
  try {
    for (;;) {
      if (abortSignal?.aborted) {
        throw new DOMException("Aborted", "AbortError");
      }

      const res = await chatLoad(
        client,
        { id: chatId, generation: pinnedGeneration },
        undefined,
        { fetchOptions: { signal: abortSignal } },
      );
      if (res.ok) {
        consecutiveFailures = 0;
        if (pinnedGeneration === undefined) {
          pinnedGeneration = res.value.generation;
        }
        // Only the *new* (post-baseline) assistant rows belong to this turn —
        // prior-turn terminal rows would otherwise satisfy the terminal check on
        // the first iteration and short-circuit the loop with empty replies.
        let lastNewAssistant: {
          finishReason?: string;
          toolCalls?: string;
        } | null = null;
        for (const m of res.value.messages) {
          if (m.role === "tool") {
            if (m.toolCallId && pendingToolCalls.delete(m.toolCallId)) {
              writer.write({
                type: "tool-output-available",
                toolCallId: m.toolCallId,
                output: toolOutputValue(m.content),
              });
            }
            continue;
          }
          if (m.role !== "assistant") continue;
          if (seen.has(m.id)) continue;
          seen.add(m.id);
          lastNewAssistant = m;
          // Open a fresh step for each completion (closing the prior one).
          // Without these boundaries the whole turn collapses into one step, and
          // assistant-ui's resume check
          // (`lastAssistantMessageIsCompleteWithToolCalls`, which inspects only
          // the last step's tool parts) sees the turn's resolved tool calls and
          // auto-resends a turn the server already finished. Per-step framing
          // keeps the final text-only row in a step of its own, so that check
          // finds no pending tool calls and stays put.
          if (stepOpen) {
            writer.write({ type: "finish-step" });
          }
          writer.write({ type: "start-step" });
          stepOpen = true;
          const text = contentText(m.content);
          if (text) {
            await writeStreamedText({
              writer,
              id: m.id,
              text,
              abortSignal,
            });
          }
          for (const call of parseToolCalls(m.toolCalls)) {
            writer.write({
              type: "tool-input-available",
              toolCallId: call.id,
              toolName: call.name,
              input: call.input,
            });
            pendingToolCalls.add(call.id);
          }
        }
        if (lastNewAssistant && isTurnTerminal(lastNewAssistant)) {
          return;
        }
      } else {
        consecutiveFailures++;
        if (consecutiveFailures >= MAX_CONSECUTIVE_POLL_FAILURES) {
          throw res.error;
        }
      }

      if (Date.now() >= deadline) {
        throw new Error("Timed out waiting for the assistant's reply.");
      }
      await sleep(nextPollDelay(attempt, pollIntervalMs), abortSignal);
      attempt++;
    }
  } finally {
    if (stepOpen) {
      writer.write({ type: "finish-step" });
    }
  }
}

// Emits a finished assistant reply as a sequence of `text-delta` events so it
// types onto the screen like a real stream instead of appearing all at once.
// Words are kept intact (we split on whitespace) and the pace adapts to length:
// the whole reply streams within `STREAM_BUDGET_MS`, capped at `STREAM_MAX_TICKS`
// delta events so very long replies don't flood the writer. Honors
// `prefers-reduced-motion` and skips emulation for trivially short replies.
// On abort, `sleep` rejects and the partially-streamed text stays rendered,
// matching how a real aborted stream behaves.
async function writeStreamedText(args: {
  writer: UIMessageStreamWriter<UIMessage>;
  id: string;
  text: string;
  abortSignal?: AbortSignal;
}): Promise<void> {
  const { writer, id, text, abortSignal } = args;

  writer.write({ type: "text-start", id });

  if (text.length <= STREAM_MIN_CHARS || prefersReducedMotion()) {
    writer.write({ type: "text-delta", id, delta: text });
    writer.write({ type: "text-end", id });
    return;
  }

  // Whitespace-preserving tokens keep words (and the spaces between them)
  // intact, so chunks never split mid-word.
  const tokens = text.match(/\s+|\S+/g) ?? [text];
  const ticks = Math.min(tokens.length, STREAM_MAX_TICKS);
  const groupSize = Math.ceil(tokens.length / ticks);
  const delayMs = Math.min(STREAM_TICK_MS, STREAM_BUDGET_MS / ticks);

  for (let i = 0; i < tokens.length; i += groupSize) {
    const delta = tokens.slice(i, i + groupSize).join("");
    writer.write({ type: "text-delta", id, delta });
    // Skip the trailing sleep so the final chunk lands without dead air.
    if (i + groupSize < tokens.length) {
      await sleep(delayMs, abortSignal);
    }
  }

  writer.write({ type: "text-end", id });
}

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

// Returns the assistant message ids and current generation for the chat. Used
// as the "already seen" baseline and the generation pin so the poll only
// surfaces rows produced by the turn we're about to send, on the same
// generation. Throws on failure: falling back to an empty baseline would let
// the poll's first iteration treat every prior assistant row as new and
// short-circuit on the previous turn's terminal row, returning the entire chat
// history as the reply.
async function snapshotAssistantIds(
  client: GramCore,
  chatId: string,
  abortSignal?: AbortSignal,
): Promise<Snapshot> {
  const res = await chatLoad(client, { id: chatId }, undefined, {
    fetchOptions: { signal: abortSignal },
  });
  if (!res.ok) {
    throw res.error;
  }
  const ids = new Set<string>();
  for (const m of res.value.messages) {
    if (m.role === "assistant") {
      ids.add(m.id);
    }
  }
  return { ids, generation: res.value.maxGeneration };
}

// A turn ends when the model returns finish_reason "stop" with no pending
// tool_calls. "tool_calls" (or any other reason) means more assistant rows are
// still on their way.
function isTurnTerminal(m: {
  finishReason?: string;
  toolCalls?: string;
}): boolean {
  if (m.finishReason !== "stop") return false;
  if (!m.toolCalls) return true;
  const trimmed = m.toolCalls.trim();
  return trimmed === "" || trimmed === "[]" || trimmed === "null";
}

interface ParsedToolCall {
  id: string;
  name: string;
  input: unknown;
}

// Decodes an assistant row's `tool_calls` JSON blob (OpenAI wire shape:
// `[{id, function: {name, arguments}}]`, with `arguments` itself a JSON
// string). Malformed blobs or entries are dropped rather than failing the
// poll — a reply we can't decorate with tool state is still a reply.
function parseToolCalls(raw: string | undefined): ParsedToolCall[] {
  if (!raw) return [];
  let decoded: unknown;
  try {
    decoded = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(decoded)) return [];
  const calls: ParsedToolCall[] = [];
  for (const entry of decoded) {
    if (!entry || typeof entry !== "object") continue;
    const { id, function: fn } = entry as {
      id?: unknown;
      function?: { name?: unknown; arguments?: unknown };
    };
    if (typeof id !== "string" || id === "") continue;
    if (typeof fn?.name !== "string" || fn.name === "") continue;
    calls.push({ id, name: fn.name, input: parseToolArguments(fn.arguments) });
  }
  return calls;
}

function parseToolArguments(args: unknown): unknown {
  if (typeof args !== "string" || args.trim() === "") return {};
  try {
    return JSON.parse(args);
  } catch {
    return args;
  }
}

// Tool rows store their output as a string — plain text for text outputs,
// JSON for structured/multimodal ones. Decode JSON-looking strings so the
// tool UI renders structured content instead of a serialized blob.
function toolOutputValue(content: unknown): unknown {
  if (typeof content === "string") {
    const trimmed = content.trim();
    if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
      try {
        return JSON.parse(trimmed);
      } catch {
        // plain text that merely looks like JSON
      }
    }
  }
  return content;
}

function contentText(content: unknown): string {
  if (typeof content === "string") {
    return content;
  }
  if (Array.isArray(content)) {
    return content
      .map((p) =>
        p && typeof p === "object" && (p as { type?: string }).type === "text"
          ? ((p as { text?: string }).text ?? "")
          : "",
      )
      .join("");
  }
  return "";
}
