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
  /**
   * Tool calls surfaced but not yet answered, owned by the caller so the state
   * survives a failed stream. If this throws, whatever remains in here is a
   * call the poll fallback still has to resolve from the persisted tool row.
   */
  pendingToolCalls?: Set<string>;
  /**
   * Called once the subscription is live on the server. The handler flushes
   * the SSE headers immediately after subscribing, so a resolved response
   * proves no frame published from here on can be missed — which is what lets
   * the caller send the message only after the stream is listening.
   */
  onSubscribed?: () => void;
  /**
   * Replay the chat's retained frames instead of starting from now. Correct
   * only for a chat created by this turn, where the retained history IS this
   * turn; on an existing chat it would replay a previous turn's terminal frame
   * and end this one before it began.
   */
  replayFromStart?: boolean;
}): Promise<void> {
  const { chatId, writer, abortSignal, sessionToken, projectSlug } = args;

  let cursor = "";
  let reconnects = 0;
  // Each persisted assistant row's content gets its own step, so the turn's
  // final text lands in a step with no tool calls — assistant-ui's resume
  // check inspects the last step, and a step carrying resolved tool calls
  // makes it re-send a turn the server already finished.
  //
  // A step is only observable once something is written into it: the AI
  // SDK pushes the `step-start` part on `start-step` but does not flush the
  // message, so an empty trailing step never reaches the consumer. Steps have
  // to be arranged so the turn's last CONTENT is free of tool calls; opening a
  // bare step after them does nothing.
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
  // the turn ends cleanly are closed out rather than left to trigger that.
  //
  // The caller owns the set so a failed stream can hand its unresolved calls to
  // the poll fallback, which resolves them from the persisted tool rows. Those
  // ids are `chat_messages.tool_call_id`, the same ids the poll matches on.
  const pendingToolCalls = args.pendingToolCalls ?? new Set<string>();
  // Whether the turn reached its terminal frame. Only then is fabricating an
  // empty output for an unanswered call safe: on a failure the call may still
  // be running, and inventing a result both shows the user something that did
  // not happen and can be sent back to the model as if it had.
  let completed = false;
  // Whether the caller has been told the subscription is live.
  let subscribed = false;

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
      if (cursor) {
        url.searchParams.set("after", cursor);
      } else if (args.replayFromStart) {
        url.searchParams.set("after", "0");
      }

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
        await new Promise((r) => {
          setTimeout(r, RECONNECT_DELAY_MS);
        });
        continue;
      }
      if (!response.ok || !response.body) {
        // A 4xx is the server refusing the subscription — streaming disabled,
        // bad chat id — and retrying cannot change its answer. Failing now
        // matters because the caller's send waits on `subscribed`: burning the
        // retry budget here would delay every message on a deployment without
        // streaming, when the poll fallback could start immediately.
        const refused =
          !response.ok &&
          response.status >= 400 &&
          response.status < 500 &&
          response.status !== 408 &&
          response.status !== 429;
        if (refused || ++reconnects > MAX_RECONNECTS) {
          throw new Error(`turn stream failed: ${response.status}`);
        }
        await new Promise((r) => {
          setTimeout(r, RECONNECT_DELAY_MS);
        });
        continue;
      }
      reconnects = 0;
      // Only after the first successful connection: a reconnect is resuming a
      // subscription the caller already acted on.
      if (!subscribed) {
        subscribed = true;
        args.onSubscribed?.();
      }

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

      // Set when the connection fails mid-stream. A rejected read is a dropped
      // connection, which is exactly what the reconnect loop below exists for
      // — letting it propagate skipped straight past it to the poll fallback.
      let dropped = false;
      while (!done) {
        let finished: boolean;
        let value: Uint8Array | undefined;
        try {
          ({ done: finished, value } = await reader.read());
        } catch (err) {
          if (abortSignal?.aborted) throw err;
          dropped = true;
          break;
        }
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
              // Text that arrives after a row has landed belongs to the NEXT
              // row, so it opens a step of its own. Sharing the previous row's
              // step put the turn's closing text in the same step as the tool
              // calls that preceded it, and assistant-ui's resume check
              // (`lastAssistantMessageIsCompleteWithToolCalls`) inspects the
              // last step: finding resolved tool calls there, it re-sent a turn
              // the server had already finished — forever, since every resend
              // reproduced the same shape.
              if (stepHasMessage) closeStep();
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
              // A row that follows another opens its own step, so a row's tool
              // calls never share a step with the next row's content. The step
              // this opens is only real once something is written into it (see
              // `stepOpen` above), which is why the turn's closing text opens
              // its step itself rather than relying on this.
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
        completed = true;
        release();
        return;
      }
      release();
      // The connection ended without a terminal frame — dropped, or closed by
      // the server while the turn is still running. Either way resume from the
      // cursor rather than treating the turn as over.
      if (++reconnects > MAX_RECONNECTS) {
        throw new Error(
          dropped
            ? "turn stream connection dropped"
            : "turn stream ended before the turn finished",
        );
      }
      await new Promise((r) => {
        setTimeout(r, RECONNECT_DELAY_MS);
      });
    }
  } finally {
    // A turn that ended without an output for some call would otherwise be
    // re-sent by assistant-ui's resume check. Only do this once the turn is
    // known to have ended: on a failed stream the call may still be running,
    // and an invented empty result is both shown to the user and sent back to
    // the model as though the tool had returned it. Those calls stay in the
    // set instead, for the caller's resync to resolve from the tool rows.
    if (completed) {
      for (const id of pendingToolCalls) {
        writer.write({
          type: "tool-output-available",
          toolCallId: id,
          output: "",
        });
      }
      pendingToolCalls.clear();
    }
    if (sawTextThisMessage) {
      writer.write({ type: "text-end", id: `turn-${chatId}-${messageIndex}` });
      sawTextThisMessage = false;
    }
    closeStep();
  }
}
