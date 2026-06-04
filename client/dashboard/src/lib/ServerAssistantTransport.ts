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
 * server mints one, which we adopt via `ctx.setChatId` so Elements' thread
 * list/history resolve to it. It then polls `chat.load` for the assistant's
 * reply and surfaces it. History, the conversation list, and titles are owned by
 * Elements' RemoteThreadListAdapter — this only does send + reply reflection.
 */
export function createServerAssistantTransport(
  deps: ServerAssistantTransportDeps,
): (ctx: ElementsTransportContext) => ChatTransport<UIMessage> {
  return (ctx) => ({
    async sendMessages({ messages, abortSignal }) {
      let latest: UIMessage | undefined;
      for (let i = messages.length - 1; i >= 0; i--) {
        if (messages[i].role === "user") {
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
          writer.write({ type: "start" });

          let chatId = ctx.getChatId();
          // Bind the local thread identity at send-start so a server-minted
          // chat id is reconciled with THIS thread even if a parallel send on
          // another thread (or a user thread-switch) shifts the runtime's
          // active thread before our setChatId call lands.
          const localThreadIdSnapshot = ctx.captureLocalThreadId();

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
            ctx.setChatId(chatId, localThreadIdSnapshot);
          }

          await pollForReplies({
            deps,
            chatId,
            snapshot,
            writer,
            abortSignal,
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
 * Polls chat.load until the assistant's turn ends, emitting text chunks to the
 * writer as soon as each new assistant row appears. A tool-using turn writes
 * multiple assistant rows interleaved with tool rows; we keep polling until the
 * latest assistant row carries finish_reason "stop" with no pending tool_calls.
 * Empty assistant rows (pure tool-call turns with no narrative) are skipped.
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
  let consecutiveFailures = 0;

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
        if (m.role !== "assistant") continue;
        if (seen.has(m.id)) continue;
        seen.add(m.id);
        lastNewAssistant = m;
        const text = contentText(m.content);
        if (text) {
          writer.write({ type: "text-start", id: m.id });
          writer.write({ type: "text-delta", id: m.id, delta: text });
          writer.write({ type: "text-end", id: m.id });
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
    await sleep(pollIntervalMs, abortSignal);
  }
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
