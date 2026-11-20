import { readFileSync, writeFileSync } from "node:fs";
import {
  createServer,
  type IncomingMessage,
  type Server,
  type ServerResponse,
} from "node:http";
import path from "node:path";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { z } from "zod";
// With the extension: chatTestServerMain.ts runs this file under plain Node.
import { closeServer, listenOnLoopback, readBody } from "./loopbackServer.ts";

/**
 * A canned reply from the fake OpenRouter endpoint: either a recorded
 * server-sent event stream or an HTTP error.
 */
export type ModelReply =
  | { kind: "stream"; fixture: string }
  | { kind: "error"; status: number; body: unknown };

export type RecordedModelRequest = {
  headers: IncomingMessage["headers"];
  body: {
    model: string;
    stream: boolean;
    messages: Array<Record<string, unknown>>;
    tools?: Array<{
      type: string;
      function: { name: string; parameters: Record<string, unknown> };
    }>;
  };
};

const fixturesDir = path.join(import.meta.dirname, "fixtures", "openrouter");

/**
 * Record mode: instead of replaying fixtures, forward each queued stream
 * request to the real OpenRouter API and save the response as that fixture.
 * Needs OPENROUTER_DEV_KEY set to an inference key (not a management key).
 * Never set in CI. From client/dashboard:
 *
 *   RECORD_OPENROUTER_FIXTURES=1 mise exec -- aube exec vitest run src/elements/lib/chatStream.test.ts
 */
export const isRecordingFixtures =
  process.env["RECORD_OPENROUTER_FIXTURES"] === "1";

const OPENROUTER_CHAT_URL = "https://openrouter.ai/api/v1/chat/completions";

function fixturePath(name: string): string {
  return path.join(fixturesDir, `${name}.sse`);
}

function loadOpenRouterFixture(name: string): string {
  return readFileSync(fixturePath(name), "utf8");
}

export type FixtureSummary = {
  /** Concatenated assistant text. */
  text: string;
  /** Tool calls, with arguments reassembled from their streamed pieces. */
  toolCalls: Array<{ id: string; name: string; arguments: string }>;
};

type FixtureChunk = {
  choices?: Array<{
    delta?: {
      content?: string | null;
      tool_calls?: Array<{
        index: number;
        id?: string;
        function?: { name?: string; arguments?: string };
      }>;
    };
  }>;
};

/**
 * Reads what a fixture's stream says, so tests assert against the fixture
 * instead of hard-coding text and ids that change on every recording.
 */
export function summarizeOpenRouterFixture(name: string): FixtureSummary {
  let text = "";
  const calls = new Map<
    number,
    { id: string; name: string; arguments: string }
  >();
  for (const line of loadOpenRouterFixture(name).split("\n")) {
    if (!line.startsWith("data: ")) continue;
    const payload = line.slice("data: ".length).trim();
    if (payload === "[DONE]") continue;
    const chunk = JSON.parse(payload) as FixtureChunk;
    for (const choice of chunk.choices ?? []) {
      text += choice.delta?.content ?? "";
      for (const call of choice.delta?.tool_calls ?? []) {
        const entry = calls.get(call.index) ?? {
          id: "",
          name: "",
          arguments: "",
        };
        entry.id ||= call.id ?? "";
        entry.name ||= call.function?.name ?? "";
        entry.arguments += call.function?.arguments ?? "";
        calls.set(call.index, entry);
      }
    }
  }
  return { text, toolCalls: [...calls.values()] };
}

/**
 * A failed recording. The test server answers with OpenRouter's status so the
 * AI SDK retries only what it would retry in production: a 401 from a bad key
 * fails once instead of three times.
 */
class UpstreamError extends Error {
  readonly status: number;
  constructor(status: number, body: string) {
    super(`OpenRouter returned ${status}: ${body}`);
    this.status = status;
  }
}

/**
 * Sends the request the AI SDK built to the real OpenRouter API and saves the
 * raw response as the fixture. Only the response body is saved; the API key
 * travels in a request header and never reaches the file.
 */
async function recordFromOpenRouter(
  rawBody: string,
  fixture: string,
): Promise<string> {
  const key = process.env["OPENROUTER_DEV_KEY"];
  if (!key || key === "unset") {
    throw new Error("Set OPENROUTER_DEV_KEY to record OpenRouter fixtures.");
  }
  const upstream = await fetch(OPENROUTER_CHAT_URL, {
    method: "POST",
    headers: {
      authorization: `Bearer ${key}`,
      "content-type": "application/json",
    },
    body: rawBody,
  });
  const body = await upstream.text();
  if (!upstream.ok) {
    throw new UpstreamError(upstream.status, body);
  }
  const scrubbed = scrubRecording(body);
  writeFileSync(fixturePath(fixture), scrubbed);
  return scrubbed;
}

/**
 * Replaces the generation and tool call ids a recording carries with fixed
 * placeholders. Fixtures are committed to a public repository, and these ids
 * belong to whichever OpenRouter account recorded them.
 */
function scrubRecording(body: string): string {
  return body
    .replace(/gen-\d+-[A-Za-z0-9]+/g, "gen-recorded")
    .replace(/toolu_[A-Za-z0-9]+/g, "toolu_recorded")
    .replace(/call_[A-Za-z0-9]+/g, "call_recorded");
}

/** The question the weather fixtures were recorded with. */
export const WEATHER_PROMPT = "What's the weather in Paris?";

export const WEATHER_TOOL_RESULT = "18°C and sunny";

/**
 * A real MCP server (official SDK, Streamable HTTP, stateless) with one
 * `get_weather` tool. A new server handles each request, as the SDK's
 * stateless example does.
 */
function createWeatherMcpServer(): McpServer {
  const server = new McpServer({ name: "weather-test", version: "1.0.0" });
  server.registerTool(
    "get_weather",
    {
      description: "Get the current weather for a city.",
      inputSchema: { city: z.string().describe("City name") },
    },
    ({ city }) => ({
      content: [{ type: "text", text: `${WEATHER_TOOL_RESULT} in ${city}` }],
    }),
  );
  return server;
}

export type RecordedMcpRequest = {
  headers: IncomingMessage["headers"];
  /** JSON-RPC method, e.g. `tools/call`. */
  method: string | undefined;
};

async function handleMcp(
  req: IncomingMessage,
  res: ServerResponse,
  requests: RecordedMcpRequest[],
) {
  const body = req.method === "POST" ? JSON.parse(await readBody(req)) : null;
  requests.push({
    headers: req.headers,
    method: (body as { method?: string } | null)?.method,
  });
  const server = createWeatherMcpServer();
  const transport = new StreamableHTTPServerTransport({
    sessionIdGenerator: undefined,
  });
  res.on("close", () => {
    void transport.close();
    void server.close();
  });
  await server.connect(transport);
  await transport.handleRequest(req, res, body);
}

async function handleChatCompletions(
  req: IncomingMessage,
  res: ServerResponse,
  dequeueReply: () => ModelReply | undefined,
  requests: RecordedModelRequest[],
) {
  const rawBody = await readBody(req);
  requests.push({
    headers: req.headers,
    body: JSON.parse(rawBody) as RecordedModelRequest["body"],
  });
  const reply = dequeueReply();
  if (!reply) {
    res.writeHead(500, { "content-type": "application/json" });
    res.end(JSON.stringify({ error: { message: "no reply queued" } }));
    return;
  }
  if (reply.kind === "error") {
    res.writeHead(reply.status, { "content-type": "application/json" });
    res.end(JSON.stringify(reply.body));
    return;
  }
  const stream = isRecordingFixtures
    ? await recordFromOpenRouter(rawBody, reply.fixture)
    : loadOpenRouterFixture(reply.fixture);
  res.writeHead(200, {
    "content-type": "text/event-stream",
    "cache-control": "no-cache",
    connection: "keep-alive",
  });
  res.end(stream);
}

export type ChatTestServer = {
  baseUrl: string;
  mcpUrl: string;
  /** Requests the fake OpenRouter endpoint received, in order. */
  modelRequests: RecordedModelRequest[];
  /** Requests the MCP server received, in order. */
  mcpRequests: RecordedMcpRequest[];
  /** Queues replies for the next model requests, in order. */
  queueModelReplies: (...replies: ModelReply[]) => void;
  reset: () => void;
  close: () => Promise<void>;
};

/**
 * Starts one loopback HTTP server that answers `/mcp` with a real MCP server
 * and `/chat/completions` with queued OpenRouter replies, so a chat turn runs
 * through the real AI SDK, OpenRouter provider, and MCP client.
 */
export async function startChatTestServer(): Promise<ChatTestServer> {
  const modelRequests: RecordedModelRequest[] = [];
  const mcpRequests: RecordedMcpRequest[] = [];
  let replies: ModelReply[] = [];
  const reset = () => {
    modelRequests.length = 0;
    mcpRequests.length = 0;
    replies = [];
  };

  // Control routes for a server running in another process; see
  // chatTestServerProcess.ts.
  const handleControl = async (
    pathname: string,
    req: IncomingMessage,
    res: ServerResponse,
  ) => {
    const body = await readBody(req);
    if (pathname === "/__test/queue") {
      replies.push(...(JSON.parse(body) as ModelReply[]));
    } else if (pathname === "/__test/reset") {
      reset();
    } else if (pathname !== "/__test/state") {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ modelRequests, mcpRequests }));
  };

  const route = async (req: IncomingMessage, res: ServerResponse) => {
    const { pathname } = new URL(req.url ?? "/", "http://localhost");
    if (pathname.startsWith("/__test/")) {
      await handleControl(pathname, req, res);
    } else if (pathname === "/mcp") {
      await handleMcp(req, res, mcpRequests);
    } else if (pathname === "/chat/completions" && req.method === "POST") {
      await handleChatCompletions(
        req,
        res,
        () => replies.shift(),
        modelRequests,
      );
    } else {
      res.writeHead(404).end();
    }
  };

  const server: Server = createServer((req, res) => {
    // Component tests run in a DOM environment whose fetch applies CORS.
    res.setHeader("access-control-allow-origin", "*");
    res.setHeader("access-control-allow-headers", "*");
    res.setHeader("access-control-allow-methods", "GET, POST, DELETE");
    res.setHeader("access-control-expose-headers", "*");
    if (req.method === "OPTIONS") {
      res.writeHead(204).end();
      return;
    }
    route(req, res).catch((error: unknown) => {
      // A failed recording otherwise surfaces only as a masked stream error.
      if (isRecordingFixtures) console.error(error);
      if (!res.headersSent) {
        res.writeHead(error instanceof UpstreamError ? error.status : 500);
      }
      res.end(String(error));
    });
  });

  const baseUrl = await listenOnLoopback(server);

  return {
    baseUrl,
    mcpUrl: `${baseUrl}/mcp`,
    modelRequests,
    mcpRequests,
    queueModelReplies: (...next) => {
      replies.push(...next);
    },
    reset,
    close: () => closeServer(server),
  };
}
