import { ActiveChatTitle, GramElementsProvider } from "@/elements";
import { ChatIdContext } from "@/elements/contexts/ChatIdContext";
import type {
  ElementsConfig,
  ElementsTransportContext,
} from "@/elements/types";
import { useAui, useAuiState } from "@assistant-ui/react";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createUIMessageStream, type UIMessage } from "ai";
import { useContext } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Mounts the real Elements history runtime (useRemoteThreadListRuntime +
// useGramThreadListAdapter + useChatRuntime). These sequences are the ones
// @assistant-ui/core 0.3.33 and @assistant-ui/ai-sdk 0.0.17 change, and the
// other Elements integration tests never turn history on for real.

vi.mock("@/elements/hooks/useAuth", () => ({
  useAuth: () => ({
    isLoading: false,
    headers: {},
    ensureValidHeaders: async () => ({}),
  }),
}));
vi.mock("@/elements/hooks/useMCPTools", () => ({
  useMCPTools: () => ({
    data: {},
    mcpHeaders: {},
    isLoading: false,
    error: null,
  }),
}));
vi.mock("@/elements/lib/errorTracking", () => ({
  initErrorTracking: vi.fn(),
  trackError: vi.fn(),
}));

const API_URL = "https://elements.test";
const SAVED_CHAT_ID = "saved-chat";
const SERVER_CHAT_ID = "11111111-1111-4111-8111-111111111111";
const USER_TEXT = "What is the weather?";
const ASSISTANT_TEXT = "It is sunny.";
const SAVED_TITLE = "Saved chat";
const EDITED_TITLE = "Edited title";
// Same pause useGramThreadListAdapter waits before chat.generateTitle.
const TITLE_DELAY_MS = 2_000;
const HISTORY_DEPRECATION =
  "[assistant-ui] RemoteThreadListAdapter.unstable_Provider is deprecated and will be removed in a future minor release. Return the same adapters from unstable_useAdapters and drop unstable_Provider so useRemoteThreadListRuntime runs on the RemoteThreadList client.";

type RpcCall = {
  url: string;
  method: string;
  body: { id?: string; title?: string } | undefined;
};

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function savedChat(messages: { id: string; role: string; content: string }[]) {
  return {
    id: SAVED_CHAT_ID,
    title: SAVED_TITLE,
    messages: messages.map((message, index) => ({
      ...message,
      seq: index + 1,
      model: "",
      created_at: "2026-01-01T00:00:00Z",
    })),
    has_more_before: false,
  };
}

function requestUrl(input: RequestInfo | URL): URL {
  if (typeof input === "string") return new URL(input);
  if (input instanceof URL) return input;
  return new URL(input.url);
}

function installFetch(
  handler: (url: URL, call: RpcCall) => Response,
): RpcCall[] {
  const calls: RpcCall[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = requestUrl(input);
      let body: RpcCall["body"];
      if (typeof init?.body === "string" && init.body !== "") {
        body = JSON.parse(init.body) as RpcCall["body"];
      }
      const call: RpcCall = {
        url: url.toString(),
        method: init?.method ?? "GET",
        body,
      };
      calls.push(call);
      return handler(url, call);
    }),
  );
  return calls;
}

// Every limit=200 load counts, including a later before_seq page. That page is
// another read of the transcript, so a second one fails the once check.
function fullPageLoads(calls: readonly RpcCall[], chatId: string): RpcCall[] {
  return calls.filter((call) => {
    const url = new URL(call.url);
    return (
      url.pathname === "/rpc/chat.load" &&
      url.searchParams.get("id") === chatId &&
      url.searchParams.get("limit") === "200"
    );
  });
}

// The adapter's pause is for the server-side title job, not these tests.
function collapseTitleDelay() {
  const realSetTimeout = globalThis.setTimeout;
  vi.spyOn(globalThis, "setTimeout").mockImplementation(
    (handler, delay, ...args) =>
      realSetTimeout(
        handler,
        delay === TITLE_DELAY_MS ? 0 : delay,
        ...(args as []),
      ),
  );
}

function messageText(message: {
  content: readonly { type: string; text?: string }[];
}): string {
  return message.content
    .filter((part) => part.type === "text")
    .map((part) => part.text ?? "")
    .join("");
}

const seen: {
  aui: ReturnType<typeof useAui> | undefined;
} = { aui: undefined };

function HistoryProbe() {
  const aui = useAui();
  seen.aui = aui;
  const remoteId = useAuiState((state) => state.threadListItem.remoteId ?? "");
  const chatId = useContext(ChatIdContext)?.chatId ?? "";
  const messages = useAuiState((state) => state.thread.messages);
  return (
    <>
      <output aria-label="remote id">{remoteId}</output>
      <output aria-label="chat id">{chatId}</output>
      <output aria-label="messages">
        {messages.map(messageText).join("\n")}
      </output>
      <ActiveChatTitle />
    </>
  );
}

function historyConfig(
  history: NonNullable<ElementsConfig["history"]>,
  transport?: ElementsConfig["transport"],
): ElementsConfig {
  return {
    projectSlug: "history-test",
    plugins: [],
    api: { url: API_URL },
    composer: { attachments: false },
    thread: { followUpSuggestions: false },
    history,
    ...(transport ? { transport } : {}),
  };
}

beforeEach(() => {
  seen.aui = undefined;
  vi.spyOn(console, "warn").mockImplementation((message: unknown) => {
    expect(message).toBe(HISTORY_DEPRECATION);
  });
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Elements history runtime", () => {
  it("loads a saved chat once across a rerender", async () => {
    const calls = installFetch((url) => {
      if (url.pathname === "/rpc/chat.list") {
        return json({
          chats: [
            {
              id: SAVED_CHAT_ID,
              title: SAVED_TITLE,
              created_at: "2026-01-01T00:00:00Z",
            },
          ],
        });
      }
      if (url.pathname === "/rpc/chat.load") {
        return json(
          savedChat([
            { id: "m-user", role: "user", content: USER_TEXT },
            { id: "m-assistant", role: "assistant", content: ASSISTANT_TEXT },
          ]),
        );
      }
      throw new Error(`Unexpected request: ${url.pathname}`);
    });
    const config = historyConfig({
      enabled: true,
      initialThreadId: SAVED_CHAT_ID,
    });

    function Host() {
      return (
        <GramElementsProvider config={config}>
          <HistoryProbe />
        </GramElementsProvider>
      );
    }

    const { rerender } = render(<Host />);

    await waitFor(() => {
      expect(screen.getByLabelText("messages").textContent).toContain(
        USER_TEXT,
      );
      expect(screen.getByLabelText("messages").textContent).toContain(
        ASSISTANT_TEXT,
      );
    });
    expect(screen.getByLabelText("remote id").textContent).toBe(SAVED_CHAT_ID);
    expect(fullPageLoads(calls, SAVED_CHAT_ID)).toHaveLength(1);

    // The saved-chat switch is a 100ms timeout. Advance past it so a second
    // switch on this rerender would load again before the assertion.
    vi.useFakeTimers();
    rerender(<Host />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(200);
    });

    expect(screen.getByLabelText("messages").textContent).toContain(USER_TEXT);
    expect(fullPageLoads(calls, SAVED_CHAT_ID)).toHaveLength(1);
  });

  it("adopts the server-minted id on the first send without client minting", async () => {
    collapseTitleDelay();
    const randomUUID = vi.spyOn(crypto, "randomUUID");
    const calls = installFetch((url) => {
      if (url.pathname === "/rpc/chat.list") return json({ chats: [] });
      if (url.pathname === "/rpc/chat.load") {
        return json({
          id: SERVER_CHAT_ID,
          title: "New Chat",
          messages: [],
          has_more_before: false,
        });
      }
      if (url.pathname === "/rpc/chat.generateTitle") {
        return json({ title: "Hello" });
      }
      throw new Error(`Unexpected request: ${url.pathname}`);
    });

    function serverMintingTransport(ctx: ElementsTransportContext) {
      return {
        async sendMessages() {
          const adopt = ctx.adoptChatId();
          adopt(SERVER_CHAT_ID);
          return createUIMessageStream<UIMessage>({
            execute: ({ writer }) => {
              writer.write({ type: "text-start", id: "reply" });
              writer.write({
                type: "text-delta",
                id: "reply",
                delta: "Server reply",
              });
              writer.write({ type: "text-end", id: "reply" });
            },
          });
        },
        async reconnectToStream() {
          return null;
        },
      };
    }

    render(
      <GramElementsProvider
        config={historyConfig(
          { enabled: true, deferThreadIdMinting: true },
          serverMintingTransport,
        )}
      >
        <HistoryProbe />
      </GramElementsProvider>,
    );

    await waitFor(() => {
      expect(calls.some((call) => call.url.includes("/rpc/chat.list"))).toBe(
        true,
      );
      expect(seen.aui).toBeDefined();
    });

    seen.aui!.thread().append("Hello");

    await waitFor(() => {
      expect(screen.getByLabelText("remote id").textContent).toBe(
        SERVER_CHAT_ID,
      );
      expect(screen.getByLabelText("chat id").textContent).toBe(SERVER_CHAT_ID);
      expect(screen.getByLabelText("messages").textContent).toContain(
        "Server reply",
      );
    });
    // The title fetch is queued after the send. Wait for it while the stub is
    // still installed. The two-second pause is collapsed above.
    await waitFor(() => {
      expect(
        calls.some((call) => call.url.includes("/rpc/chat.generateTitle")),
      ).toBe(true);
    });
    expect(randomUUID).not.toHaveBeenCalled();
  });

  it("keeps a manual title when chat.generateTitle fails", async () => {
    collapseTitleDelay();
    const user = userEvent.setup();
    const calls = installFetch((url, call) => {
      if (url.pathname === "/rpc/chat.list") {
        return json({
          chats: [
            {
              id: SAVED_CHAT_ID,
              title: SAVED_TITLE,
              created_at: "2026-01-01T00:00:00Z",
            },
          ],
        });
      }
      if (url.pathname === "/rpc/chat.load") {
        return json(
          savedChat([
            { id: "m-user", role: "user", content: USER_TEXT },
            { id: "m-assistant", role: "assistant", content: ASSISTANT_TEXT },
          ]),
        );
      }
      if (url.pathname === "/rpc/chat.generateTitle") {
        if (call.body?.title) return json({ title: call.body.title });
        return json({ error: "title unavailable" }, 500);
      }
      throw new Error(`Unexpected request: ${url.pathname}`);
    });

    render(
      <GramElementsProvider
        config={historyConfig({
          enabled: true,
          initialThreadId: SAVED_CHAT_ID,
        })}
      >
        <HistoryProbe />
      </GramElementsProvider>,
    );

    await waitFor(() => {
      expect(screen.getByRole("button", { name: SAVED_TITLE })).toBeTruthy();
    });

    await user.click(
      screen.getByRole("button", { name: "Rename conversation" }),
    );
    const input = screen.getByRole("textbox", { name: "Chat title" });
    await user.clear(input);
    await user.type(input, EDITED_TITLE);
    await user.keyboard("{Enter}");

    await waitFor(() => {
      expect(screen.getByRole("button", { name: EDITED_TITLE })).toBeTruthy();
      expect(
        calls.some(
          (call) =>
            call.url.includes("/rpc/chat.generateTitle") &&
            call.body?.title === EDITED_TITLE,
        ),
      ).toBe(true);
    });

    const untitledTitleCalls = () =>
      calls.filter(
        (call) =>
          call.url.includes("/rpc/chat.generateTitle") &&
          call.body?.title === undefined,
      );
    const untitledBefore = untitledTitleCalls().length;
    seen.aui!.threadListItem().generateTitle();

    await waitFor(() => {
      expect(untitledTitleCalls().length).toBe(untitledBefore + 1);
    });
    await act(async () => {
      await new Promise((resolve) => {
        setTimeout(resolve, 0);
      });
    });
    expect(screen.getByRole("button", { name: EDITED_TITLE })).toBeTruthy();
  });
});
