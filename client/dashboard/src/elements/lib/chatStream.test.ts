// @vitest-environment node
import {
  buildChatTools,
  createChatModel,
  streamChatTurn,
} from "@/elements/lib/chatStream";
import { DEFAULT_MODEL } from "@/elements/lib/models";
import type { ApprovalHelpers } from "@/elements/lib/tools";
import { createMCPClientWithVersionFallback } from "@/lib/mcp-client";
import {
  isRecordingFixtures,
  startChatTestServer,
  summarizeOpenRouterFixture,
  WEATHER_PROMPT,
  WEATHER_TOOL_RESULT,
  type ChatTestServer,
} from "@/test/chatTestServer";
import {
  readUIMessageStream,
  type ToolSet,
  type UIMessage,
  type UIMessageChunk,
} from "ai";
import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";

// Runs Playground chat turns through the real AI SDK, OpenRouter provider and
// @ai-sdk/mcp client. Only the network edges are local: a real MCP server and
// an OpenRouter endpoint that replays recorded streams. These tests exist to
// catch AI SDK dependency updates that unit tests with mocked SDK calls miss.
//
// Expected text and tool calls come from the fixtures, so re-recording them
// needs no test changes. To re-record from the real OpenRouter API (see
// isRecordingFixtures in src/test/chatTestServer.ts), from client/dashboard:
//
//   RECORD_OPENROUTER_FIXTURES=1 mise exec -- aube exec vitest run src/elements/lib/chatStream.test.ts

const INSTRUCTIONS = "You are a test assistant.";

const approvalHelpers: ApprovalHelpers = {
  requestApproval: () => Promise.resolve(true),
  isToolApproved: () => true,
  whitelistTool: () => {},
};

let server: ChatTestServer;
let closeMcpClient: () => Promise<void>;
let mcpTools: ToolSet;

beforeAll(async () => {
  server = await startChatTestServer();
  const client = await createMCPClientWithVersionFallback({
    clientName: "gram-elements-mcp-client",
    transport: { type: "http", url: server.mcpUrl },
  });
  closeMcpClient = () => client.close();
  mcpTools = (await client.tools()) as ToolSet;
});

afterAll(async () => {
  await closeMcpClient?.();
  await server?.close();
});

beforeEach(() => {
  server.reset();
});

function userMessage(id: string, text: string): UIMessage {
  return { id, role: "user", parts: [{ type: "text", text }] };
}

async function runTurn(
  messages: UIMessage[],
  onStreamError: (error: unknown) => void = () => {},
) {
  const tools = buildChatTools({
    mcpTools,
    frontendTools: {},
    toolsRequiringApproval: undefined,
    approvalHelpers,
    maxOutputBytes: undefined,
  });
  const stream = await streamChatTurn({
    messages,
    model: createChatModel({
      apiUrl: server.baseUrl,
      headers: { "Gram-Chat-ID": "chat-1", "X-Gram-Source": "elements" },
      modelId: DEFAULT_MODEL,
    }),
    modelId: DEFAULT_MODEL,
    instructions: INSTRUCTIONS,
    tools,
    onStreamError,
  });

  const [forChunks, forMessage] = stream.tee();
  const chunks: UIMessageChunk[] = [];
  const collectChunks = (async () => {
    for await (const chunk of forChunks) chunks.push(chunk);
  })();
  let assistant: UIMessage | undefined;
  for await (const message of readUIMessageStream({ stream: forMessage })) {
    assistant = message;
  }
  await collectChunks;
  return { chunks, assistant };
}

function textOf(message: UIMessage | undefined): string {
  return (message?.parts ?? [])
    .flatMap((part) => (part.type === "text" ? [part.text] : []))
    .join("");
}

/** The weather tool call a fixture asks for, with its parsed input. */
function weatherCallIn(fixture: string) {
  const call = summarizeOpenRouterFixture(fixture).toolCalls[0];
  expect(call, `${fixture} has no tool call`).toBeDefined();
  expect(call!.name).toBe("get_weather");
  return { ...call!, input: JSON.parse(call!.arguments) as { city: string } };
}

// Recording waits on the real model.
describe(
  "Playground chat turn",
  { timeout: isRecordingFixtures ? 120_000 : undefined },
  () => {
    it("lists MCP tools through @ai-sdk/mcp", () => {
      expect(Object.keys(mcpTools)).toEqual(["get_weather"]);
    });

    it("calls an MCP tool and streams the answer", async () => {
      server.queueModelReplies(
        { kind: "stream", fixture: "tool-call-get-weather" },
        { kind: "stream", fixture: "text-weather-answer" },
      );

      const { chunks, assistant } = await runTurn([
        userMessage("u1", WEATHER_PROMPT),
      ]);
      const call = weatherCallIn("tool-call-get-weather");

      // UI stream: tool input, tool output from the MCP server, then text.
      expect(chunks).toContainEqual(
        expect.objectContaining({
          type: "tool-input-available",
          toolCallId: call.id,
          toolName: "get_weather",
          input: call.input,
        }),
      );
      const output = chunks.find((c) => c.type === "tool-output-available");
      expect(JSON.stringify(output)).toContain(
        `${WEATHER_TOOL_RESULT} in ${call.input.city}`,
      );
      expect(chunks.at(-1)).toMatchObject({ type: "finish" });
      const answer = summarizeOpenRouterFixture("text-weather-answer").text;
      expect(answer).not.toBe("");
      expect(textOf(assistant)).toBe(
        summarizeOpenRouterFixture("tool-call-get-weather").text + answer,
      );

      // First model request: headers, instructions, and the MCP tool schema.
      expect(server.modelRequests).toHaveLength(2);
      const [first, second] = server.modelRequests;
      expect(first!.headers["gram-chat-id"]).toBe("chat-1");
      expect(first!.headers["x-gram-source"]).toBe("elements");
      expect(first!.body).toMatchObject({ model: DEFAULT_MODEL, stream: true });
      expect(first!.body.messages[0]).toMatchObject({ role: "system" });
      expect(JSON.stringify(first!.body.messages[0])).toContain(INSTRUCTIONS);
      const tool = first!.body.tools?.find(
        (t) => t.function.name === "get_weather",
      );
      expect(tool?.function.parameters).toMatchObject({
        type: "object",
        properties: { city: { type: "string" } },
      });

      // Second model request carries the tool call and the MCP result.
      expect(second!.body.messages).toContainEqual(
        expect.objectContaining({
          role: "assistant",
          tool_calls: [
            expect.objectContaining({
              id: call.id,
              function: expect.objectContaining({ name: "get_weather" }),
            }),
          ],
        }),
      );
      const toolMessage = second!.body.messages.find((m) => m.role === "tool");
      expect(toolMessage).toMatchObject({ tool_call_id: call.id });
      expect(JSON.stringify(toolMessage)).toContain(WEATHER_TOOL_RESULT);
    });

    it("replays a finished tool turn as history on the next turn", async () => {
      server.queueModelReplies(
        { kind: "stream", fixture: "tool-call-get-weather" },
        { kind: "stream", fixture: "text-weather-answer" },
      );
      const firstUser = userMessage("u1", WEATHER_PROMPT);
      const { assistant } = await runTurn([firstUser]);

      server.reset();
      server.queueModelReplies({ kind: "stream", fixture: "text-follow-up" });
      const { assistant: reply } = await runTurn([
        firstUser,
        assistant!,
        userMessage("u2", "Thanks!"),
      ]);

      const followUp = summarizeOpenRouterFixture("text-follow-up").text;
      expect(followUp).not.toBe("");
      expect(textOf(reply)).toBe(followUp);
      expect(server.modelRequests).toHaveLength(1);
      const roles = server.modelRequests[0]!.body.messages.map((m) => m.role);
      expect(roles).toEqual([
        "system",
        "user",
        "assistant",
        "tool",
        "assistant",
        "user",
      ]);
      const history = JSON.stringify(server.modelRequests[0]!.body.messages);
      expect(history).toContain(weatherCallIn("tool-call-get-weather").id);
      expect(history).toContain(WEATHER_TOOL_RESULT);
    });
  },
);
