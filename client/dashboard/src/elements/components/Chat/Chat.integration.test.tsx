import { Chat, GramElementsProvider } from "@/elements";
import { DEFAULT_MODEL } from "@/elements/lib/models";
import { CREDITS_EXHAUSTED_MESSAGE } from "@/elements/lib/streamErrorMessage";
import type { ElementsConfig } from "@/elements/types";
import {
  summarizeOpenRouterFixture,
  WEATHER_PROMPT,
  WEATHER_TOOL_RESULT,
} from "@/test/chatTestServer";
import {
  startChatTestServerProcess,
  type ChatTestServerProcess,
} from "@/test/chatTestServerProcess";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

// Renders the Playground chat (the real GramElementsProvider, assistant-ui
// runtime, and <Chat /> thread) and sends a message through the UI. The model
// call and MCP tool call go to the same test server the stream tests use, so
// this covers what those tests skip: the provider's transport wiring,
// @assistant-ui/react-ai-sdk, and rendering the result.

vi.mock("@/elements/lib/errorTracking", () => ({
  initErrorTracking: vi.fn(),
  trackError: vi.fn(),
}));

const SESSION = "test-session";

let server: ChatTestServerProcess;

beforeAll(async () => {
  server = await startChatTestServerProcess();
});

afterAll(async () => {
  await server?.close();
});

beforeEach(async () => {
  await server.reset();
});

afterEach(() => {
  cleanup();
});

/** The thread renders inside <Chat />'s (open) shadow root. */
async function chatShadowRoot(container: HTMLElement): Promise<HTMLElement> {
  let root: ShadowRoot | null | undefined;
  await waitFor(() => {
    root = container.querySelector(".gram-elements > div")?.shadowRoot;
    expect(root).toBeTruthy();
  });
  // Testing Library queries accept any node that can contain elements.
  return root as unknown as HTMLElement;
}

function playgroundConfig(): ElementsConfig {
  return {
    projectSlug: "proj",
    api: {
      url: server.baseUrl,
      session: SESSION,
      headers: { "X-Gram-Source": "playground" },
    },
    mcp: server.mcpUrl,
    variant: "standalone",
    model: { defaultModel: DEFAULT_MODEL, showModelPicker: false },
    history: { enabled: false },
    // Suggestions make a second model call after every turn.
    thread: { followUpSuggestions: false },
    composer: { requireMcpTools: true },
    welcome: {
      title: "Test Your MCP Server",
      subtitle: "",
      suggestions: [
        { title: "Weather", label: "In Paris", prompt: WEATHER_PROMPT },
      ],
    },
  };
}

/**
 * Renders the Playground chat and sends the weather question once MCP tools
 * have loaded. Returns the thread's shadow root.
 */
async function renderAndAskForWeather(): Promise<HTMLElement> {
  const user = userEvent.setup();
  // A fresh host client per test, as the dashboard provides one: the
  // provider's standalone client is module-level and would cache MCP tools
  // across tests.
  const { container } = render(
    <QueryClientProvider client={new QueryClient()}>
      <GramElementsProvider config={playgroundConfig()}>
        <Chat />
      </GramElementsProvider>
    </QueryClientProvider>,
  );
  const thread = await chatShadowRoot(container);

  // The composer only sends once MCP tools have loaded.
  await waitFor(async () => {
    const { mcpRequests } = await server.requests();
    expect(mcpRequests.some((r) => r.method === "tools/list")).toBe(true);
  });
  // Send through a welcome suggestion: happy-dom can't type into the
  // composer's contentEditable="plaintext-only" input.
  const suggestion = await waitFor(() => {
    const button = thread.querySelector<HTMLElement>(
      `[aria-label="${WEATHER_PROMPT}"]`,
    );
    expect(button).toBeTruthy();
    return button!;
  });
  await user.click(suggestion);
  return thread;
}

describe("Playground chat UI", () => {
  it("sends a message, runs the MCP tool, and renders the answer", async () => {
    await server.queueModelReplies(
      { kind: "stream", fixture: "tool-call-get-weather" },
      { kind: "stream", fixture: "text-weather-answer" },
    );

    const thread = await renderAndAskForWeather();

    const answer = summarizeOpenRouterFixture("text-weather-answer").text;
    await waitFor(
      () => {
        expect(thread.textContent).toContain(answer);
      },
      { timeout: 10_000 },
    );

    // The thread shows the question, the tool card, then the answer.
    const rendered = thread.textContent ?? "";
    const prompt = rendered.indexOf(WEATHER_PROMPT);
    const toolCard = rendered.indexOf("Get Weather");
    expect(prompt).toBeGreaterThanOrEqual(0);
    expect(toolCard).toBeGreaterThan(prompt);
    expect(rendered.indexOf(answer)).toBeGreaterThan(toolCard);

    const { modelRequests, mcpRequests } = await server.requests();

    // The model call went out with the provider's headers.
    expect(modelRequests).toHaveLength(2);
    const [first] = modelRequests;
    expect(first!.headers["x-gram-source"]).toBe("playground");
    expect(first!.headers["gram-chat-session"]).toBe(SESSION);
    const chatId = first!.headers["gram-chat-id"];
    expect(chatId).toEqual(expect.any(String));

    // The tool call reached the MCP server with the session and chat id.
    const toolCall = mcpRequests.find((r) => r.method === "tools/call");
    expect(toolCall?.headers["gram-chat-session"]).toBe(SESSION);
    expect(toolCall?.headers["gram-chat-id"]).toBe(chatId);

    // The tool result went back to the model.
    expect(JSON.stringify(modelRequests[1]!.body.messages)).toContain(
      WEATHER_TOOL_RESULT,
    );
  });

  it("shows the credits message when the gateway returns 402", async () => {
    await server.queueModelReplies({
      kind: "error",
      status: 402,
      body: {
        error: { message: "This request requires more credits", code: 402 },
      },
    });
    // The provider logs every stream error.
    const logged = vi.spyOn(console, "error").mockImplementation(() => {});

    const thread = await renderAndAskForWeather();

    await waitFor(() => {
      expect(
        thread.querySelector(".aui-message-error-message")?.textContent,
      ).toBe(CREDITS_EXHAUSTED_MESSAGE);
    });
    expect(logged).toHaveBeenCalledWith(
      "Stream error in onError callback:",
      expect.anything(),
    );
  });
});
