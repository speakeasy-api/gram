import { Chat } from "@ai-sdk/react";
import {
  createUIMessageStream,
  lastAssistantMessageIsCompleteWithToolCalls,
  readUIMessageStream,
  type UIMessage,
} from "ai";
import { afterEach, describe, expect, it, vi } from "vitest";

import { streamTurn } from "./turnStream";

// The turn stream is only correct if what assistant-ui ends up holding is a
// finished message. Asserting on the chunks we emit proves nothing: the same
// sequence can leave the runtime mid-step, mid-part, or looking like a turn
// that still owes tool outputs — which is what keeps the spinner alive and
// makes `sendAutomaticallyWhen` re-send a finished turn. So these tests feed
// real server frames through `streamTurn` and inspect the message the AI SDK
// reconstructs, the same way the runtime does.

vi.mock("@/lib/utils", () => ({ getServerURL: () => "http://test.local" }));

const CHAT_ID = "chat-1";

interface Frame {
  kind: "text" | "message" | "tool_output" | "done";
  cursor?: string;
  text?: string;
  message_id?: string;
  tool_calls?: unknown;
  finish_reason?: string;
  tool_call_id?: string;
  output?: unknown;
}

/** Serialises frames as the SSE body the handler writes. */
function sseBody(frames: Frame[]): string {
  return frames.map((f) => `data: ${JSON.stringify(f)}\n\n`).join("");
}

/**
 * Stubs fetch with one SSE response per call, so a test can also exercise the
 * reconnect path by supplying more than one body.
 */
function stubFetch(bodies: string[]): void {
  let call = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      const body = bodies[Math.min(call++, bodies.length - 1)] ?? "";
      return new Response(new TextEncoder().encode(body), {
        status: 200,
        headers: { "content-type": "text/event-stream" },
      });
    }),
  );
}

/**
 * Runs a turn end to end and returns the message assistant-ui would hold,
 * reconstructed by the AI SDK from the chunks `streamTurn` wrote.
 */
async function runTurn(frames: Frame[]): Promise<UIMessage> {
  stubFetch([sseBody(frames)]);

  const stream = createUIMessageStream<UIMessage>({
    originalMessages: [{ id: "u1", role: "user", parts: [] }],
    execute: async ({ writer }) => {
      writer.write({ type: "start" });
      await streamTurn({
        chatId: CHAT_ID,
        writer,
        sessionToken: "session",
        projectSlug: "proj",
      });
      writer.write({ type: "finish" });
    },
  });

  let last: UIMessage | undefined;
  for await (const message of readUIMessageStream({ stream })) {
    last = message;
  }
  if (!last) throw new Error("the turn produced no message");
  return last;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("streamTurn", () => {
  // The spinner that outlived a finished reply. A text-only turn must leave a
  // message the runtime considers done — no open text part, no open step.
  it("leaves a text-only turn complete", async () => {
    const message = await runTurn([
      { kind: "text", cursor: "1-0", text: "Hel" },
      { kind: "text", cursor: "1-1", text: "lo" },
      {
        kind: "message",
        cursor: "1-2",
        message_id: "m1",
        finish_reason: "stop",
      },
      { kind: "done", cursor: "1-3", finish_reason: "stop" },
    ]);

    const text = message.parts
      .filter((p): p is { type: "text"; text: string } => p.type === "text")
      .map((p) => p.text)
      .join("");
    expect(text).toBe("Hello");
    expect(
      message.parts.every((p) => p.type !== "text" || p.state === "done"),
    ).toBe(true);
    expect(
      lastAssistantMessageIsCompleteWithToolCalls({
        messages: [message],
      }),
    ).toBe(false);
  });

  // A tool-using turn must not look resumable once it ends, or
  // `sendAutomaticallyWhen` re-sends a turn the server already finished —
  // which is what produced a second send and a spinner mid-conversation.
  it("does not look resumable after a tool-using turn", async () => {
    const message = await runTurn([
      {
        kind: "message",
        cursor: "1-0",
        message_id: "m1",
        finish_reason: "tool_calls",
        tool_calls: [
          {
            id: "call-1",
            function: { name: "compose", arguments: '{"script":"x"}' },
          },
        ],
      },
      {
        kind: "tool_output",
        cursor: "1-1",
        tool_call_id: "call-1",
        output: "done",
      },
      { kind: "text", cursor: "1-2", text: "All set" },
      {
        kind: "message",
        cursor: "1-3",
        message_id: "m2",
        finish_reason: "stop",
      },
      { kind: "done", cursor: "1-4", finish_reason: "stop" },
    ]);

    const tools = message.parts.filter((p) => p.type.startsWith("tool-"));
    expect(tools).toHaveLength(1);
    expect(tools[0]).toMatchObject({ state: "output-available" });
    expect(
      lastAssistantMessageIsCompleteWithToolCalls({ messages: [message] }),
    ).toBe(false);
  });

  // A turn whose tool call is never answered still has to end cleanly: an
  // unresolved call leaves the runtime waiting for an output that is not
  // coming.
  it("closes out a tool call the turn never answered", async () => {
    const message = await runTurn([
      {
        kind: "message",
        cursor: "1-0",
        message_id: "m1",
        finish_reason: "tool_calls",
        tool_calls: [{ id: "call-1", function: { name: "compose" } }],
      },
      { kind: "done", cursor: "1-1", finish_reason: "stop" },
    ]);

    const tools = message.parts.filter((p) => p.type.startsWith("tool-"));
    expect(tools).toHaveLength(1);
    expect(tools[0]).toMatchObject({ state: "output-available" });
  });

  // The failure this whole file exists for. Chunk-level assertions all passed
  // while the dashboard span forever, because the runtime — not the chunk
  // sequence — decides whether a turn is finished. Driving a real `Chat` with
  // the same `sendAutomaticallyWhen` the dashboard uses is the only check that
  // sees it: before the fix this looped until the process ran out of memory.
  it("does not re-send a turn that has finished", async () => {
    stubFetch([
      sseBody([
        {
          kind: "message",
          cursor: "1-0",
          message_id: "m1",
          finish_reason: "tool_calls",
          tool_calls: [{ id: "call-1", function: { name: "compose" } }],
        },
        {
          kind: "tool_output",
          cursor: "1-1",
          tool_call_id: "call-1",
          output: "ok",
        },
        { kind: "text", cursor: "1-2", text: "All set" },
        {
          kind: "message",
          cursor: "1-3",
          message_id: "m2",
          finish_reason: "stop",
        },
        { kind: "done", cursor: "1-4", finish_reason: "stop" },
      ]),
    ]);

    let sends = 0;
    const chat = new Chat<UIMessage>({
      sendAutomaticallyWhen: lastAssistantMessageIsCompleteWithToolCalls,
      transport: {
        sendMessages: async ({ messages }) => {
          // A regression here re-sends without bound, so stop rather than
          // letting the runner die of heap exhaustion.
          if (++sends > 2) throw new Error("runaway resend");
          return createUIMessageStream<UIMessage>({
            originalMessages: messages,
            execute: async ({ writer }) => {
              writer.write({ type: "start" });
              await streamTurn({
                chatId: CHAT_ID,
                writer,
                sessionToken: "session",
                projectSlug: "proj",
              });
              writer.write({ type: "finish" });
            },
          });
        },
        reconnectToStream: async () => null,
      },
    });

    await chat.sendMessage({ text: "hi" });
    // `sendAutomaticallyWhen` is evaluated after the stream settles, so give
    // the runtime a turn of the event loop to decide before asserting.
    await new Promise((resolve) => {
      setTimeout(resolve, 50);
    });

    expect(sends).toBe(1);
    expect(chat.status).toBe("ready");
  });

  // Subscribing starts from "now", so the caller only sends the message once
  // the subscription is live. Signalling that any later would reopen the race
  // the ordering exists to close: frames published in the gap are lost, and a
  // turn that finished inside it leaves the client tailing forever.
  it("signals the subscription is live before any frame arrives", async () => {
    stubFetch([
      sseBody([
        { kind: "text", cursor: "1-0", text: "hi" },
        { kind: "done", cursor: "1-1", finish_reason: "stop" },
      ]),
    ]);

    // Recorded at the writer rather than by consuming the stream: breaking out
    // of the consumer early abandons a controller that `execute` is still
    // writing to, which surfaces as an unhandled "Controller is already
    // closed" and fails the run even though every assertion passed.
    const events: string[] = [];
    const stream = createUIMessageStream<UIMessage>({
      originalMessages: [{ id: "u1", role: "user", parts: [] }],
      execute: async ({ writer }) => {
        const recording: typeof writer = {
          ...writer,
          write: (chunk) => {
            if (chunk.type === "text-delta") events.push("text");
            return writer.write(chunk);
          },
        };
        recording.write({ type: "start" });
        await streamTurn({
          chatId: CHAT_ID,
          writer: recording,
          sessionToken: "session",
          projectSlug: "proj",
          onSubscribed: () => {
            events.push("subscribed");
          },
          replayFromStart: true,
        });
        recording.write({ type: "finish" });
      },
    });
    for await (const _ of readUIMessageStream({ stream })) {
      // Drain: the assertions read `events`, but the stream still has to be
      // consumed to completion so `execute` finishes.
    }

    expect(events[0]).toBe("subscribed");
    expect(events).toContain("text");
    // A chat created by this turn has no earlier turn to replay, so starting
    // from the beginning is exact — and it cannot miss frames published before
    // the connection was made.
    const first = vi.mocked(fetch).mock.calls[0]![0] as URL;
    expect(first.searchParams.get("after")).toBe("0");
  });

  // The reconnect contract: a body that ends without a terminal frame means
  // the turn is still running, so the next request resumes from the last
  // cursor rather than the client treating the turn as over.
  it("resumes from the last cursor when the connection drops", async () => {
    stubFetch([
      sseBody([{ kind: "text", cursor: "1-0", text: "par" }]),
      sseBody([
        { kind: "text", cursor: "1-1", text: "tial" },
        {
          kind: "message",
          cursor: "1-2",
          message_id: "m1",
          finish_reason: "stop",
        },
        { kind: "done", cursor: "1-3", finish_reason: "stop" },
      ]),
    ]);

    const stream = createUIMessageStream<UIMessage>({
      originalMessages: [{ id: "u1", role: "user", parts: [] }],
      execute: async ({ writer }) => {
        writer.write({ type: "start" });
        await streamTurn({
          chatId: CHAT_ID,
          writer,
          sessionToken: "session",
          projectSlug: "proj",
        });
        writer.write({ type: "finish" });
      },
    });

    let last: UIMessage | undefined;
    for await (const message of readUIMessageStream({ stream })) {
      last = message;
    }

    const text = (last?.parts ?? [])
      .filter((p): p is { type: "text"; text: string } => p.type === "text")
      .map((p) => p.text)
      .join("");
    expect(text).toBe("partial");

    const calls = vi.mocked(fetch).mock.calls;
    expect(calls).toHaveLength(2);
    const resumed = calls[1]![0] as URL;
    expect(resumed.searchParams.get("after")).toBe("1-0");
  });
});
