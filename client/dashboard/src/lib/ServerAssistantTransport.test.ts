// @vitest-environment node
import { createServer, type IncomingMessage, type Server } from "node:http";
import { Chat } from "@ai-sdk/react";
import { GramCore } from "@gram/client/core";
import {
  lastAssistantMessageIsCompleteWithToolCalls,
  type ChatTransport,
  type UIMessage,
} from "ai";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import { WEATHER_PROMPT, WEATHER_TOOL_RESULT } from "@/test/chatTestServer";
import { closeServer, listenOnLoopback, readBody } from "@/test/loopbackServer";

// Runs Project Assistant turns through the real transport, the real
// @gram/client SDK, and the real AI SDK `Chat` (the state engine behind
// useChat), with the same `sendAutomaticallyWhen` the dashboard uses. Only the
// Speakeasy API is local: a loopback server for assistants.sendMessage,
// chat.load, and /chat/turnstream. These tests exist to catch AI SDK updates
// that change how the assistant's reply ends up in the chat.

const state = vi.hoisted(() => ({ serverURL: "" }));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => state.serverURL,
}));

import { createServerAssistantTransport } from "./ServerAssistantTransport";

const ASSISTANT_ID = "assistant-1";
const PROJECT = "proj";
const SESSION = "session-token";
const NEW_CHAT_ID = "chat-new";
const EXISTING_CHAT_ID = "chat-existing";
const TIMESTAMP = "2026-11-13T12:00:00Z";

type Frame = {
  kind: "text" | "message" | "tool_output" | "done";
  cursor?: string;
  text?: string;
  message_id?: string;
  tool_calls?: unknown;
  finish_reason?: string;
  tool_call_id?: string;
  output?: unknown;
};

type Row = {
  id: string;
  role: "user" | "assistant" | "tool";
  content?: unknown;
  finish_reason?: string;
  tool_calls?: string;
  tool_call_id?: string;
};

type RecordedRequest = {
  path: string;
  query: URLSearchParams;
  headers: IncomingMessage["headers"];
  body: unknown;
};

const WEATHER_CALL = {
  id: "call-weather-1",
  type: "function",
  function: { name: "get_weather", arguments: '{"city":"Paris"}' },
};
const TOOL_OUTPUT = { forecast: WEATHER_TOOL_RESULT };
const INTRO = "Checking the weather.";
const ANSWER = `It is ${WEATHER_TOOL_RESULT} in Paris.`;

// One tool-using turn, as the server publishes it.
const TOOL_TURN_FRAMES: Frame[] = [
  { kind: "text", cursor: "1-0", text: INTRO },
  {
    kind: "message",
    cursor: "1-1",
    message_id: "m1",
    finish_reason: "tool_calls",
    tool_calls: [WEATHER_CALL],
  },
  {
    kind: "tool_output",
    cursor: "1-2",
    tool_call_id: WEATHER_CALL.id,
    output: TOOL_OUTPUT,
  },
  { kind: "text", cursor: "1-3", text: ANSWER },
  { kind: "message", cursor: "1-4", message_id: "m2", finish_reason: "stop" },
  { kind: "done", cursor: "1-5", finish_reason: "stop" },
];

// The same turn as chat.load returns it once persisted.
const TOOL_TURN_ROWS: Row[] = [
  { id: "u1", role: "user", content: WEATHER_PROMPT },
  {
    id: "m1",
    role: "assistant",
    content: INTRO,
    finish_reason: "tool_calls",
    tool_calls: JSON.stringify([WEATHER_CALL]),
  },
  {
    id: "t1",
    role: "tool",
    tool_call_id: WEATHER_CALL.id,
    content: JSON.stringify(TOOL_OUTPUT),
  },
  { id: "m2", role: "assistant", content: ANSWER, finish_reason: "stop" },
];

function chatBody(chatId: string, rows: Row[]) {
  return {
    id: chatId,
    title: "Weather",
    generation: 0,
    max_generation: 0,
    has_more_after: false,
    has_more_before: false,
    num_messages: rows.length,
    content_parts: [],
    created_at: TIMESTAMP,
    updated_at: TIMESTAMP,
    last_message_timestamp: TIMESTAMP,
    messages: rows.map((row, seq) => ({
      ...row,
      seq,
      generation: 0,
      model: "test-model",
      created_at: TIMESTAMP,
    })),
  };
}

/** What the fake API answers for the current test. */
const api = {
  requests: [] as RecordedRequest[],
  /** Frames for /chat/turnstream; null refuses the stream with a 404. */
  frames: null as Frame[] | null,
  /** chat.load rows before the message is sent (the snapshot). */
  rowsBefore: [] as Row[],
  /** chat.load rows after the message is sent. */
  rowsAfter: [] as Row[],
  sent: false,
};

let server: Server;

beforeAll(async () => {
  server = createServer((req, res) => {
    void (async () => {
      const url = new URL(req.url ?? "/", "http://localhost");
      const raw = await readBody(req);
      const body: unknown = raw ? JSON.parse(raw) : undefined;
      api.requests.push({
        path: url.pathname,
        query: url.searchParams,
        headers: req.headers,
        body,
      });
      const json = (status: number, value: unknown) => {
        res.writeHead(status, { "content-type": "application/json" });
        res.end(JSON.stringify(value));
      };

      switch (url.pathname) {
        case "/rpc/assistants.sendMessage": {
          api.sent = true;
          const chatId = (body as { chat_id?: string }).chat_id ?? NEW_CHAT_ID;
          json(200, { accepted: true, chat_id: chatId });
          return;
        }
        case "/rpc/chat.load": {
          const chatId = url.searchParams.get("id") ?? "";
          json(
            200,
            chatBody(chatId, api.sent ? api.rowsAfter : api.rowsBefore),
          );
          return;
        }
        case "/chat/turnstream": {
          if (!api.frames) {
            json(404, { message: "streaming disabled" });
            return;
          }
          res.writeHead(200, { "content-type": "text/event-stream" });
          // Flush headers first: the transport sends only once subscribed.
          res.flushHeaders();
          const frames = api.frames;
          // Frames are published after the send, as the runtime would.
          const publish = () => {
            if (!api.sent) {
              setTimeout(publish, 5);
              return;
            }
            res.end(
              frames.map((f) => `data: ${JSON.stringify(f)}\n\n`).join(""),
            );
          };
          publish();
          return;
        }
        default:
          json(404, { message: "not found" });
      }
    })();
  });
  state.serverURL = await listenOnLoopback(server);
});

afterAll(async () => {
  await closeServer(server);
});

beforeEach(() => {
  api.requests = [];
  api.frames = null;
  api.rowsBefore = [];
  api.rowsAfter = [];
  api.sent = false;
});

afterEach(() => {
  vi.restoreAllMocks();
});

/**
 * Sends one message through the transport with a real `Chat` and returns the
 * chat once the turn has settled, plus the chat id the transport adopted.
 */
async function runTurn(options: { existingChatId?: string }) {
  const adopted: string[] = [];
  const factory = createServerAssistantTransport({
    client: new GramCore({
      serverURL: state.serverURL,
      retryConfig: { strategy: "none" },
    }),
    assistantId: ASSISTANT_ID,
    projectSlug: PROJECT,
    getSessionToken: () => SESSION,
    pollIntervalMs: 20,
    pollTimeoutMs: 5_000,
  });
  const transport = factory({
    getChatId: () => options.existingChatId ?? null,
    adoptChatId: () => (chatId) => {
      adopted.push(chatId);
    },
  });

  let sends = 0;
  const counted: ChatTransport<UIMessage> = {
    sendMessages: (args) => {
      // A regression here re-sends without bound; stop rather than hang.
      if (++sends > 2) throw new Error("runaway resend");
      return transport.sendMessages(args);
    },
    reconnectToStream: (args) => transport.reconnectToStream(args),
  };
  const chat = new Chat<UIMessage>({
    transport: counted,
    sendAutomaticallyWhen: lastAssistantMessageIsCompleteWithToolCalls,
  });

  await chat.sendMessage({ text: WEATHER_PROMPT });
  // `sendAutomaticallyWhen` runs after the stream settles; give it a tick.
  await new Promise((resolve) => {
    setTimeout(resolve, 50);
  });

  return { chat, adopted, sends };
}

function assistantOf(chat: Chat<UIMessage>): UIMessage {
  const last = chat.messages.at(-1);
  expect(last?.role).toBe("assistant");
  return last!;
}

/** The assistant message's parts, reduced to what the user sees. */
type VisiblePart =
  | { text: string }
  | { tool: string; state: string; input: unknown; output: unknown };

function visibleParts(message: UIMessage): VisiblePart[] {
  return message.parts.flatMap((part): VisiblePart[] => {
    if (part.type === "text") return [{ text: part.text }];
    if (part.type.startsWith("tool-")) {
      const tool = part as {
        type: string;
        state: string;
        input?: unknown;
        output?: unknown;
      };
      return [
        {
          tool: tool.type,
          state: tool.state,
          input: tool.input,
          output: tool.output,
        },
      ];
    }
    return [];
  });
}

const EXPECTED_TOOL_TURN = [
  { text: INTRO },
  {
    tool: "tool-get_weather",
    state: "output-available",
    input: { city: "Paris" },
    output: TOOL_OUTPUT,
  },
  { text: ANSWER },
];

describe("Project Assistant turn", () => {
  it("streams a tool-using turn in a new conversation", async () => {
    api.frames = TOOL_TURN_FRAMES;

    const { chat, adopted, sends } = await runTurn({});

    expect(chat.status).toBe("ready");
    expect(chat.error).toBeUndefined();
    expect(sends).toBe(1);
    expect(adopted).toEqual([NEW_CHAT_ID]);
    expect(visibleParts(assistantOf(chat))).toEqual(EXPECTED_TOOL_TURN);

    // New conversation: send first, then replay the new chat's stream.
    expect(api.requests.map((r) => r.path)).toEqual([
      "/rpc/assistants.sendMessage",
      "/chat/turnstream",
    ]);
    const [send, stream] = api.requests;
    expect(send!.body).toMatchObject({
      assistant_id: ASSISTANT_ID,
      message: WEATHER_PROMPT,
    });
    expect(send!.headers["gram-project"]).toBe(PROJECT);
    expect(stream!.query.get("chat_id")).toBe(NEW_CHAT_ID);
    expect(stream!.query.get("after")).toBe("0");
    expect(stream!.headers["gram-session"]).toBe(SESSION);
  });

  it("subscribes before sending in an existing conversation", async () => {
    api.frames = TOOL_TURN_FRAMES;

    const { chat, adopted, sends } = await runTurn({
      existingChatId: EXISTING_CHAT_ID,
    });

    expect(chat.status).toBe("ready");
    expect(sends).toBe(1);
    expect(adopted).toEqual([]);
    expect(visibleParts(assistantOf(chat))).toEqual(EXPECTED_TOOL_TURN);
    expect(api.requests.map((r) => r.path)).toEqual([
      "/rpc/chat.load",
      "/chat/turnstream",
      "/rpc/assistants.sendMessage",
    ]);
    expect(api.requests[1]!.query.has("after")).toBe(false);
    expect(api.requests[2]!.body).toMatchObject({ chat_id: EXISTING_CHAT_ID });
  });

  it("falls back to polling chat.load when streaming is refused", async () => {
    api.frames = null;
    api.rowsAfter = TOOL_TURN_ROWS;
    const logged = vi.spyOn(console, "error").mockImplementation(() => {});

    const { chat, sends } = await runTurn({});

    expect(chat.status).toBe("ready");
    expect(chat.error).toBeUndefined();
    expect(sends).toBe(1);
    expect(visibleParts(assistantOf(chat))).toEqual(EXPECTED_TOOL_TURN);
    expect(logged).toHaveBeenCalledWith(
      "[turnstream] fell back to polling:",
      expect.any(Error),
    );
    expect(api.requests.some((r) => r.path === "/rpc/chat.load")).toBe(true);
  });
});
