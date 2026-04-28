import { describe, expect, it } from "vitest";
import {
  convertToModelMessages,
  isToolUIPart,
  jsonSchema,
  lastAssistantMessageIsCompleteWithToolCalls,
  readUIMessageStream,
  stepCountIs,
  streamText,
  type ToolSet,
  type UIMessage,
  type UIMessagePart,
} from "ai";
import { MockLanguageModelV2 } from "ai/test";

type MockStream = Extract<
  NonNullable<
    NonNullable<
      ConstructorParameters<typeof MockLanguageModelV2>[0]
    >["doStream"]
  >,
  (...a: never[]) => PromiseLike<{ stream: ReadableStream<unknown> }>
>;
type StreamPart =
  Awaited<ReturnType<MockStream>>["stream"] extends ReadableStream<infer T>
    ? T
    : never;

/**
 * Repro for the assistants-onboarding "Skip bugged state":
 *
 * 1. Assistant calls a frontend tool (e.g. `request_environment_secrets`) that
 *    renders a form with a Skip button.
 * 2. User clicks Skip. The form calls `draft.resolvePending(toolCallId, { cancelled: true })`.
 * 3. Expected: tool-result is patched onto the message, the agent continues,
 *    chat returns to a ready state.
 * 4. Observed (pre-fix): the chat stayed stuck — the next user message landed
 *    with an invalid tool sequence and the provider rejected it with a
 *    "message needing to be sent with role: assistant"-shaped error.
 *
 * `streamText` runs without an `execute` for frontend tools: AI-SDK's
 * `frontendTools()` helper strips execute so client-side logic can take over.
 * The missing link on main was that the runtime patched in the tool result
 * but nothing resumed the turn. The fix wires `sendAutomaticallyWhen:
 * lastAssistantMessageIsCompleteWithToolCalls` into `useChatRuntime`, which
 * flips that resume on.
 */

function toolCallChunks(opts: {
  toolCallId: string;
  toolName: string;
  input: string;
}): StreamPart[] {
  return [
    { type: "stream-start", warnings: [] },
    {
      type: "response-metadata",
      id: "resp-1",
      modelId: "m",
      timestamp: new Date(0),
    },
    { type: "tool-input-start", id: opts.toolCallId, toolName: opts.toolName },
    { type: "tool-input-delta", id: opts.toolCallId, delta: opts.input },
    { type: "tool-input-end", id: opts.toolCallId },
    {
      type: "tool-call",
      toolCallId: opts.toolCallId,
      toolName: opts.toolName,
      input: opts.input,
    },
    {
      type: "finish",
      finishReason: "tool-calls",
      usage: { inputTokens: 1, outputTokens: 1, totalTokens: 2 },
    },
  ];
}

function makeStream<T>(chunks: T[]): ReadableStream<T> {
  return new ReadableStream({
    start(controller) {
      for (const c of chunks) controller.enqueue(c);
      controller.close();
    },
  });
}

async function collectUIMessages(
  stream: AsyncIterable<UIMessage>,
): Promise<UIMessage[]> {
  const out: UIMessage[] = [];
  for await (const msg of stream) {
    const idx = out.findIndex((m) => m.id === msg.id);
    if (idx >= 0) out[idx] = msg;
    else out.push(msg);
  }
  return out;
}

async function streamToolCallOnly(toolCallId: string): Promise<UIMessage[]> {
  const toolsNoExecute = {
    request_environment_secrets: {
      description: "Ask the user to enter secrets for an env.",
      inputSchema: jsonSchema({
        type: "object",
        properties: {
          keys: {
            type: "array",
            items: {
              type: "object",
              properties: { name: { type: "string" } },
              required: ["name"],
            },
          },
        },
        required: ["keys"],
      }),
    },
  } as unknown as ToolSet;

  const model = new MockLanguageModelV2({
    doStream: async () => ({
      stream: makeStream([
        ...toolCallChunks({
          toolCallId,
          toolName: "request_environment_secrets",
          input: JSON.stringify({ keys: [{ name: "SLACK_BOT_TOKEN" }] }),
        }),
      ]),
    }),
  });

  const result = streamText({
    model,
    messages: [{ role: "user", content: "Set up Slack" }],
    tools: toolsNoExecute,
    stopWhen: stepCountIs(5),
  });
  return collectUIMessages(
    readUIMessageStream({ stream: result.toUIMessageStream() }),
  );
}

describe("frontend tool Skip flow (sendAutomaticallyWhen fix)", () => {
  it("without a tool-result, the message sequence is invalid — this is the bug we are fixing", async () => {
    // Mirrors the elements flow on main: frontend tool has no execute inside
    // streamText (it's run client-side by useToolInvocations). If nothing
    // patches a tool-result onto the message, sending a follow-up user
    // message produces an invalid sequence.
    const toolCallId = "call_unresolved";
    const messages = await streamToolCallOnly(toolCallId);

    const assistant = messages.find((m) => m.role === "assistant")!;
    const toolParts = (assistant.parts as UIMessagePart<never, never>[]).filter(
      (p) => isToolUIPart(p),
    );
    expect(toolParts).toHaveLength(1);
    expect((toolParts[0] as unknown as { state: string }).state).toBe(
      "input-available",
    );

    const follow: UIMessage[] = [
      ...messages,
      {
        id: "u2",
        role: "user",
        parts: [{ type: "text", text: "skip" }],
      } as unknown as UIMessage,
    ];

    // Also: `lastAssistantMessageIsCompleteWithToolCalls` must return `false`
    // here — there is no tool result, so the runtime should NOT auto-resume.
    expect(lastAssistantMessageIsCompleteWithToolCalls({ messages })).toBe(
      false,
    );

    // And the resulting model-message sequence contains a bogus `role: "tool"`
    // with empty content — the provider will reject this as an invalid tool
    // message, surfacing to the user as the "needs role: assistant" error.
    const modelMsgs = convertToModelMessages(follow);
    const assistantIdx = modelMsgs.findIndex(
      (m) =>
        m.role === "assistant" &&
        Array.isArray(m.content) &&
        (m.content as Array<{ type: string }>).some(
          (c) => c.type === "tool-call",
        ),
    );
    expect(assistantIdx).toBeGreaterThanOrEqual(0);
    expect(modelMsgs[assistantIdx + 1]?.role).toBe("tool");
    expect(modelMsgs[assistantIdx + 1]?.content).toEqual([]);
  });

  it("once the tool-result is patched onto the message, sendAutomaticallyWhen fires and the sequence is valid", async () => {
    // Simulates the full post-fix behaviour: `useToolInvocations` ran execute
    // client-side and called `addToolResult`, which flips the tool part to
    // `output-available`. With the result in place:
    //   - `lastAssistantMessageIsCompleteWithToolCalls` returns true, so the
    //     runtime re-issues the model turn (this is the 1-line fix).
    //   - `convertToModelMessages` produces a real `role: "tool"` message
    //     with the result, which the provider accepts.
    const toolCallId = "call_resolved";
    const rawMessages = await streamToolCallOnly(toolCallId);

    // Patch in the tool-output-available state — this is the shape
    // `chatHelpers.addToolResult` produces under the hood.
    const patched: UIMessage[] = rawMessages.map((m) => {
      if (m.role !== "assistant") return m;
      return {
        ...m,
        parts: (m.parts as Array<Record<string, unknown>>).map((p) =>
          isToolUIPart(p as UIMessagePart<never, never>)
            ? {
                ...p,
                state: "output-available",
                output: { ok: true, cancelled: true },
              }
            : p,
        ),
      } as UIMessage;
    });

    const toolPart = (
      (
        patched.find((m) => m.role === "assistant")!.parts as UIMessagePart<
          never,
          never
        >[]
      ).filter((p) => isToolUIPart(p))[0] as unknown as { state: string }
    ).state;
    expect(toolPart).toBe("output-available");

    // Pre-condition for the fix: the runtime auto-resumes the turn.
    expect(
      lastAssistantMessageIsCompleteWithToolCalls({ messages: patched }),
    ).toBe(true);

    // And the sequence handed to the model is well-formed (assistant
    // tool-call is followed by a real role:"tool" message with a result).
    const modelMsgs = convertToModelMessages(patched);
    const assistantIdx = modelMsgs.findIndex(
      (m) =>
        m.role === "assistant" &&
        Array.isArray(m.content) &&
        (m.content as Array<{ type: string }>).some(
          (c) => c.type === "tool-call",
        ),
    );
    const next = modelMsgs[assistantIdx + 1];
    expect(next?.role).toBe("tool");
    expect(Array.isArray(next?.content) && next.content.length).toBeGreaterThan(
      0,
    );
    const toolResult = (
      next?.content as Array<{ type: string; output?: { type?: string } }>
    )[0];
    expect(toolResult?.type).toBe("tool-result");
  });
});
