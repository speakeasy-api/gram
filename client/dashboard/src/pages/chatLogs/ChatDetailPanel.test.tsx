import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type { Chat } from "@gram/client/models/components/chat.js";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ChatDetailSheet } from "./ChatDetailPanel";

const state = vi.hoisted(() => ({ chat: undefined as Chat | undefined }));

const messages: ChatMessage[] = [
  {
    id: "message-user",
    role: "user",
    content: "Summarize the open incidents",
    seq: 1,
    generation: 0,
    model: "",
    createdAt: new Date("2026-01-01T12:00:00Z"),
  },
  {
    id: "message-assistant",
    role: "assistant",
    content: "There are two open incidents.",
    seq: 2,
    generation: 0,
    model: "",
    createdAt: new Date("2026-01-01T12:00:05Z"),
  },
];

vi.mock("./useChatTranscript", () => ({
  useChatTranscript: () => ({
    chat: state.chat,
    messages,
    isLoading: false,
    isError: false,
    isForbidden: false,
    hasMoreBefore: false,
    hasMoreAfter: false,
    fetchOlder: vi.fn(),
    fetchNewer: vi.fn(),
    isFetchingOlder: false,
    isFetchingNewer: false,
    loadRest: vi.fn(),
    isLoadingRest: false,
  }),
}));

vi.mock("./useWindowedTranscript", () => ({
  useWindowedTranscript: () => ({
    chat: undefined,
    messages: [],
    gaps: new Set<number>(),
    hasMoreBefore: false,
    hasMoreAfter: false,
    loadBefore: vi.fn(),
    loadAfter: vi.fn(),
    loadAllBefore: vi.fn(),
    loadAllAfter: vi.fn(),
    loadGap: vi.fn(),
    loadAll: vi.fn(),
    loadingKey: null,
    isLoading: false,
    isError: false,
    isForbidden: false,
  }),
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: {
      members: [
        { id: "gram-user-1", email: "ada@example.com", name: "Ada Lovelace" },
      ],
    },
  }),
}));

vi.mock("@gram/client/react-query/searchLogs.js", () => ({
  useSearchLogsMutation: () => ({
    mutate: vi.fn(),
    data: undefined,
    isPending: false,
    error: null,
  }),
}));

vi.mock("@gram/client/react-query/riskListResults.js", () => ({
  useRiskListResults: () => ({ data: { results: [] } }),
}));

vi.mock("@gram/client/react-query/chatSetPinned.js", () => ({
  useChatSetPinnedMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("@gram/client/react-query/listChats.js", () => ({
  invalidateAllListChats: vi.fn(),
}));

vi.mock("@gram/client/react-query/listChatSessionLinks.js", () => ({
  useListChatSessionLinks: () => ({ data: { links: [] } }),
}));

vi.mock("@gram/client/react-query/summarizeChat.js", () => ({
  useSummarizeChatMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => false,
    hasAnyScope: () => false,
    hasAllScopes: () => false,
    isLoading: false,
    grants: [],
    error: null,
  }),
}));

vi.mock("@/contexts/Auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/contexts/Auth")>()),
  useIsPlatformAdmin: () => false,
  useSession: () => ({
    // Distinct from the chat owner so the owner resolves to the member name
    // instead of collapsing to "You".
    user: { id: "gram-user-viewer", email: "viewer@example.com" },
  }),
}));

vi.mock("@/components/observe/observeDeepLink", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/components/observe/observeDeepLink")
  >()),
  useCanViewToolLogs: () => false,
}));

// The agent is in the inventory the reader may read, so it links.
vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: () => ({ data: [{ id: "agent-1" }], isError: false }),
}));

vi.mock("@/components/ui/Tooltip", () => ({
  SimpleTooltip: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
  TooltipContent: () => null,
}));

// Layout has no viewport in happy-dom: make every virtual row visible.
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        key: index,
        start: index * 72,
      })),
    getTotalSize: () => count * 72,
    measureElement: vi.fn(),
    scrollToIndex: vi.fn(),
  }),
}));

vi.mock("@/pages/security/useDismissFinding", () => ({
  useDismissFinding: () => ({ dismiss: vi.fn() }),
}));

vi.mock("@/elements", async () => {
  const { ToolUI, ToolUIGroup } =
    await import("@/elements/components/ui/tool-ui");
  return { ToolUI, ToolUIGroup, MessageContent: () => null };
});

function makeChat(overrides: Partial<Chat>): Chat {
  return {
    id: "chat-1",
    title: "Triage incidents",
    contentParts: [],
    createdAt: new Date("2026-01-01T12:00:00Z"),
    updatedAt: new Date("2026-01-01T12:01:00Z"),
    lastMessageTimestamp: new Date("2026-01-01T12:01:00Z"),
    generation: 0,
    maxGeneration: 0,
    hasMoreAfter: false,
    hasMoreBefore: false,
    messages,
    numMessages: messages.length,
    // chat.load reports an empty owner rather than omitting it.
    userId: "",
    externalUserId: "",
    ...overrides,
  };
}

function renderPanel(chat: Chat) {
  state.chat = chat;
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <ChatDetailSheet
          chatId={chat.id}
          onClose={vi.fn<() => void>()}
          onDelete={vi.fn<(chatId: string) => void>()}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Opens the "Session details" popover and returns its content. */
function openSessionDetails(): HTMLElement {
  fireEvent.click(screen.getByRole("button", { name: /Details/ }));
  return screen.getByText("Session details").parentElement!;
}

/** The name on the user turn's header in the transcript. */
function userTurnName(): string | null {
  // Both turn headers style their name the same way, and the assistant's
  // reads "Assistant": the other one is the user's.
  const assistantName = screen.getByText("Assistant", { selector: "span" });
  const userName = Array.from(document.body.querySelectorAll("span")).find(
    (element) =>
      element !== assistantName &&
      element.className === assistantName.className,
  );
  return userName?.textContent ?? null;
}

function expectAgentLink(name: string) {
  const links = screen.getAllByRole("link", { name });
  expect(links.length).toBeGreaterThan(0);
  for (const link of links) {
    expect(link.getAttribute("href")).toContain(
      encodeURIComponent("agent:agent-1"),
    );
  }
}

describe("ChatDetailSheet session actor", () => {
  afterEach(() => {
    cleanup();
    state.chat = undefined;
  });

  it("shows the full chat id with a copy button at the top", () => {
    const writeText = vi.fn();
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    renderPanel(makeChat({ id: "chat_01HXQ1P84WV3S9J7Z52DKVE7NE" }));

    expect(screen.getByText("chat_01HXQ1P84WV3S9J7Z52DKVE7NE")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Copy chat ID" }));
    expect(writeText).toHaveBeenCalledWith("chat_01HXQ1P84WV3S9J7Z52DKVE7NE");
  });

  it("names the agent and no person on an agent session that reports nobody", () => {
    renderPanel(makeChat({ agentId: "agent-1", agentName: "Release bot" }));

    expectAgentLink("Release bot");
    expect(screen.queryByText("on behalf of")).toBeNull();

    const details = openSessionDetails();
    expect(within(details).getByText("Agent")).toBeTruthy();
    expect(within(details).queryByText("On behalf of")).toBeNull();
    expect(within(details).queryByText("User")).toBeNull();

    // The user turn carries a neutral label, not a person.
    expect(userTurnName()).toBe("User");
    expect(document.body.textContent).not.toMatch(/anonymous/i);
  });

  it("names the person behind an agent session that reports one", () => {
    renderPanel(
      makeChat({
        agentId: "agent-1",
        agentName: "Release bot",
        userId: "gram-user-1",
      }),
    );

    expectAgentLink("Release bot");
    expect(screen.getByText("on behalf of")).toBeTruthy();

    const details = openSessionDetails();
    expect(within(details).getByText("Agent")).toBeTruthy();
    expect(within(details).getByText("On behalf of")).toBeTruthy();
    expect(within(details).getByText("Ada Lovelace")).toBeTruthy();

    expect(userTurnName()).toBe("Ada Lovelace");
  });

  it("keeps a hosted assistant session acting for its person", () => {
    renderPanel(
      makeChat({
        assistantId: "assistant-1",
        assistantName: "Helper",
        assistantAgentId: "agent-1",
        userId: "gram-user-1",
      }),
    );

    expectAgentLink("Helper");
    expect(screen.getByText("on behalf of")).toBeTruthy();

    const details = openSessionDetails();
    expect(within(details).getByText("Agent")).toBeTruthy();
    expect(within(details).getByText("On behalf of")).toBeTruthy();
    expect(within(details).getByText("Ada Lovelace")).toBeTruthy();

    expect(userTurnName()).toBe("Ada Lovelace");
  });

  it("keeps a person's own session naming them", () => {
    renderPanel(makeChat({ userId: "gram-user-1" }));

    expect(screen.queryByText("on behalf of")).toBeNull();

    const details = openSessionDetails();
    expect(within(details).queryByText("Agent")).toBeNull();
    expect(within(details).getByText("User")).toBeTruthy();
    expect(within(details).getByText("Ada Lovelace")).toBeTruthy();

    expect(userTurnName()).toBe("Ada Lovelace");
  });
});
