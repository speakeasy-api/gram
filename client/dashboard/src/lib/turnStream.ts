import { getServerURL } from "@/lib/utils";
import type { UIMessage, UIMessageStreamWriter } from "ai";

/**
 * Consumes a chat's turn frames and renders them.
 *
 * This replaces polling `chat.load` for the duration of a turn. The server
 * publishes a frame for everything the dashboard needs — text as it is
 * generated, each persisted assistant row with its tool calls, each tool
 * result, and a terminal frame — so nothing has to be discovered by asking
 * again.
 *
 * Losslessness comes from the cursor. Every frame carries one, and a
 * reconnect resumes with the last cursor applied, so the server replays what
 * was missed before sending anything new. Without that, dropping the poll
 * would mean a dropped connection loses a turn.
 */

const RECONNECT_DELAY_MS = 400;
const MAX_RECONNECTS = 5;

interface TurnFrame {
  kind: "text" | "message" | "tool_output" | "done";
  cursor?: string;
  text?: string;
  message_id?: string;
  tool_calls?: unknown;
  finish_reason?: string;
  tool_call_id?: string;
  output?: unknown;
}

interface ParsedToolCall {
  id: string;
  name: string;
  input: unknown;
}

function parseFrameToolCalls(raw: unknown): ParsedToolCall[] {
  if (!Array.isArray(raw)) return [];
  const calls: ParsedToolCall[] = [];
  for (const entry of raw) {
    if (!entry || typeof entry !== "object") continue;
    const call = entry as {
      id?: string;
      function?: { name?: string; arguments?: string };
    };
    if (!call.id || !call.function?.name) continue;
    let input: unknown = {};
    try {
      input = call.function.arguments
        ? JSON.parse(call.function.arguments)
        : {};
    } catch {
      // A tool call whose arguments are still malformed is worth surfacing
      // with an empty input rather than dropping: the user sees the call.
      input = {};
    }
    calls.push({ id: call.id, name: call.function.name, input });
  }
  return calls;
}

/**
 * Streams one turn to the writer, returning when the turn is terminal.
 *
 * Reconnects transparently on a dropped connection, resuming from the last
 * cursor. Throws only when it cannot deliver the turn at all, which the caller
 * turns into a resync.
 */
export async function streamTurn(args: {
  chatId: string;
  writer: UIMessageStreamWriter<UIMessage>;
  abortSignal?: AbortSignal;
  /**
   * The caller's `Gram-Session` token. These routes authenticate from request
   * headers, not cookies, so without it every subscription 401s.
   */
  sessionToken?: string;
  /**
   * Project slug. The routes resolve the caller's project from `Gram-Project`
   * and reject with "empty project slug" before the session is considered.
   */
  projectSlug: string;
}): Promise<void> {
  const { chatId, writer, abortSignal, sessionToken, projectSlug } = args;

  let cursor = "";
  let reconnects = 0;
  // Each persisted assistant row opens its own step, so the turn's final,
  // text-only row lands in a step with no tool calls — assistant-ui's resume
  // check inspects the last step, and a step carrying resolved tool calls
  // makes it re-send a turn the server already finished.
  let stepOpen = false;
  // Text arrives before the row that will contain it, so the part id is per
  // message index rather than per row id.
  let messageIndex = 0;
  let sawTextThisMessage = false;
  // Whether the open step already carries a row, so the next row knows to
  // close it rather than sharing.
  let stepHasMessage = false;
  // Tool calls announced but not yet answered. assistant-ui re-sends a turn
  // whose last step holds an unresolved call, so any still outstanding when
  // the turn ends are closed out rather than left to trigger that.
  const pendingToolCalls = new Set<string>();

  const closeStep = () => {
    if (stepOpen) {
      writer.write({ type: "finish-step" });
      stepOpen = false;
      stepHasMessage = false;
    }
  };
  const openStep = () => {
    if (!stepOpen) {
      writer.write({ type: "start-step" });
      stepOpen = true;
    }
  };

  try {
    for (;;) {
      if (abortSignal?.aborted) {
        throw new DOMException("Aborted", "AbortError");
      }

      const url = new URL(`${getServerURL()}/chat/turnstream`);
      url.searchParams.set("chat_id", chatId);
      if (cursor) url.searchParams.set("after", cursor);

      let response: Response;
      try {
        response = await fetch(url, {
          credentials: "include",
          signal: abortSignal,
          headers: {
            accept: "text/event-stream",
            "Gram-Project": projectSlug,
            ...(sessionToken ? { "Gram-Session": sessionToken } : {}),
          },
        });
      } catch (err) {
        if (abortSignal?.aborted) throw err;
        if (++reconnects > MAX_RECONNECTS) throw err;
        await new Promise((r) => setTimeout(r, RECONNECT_DELAY_MS));
        continue;
      }
      if (!response.ok || !response.body) {
        if (++reconnects > MAX_RECONNECTS) {
          throw new Error(`turn stream failed: ${response.status}`);
        }
        await new Promise((r) => setTimeout(r, RECONNECT_DELAY_MS));
        continue;
      }
      reconnects = 0;

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let done = false;

      // The turn can end while the response is still open (the server closes
      // after the terminal frame, but the client returns as soon as it reads
      // one). Cancelling releases the connection rather than leaving an
      // in-flight request behind for the rest of the session.
      const release = () => {
        void reader.cancel().catch(() => {});
      };

      while (!done) {
        const { done: finished, value } = await reader.read();
        if (finished) break;
        buffer += decoder.decode(value, { stream: true });

        // Frames are separated by a blank line; a trailing partial frame stays
        // buffered until the rest of it arrives.
        const chunks = buffer.split("\n\n");
        buffer = chunks.pop() ?? "";

        for (const chunk of chunks) {
          const line = chunk.split("\n").find((l) => l.startsWith("data: "));
          if (!line) continue;
          let frame: TurnFrame;
          try {
            frame = JSON.parse(line.slice("data: ".length)) as TurnFrame;
          } catch {
            continue;
          }
          if (frame.cursor) cursor = frame.cursor;

          switch (frame.kind) {
            case "text": {
              if (!frame.text) break;
              openStep();
              // assistant-ui addresses text parts by id and rejects a delta
              // for a part it has not been told about, so each part is opened
              // before its first delta and closed when the row lands.
              if (!sawTextThisMessage) {
                writer.write({
                  type: "text-start",
                  id: `turn-${chatId}-${messageIndex}`,
                });
                sawTextThisMessage = true;
              }
              writer.write({
                type: "text-delta",
                id: `turn-${chatId}-${messageIndex}`,
                delta: frame.text,
              });
              break;
            }
            case "message": {
              // A new row starts a new step, so close the previous one first.
              // The step must NOT be closed straight after emitting tool
              // inputs: their outputs arrive as later frames and belong in the
              // same step. assistant-ui's resume check looks at the last step,
              // and a step holding tool calls with no outputs makes it re-send
              // a turn the server is still running — which is exactly what
              // produced a second sendMessage mid-turn.
              // Close the text part before the step that owns it closes:
              // assistant-ui drops a step's parts when the step ends, so a
              // text-end emitted afterwards addresses a part that no longer
              // exists ("Received text-end for missing text part").
              if (sawTextThisMessage) {
                writer.write({
                  type: "text-end",
                  id: `turn-${chatId}-${messageIndex}`,
                });
                messageIndex++;
                sawTextThisMessage = false;
              }
              const calls = parseFrameToolCalls(frame.tool_calls);
              // Every row gets its own step, including the final text-only
              // one. That is what keeps the turn's last step free of tool
              // calls: assistant-ui's resume check inspects only the last
              // step, and if it finds tool calls there it re-sends a turn the
              // server already finished. Skipping the step for a row with no
              // tool calls left the tool-calling step last and restarted that
              // loop.
              if (stepHasMessage) closeStep();
              openStep();
              stepHasMessage = true;
              for (const call of calls) {
                writer.write({
                  type: "tool-input-available",
                  toolCallId: call.id,
                  toolName: call.name,
                  input: call.input,
                });
                pendingToolCalls.add(call.id);
              }
              break;
            }
            case "tool_output": {
              if (!frame.tool_call_id) break;
              writer.write({
                type: "tool-output-available",
                toolCallId: frame.tool_call_id,
                output: frame.output ?? "",
              });
              pendingToolCalls.delete(frame.tool_call_id);
              break;
            }
            case "done": {
              done = true;
              break;
            }
          }
          if (done) break;
        }
      }

      if (done) {
        release();
        return;
      }
      release();
      // The server closed without a terminal frame — the turn is still
      // running, so resume from the cursor rather than treating it as over.
      if (++reconnects > MAX_RECONNECTS) {
        throw new Error("turn stream ended before the turn finished");
      }
      await new Promise((r) => setTimeout(r, RECONNECT_DELAY_MS));
    }
  } finally {
    // A turn that ended without an output for some call would otherwise be
    // re-sent by assistant-ui's resume check.
    for (const id of pendingToolCalls) {
      writer.write({
        type: "tool-output-available",
        toolCallId: id,
        output: "",
      });
    }
    pendingToolCalls.clear();
    if (sawTextThisMessage) {
      writer.write({ type: "text-end", id: `turn-${chatId}-${messageIndex}` });
      sawTextThisMessage = false;
    }
    closeStep();
  }
}
