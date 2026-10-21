import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ChatDetailSheet } from "./ChatDetailPanel";
import { ChatLogsTable } from "./ChatLogsTable";

const restricted = vi.hoisted(() => ({ value: false }));
const lineage = vi.hoisted(() => ({ links: [] as unknown[] }));
const overviewQuery = vi.hoisted(() => vi.fn());
vi.mock("@gram/client/react-query/loadChatOverview.js", () => ({
  buildLoadChatOverviewQuery: overviewQuery,
}));
vi.mock("@/hooks/useSensitiveContentRestricted", () => ({
  useSensitiveContentRestricted: () => restricted.value,
}));

vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));

vi.mock("@/components/ui/Button", () => ({
  Button: ({
    children,
    onClick,
  }: {
    children: ReactNode;
    onClick?: () => void;
  }) => <button onClick={onClick}>{children}</button>,
}));

vi.mock("@/components/ui/Icon", () => ({
  Icon: ({ name }: { name: string }) => <span>{name}</span>,
}));

vi.mock("@/components/ui/Tooltip", () => ({
  SimpleTooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: {
      members: [
        {
          id: "gram-user-1",
          email: "ada@example.com",
          name: "Ada Lovelace",
        },
      ],
    },
  }),
}));

vi.mock("@gram/client/react-query/listChatSessionLinks.js", () => ({
  // No lineage edges by default: rows render without the icon cluster, and
  // the hook never reaches useGramContext (no SDK provider in these tests).
  useListChatSessionLinks: () => ({ data: lineage }),
}));

vi.mock("@gram/client/react-query/chatSetPinned.js", () => ({
  useChatSetPinnedMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/listChats.js", () => ({
  invalidateAllListChats: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    hasAllScopes: () => true,
    isLoading: false,
    grants: [],
    error: null,
  }),
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({
    // Distinct from the chat owner so ChatOwnerLabel resolves the member name
    // instead of collapsing to "You".
    user: {
      id: "gram-user-viewer",
      email: "viewer@example.com",
      name: "Viewer",
    },
  }),
}));

function makeChat(id: string): ChatOverview {
  const createdAt = new Date("2026-01-01T12:00:00Z");

  return {
    createdAt,
    id,
    lastMessageTimestamp: new Date("2026-01-01T12:03:00Z"),
    numMessages: 4,
    title: "Investigate session",
    updatedAt: new Date("2026-01-01T12:03:00Z"),
  };
}

function renderTable(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    // The owner cell links to the person's identity page.
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ChatLogsTable", () => {
  afterEach(() => {
    restricted.value = false;
    lineage.links = [];
    cleanup();
    vi.restoreAllMocks();
  });

  it("does not prefetch protected ancestor overviews and keeps safe lineage metadata", () => {
    restricted.value = true;
    lineage.links = [
      {
        kind: "subagent",
        parentCaptured: true,
        parentChatId: "parent",
        childChatId: "child",
      },
    ];
    renderTable(
      <ChatLogsTable
        chats={[makeChat("child")]}
        onSelectChat={vi.fn<(chat: ChatOverview) => void>()}
        onDeleteChat={vi.fn<(chatId: string) => void>()}
        isLoading={false}
        error={null}
      />,
    );
    expect(overviewQuery).not.toHaveBeenCalled();
    expect(screen.getByText("Session · child")).toBeTruthy();
  });

  it("removes an already selected cached session title when access becomes restricted", () => {
    const chat = makeChat("session-123456");
    const table = (
      <ChatLogsTable
        chats={[chat]}
        selectedChatId={chat.id}
        onSelectChat={vi.fn<(chat: ChatOverview) => void>()}
        onDeleteChat={vi.fn<(chatId: string) => void>()}
        isLoading={false}
        error={null}
      />
    );
    const view = renderTable(table);
    expect(screen.getByText(chat.title)).toBeTruthy();
    restricted.value = true;
    view.rerender(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>{table}</MemoryRouter>
      </QueryClientProvider>,
    );
    expect(screen.queryByText(chat.title)).toBeNull();
    expect(screen.queryByRole("button", { name: /Open session/ })).toBeNull();
  });

  it("does not mount a deep-linked transcript sheet during restricted access", () => {
    restricted.value = true;
    renderTable(
      <ChatDetailSheet
        chatId="cached-session"
        onClose={vi.fn<() => void>()}
        onDelete={vi.fn<(chatId: string) => void>()}
      />,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(overviewQuery).not.toHaveBeenCalled();
  });

  it("protects cached titles and removes session actions while preserving metadata", () => {
    restricted.value = true;
    const onSelect = vi.fn<(chat: ChatOverview) => void>();
    const onDelete = vi.fn<(chatId: string) => void>();
    renderTable(
      <ChatLogsTable
        chats={[makeChat("session-123456")]}
        selectedChatId="session-123456"
        onSelectChat={onSelect}
        onDeleteChat={onDelete}
        isLoading={false}
        error={null}
      />,
    );
    expect(screen.queryByText("Investigate session")).toBeNull();
    expect(screen.getByText("Session · session-")).toBeTruthy();
    expect(screen.getByTitle("Copy Chat ID").closest("h3")).toBeTruthy();
    expect(screen.queryByText("session-", { exact: true })).toBeNull();
    expect(screen.getByText(/^Created Jan 1/)).toBeTruthy();
    expect(
      screen.queryByRole("button", {
        name: /Open session|Pin session|Delete chat/,
      }),
    ).toBeNull();
    fireEvent.contextMenu(screen.getByText("Session · session-"));
    expect(screen.queryByText("Delete chat")).toBeNull();
    expect(onSelect).not.toHaveBeenCalled();
    expect(onDelete).not.toHaveBeenCalled();
  });

  it("copies the raw chat id without a label prefix", () => {
    const writeText = vi.fn();
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    const chatId = "chat_01HXQ1P84WV3S9J7Z52DKVE7NE";

    renderTable(
      <ChatLogsTable
        chats={[makeChat(chatId)]}
        onDeleteChat={() => {
          /* test stub */
        }}
        onSelectChat={() => {
          /* test stub */
        }}
        isLoading={false}
        error={null}
      />,
    );

    fireEvent.click(screen.getByTitle("Copy Chat ID"));

    expect(writeText).toHaveBeenCalledWith(chatId);
  });

  it("shows created and last activity timestamps", () => {
    renderTable(
      <ChatLogsTable
        chats={[makeChat("chat_01HXQ1P84WV3S9J7Z52DKVE7NE")]}
        onDeleteChat={() => {
          /* test stub */
        }}
        onSelectChat={() => {
          /* test stub */
        }}
        isLoading={false}
        error={null}
      />,
    );

    expect(screen.getByText(/^Created Jan 1, \d{2}:00$/)).toBeTruthy();
    expect(screen.getByText(/^Last activity Jan 1, \d{2}:03$/)).toBeTruthy();
  });

  it("shows the normalized product surface for a session source", () => {
    renderTable(
      <ChatLogsTable
        chats={[
          {
            ...makeChat("chat_01HXQ1P84WV3S9J7Z52DKVE7NE"),
            source: "claude",
          },
        ]}
        onDeleteChat={() => {
          /* test stub */
        }}
        onSelectChat={() => {
          /* test stub */
        }}
        isLoading={false}
        error={null}
      />,
    );

    expect(screen.getByText("Claude Chat Desktop")).toBeTruthy();
    expect(screen.queryByText("claude")).toBeNull();
  });

  it("shows the originating client for a session routed through LiteLLM", () => {
    renderTable(
      <ChatLogsTable
        chats={[
          {
            ...makeChat("chat_01HXQ1P84WV3S9J7Z52DKVE7NE"),
            source: "litellm",
            originatingClient: "claude-code",
          },
        ]}
        onDeleteChat={() => {
          /* test stub */
        }}
        onSelectChat={() => {
          /* test stub */
        }}
        isLoading={false}
        error={null}
      />,
    );

    expect(screen.getByText("Claude Code via LiteLLM")).toBeTruthy();
  });

  it("shows plain LiteLLM when the originating client is unknown", () => {
    renderTable(
      <ChatLogsTable
        chats={[
          {
            ...makeChat("chat_01HXQ1P84WV3S9J7Z52DKVE7NE"),
            source: "litellm",
          },
        ]}
        onDeleteChat={() => {
          /* test stub */
        }}
        onSelectChat={() => {
          /* test stub */
        }}
        isLoading={false}
        error={null}
      />,
    );

    expect(screen.getByText("LiteLLM")).toBeTruthy();
    expect(screen.queryByText(/via LiteLLM/)).toBeNull();
  });

  it("shows a resolved member name for a compliance chat", () => {
    renderTable(
      <ChatLogsTable
        chats={[
          {
            ...makeChat("chat_01HXQ1P84WV3S9J7Z52DKVE7NE"),
            userId: "gram-user-1",
            externalUserId: "user_01HXXXXXXXXXXXXXXXXXXXXXXX",
          },
        ]}
        onDeleteChat={() => {
          /* test stub */
        }}
        onSelectChat={() => {
          /* test stub */
        }}
        isLoading={false}
        error={null}
      />,
    );

    expect(screen.getByText("Ada Lovelace")).toBeTruthy();
    expect(screen.queryByText("user_01HXXXXXXXXXXXXXXXXXXXXXXX")).toBeNull();
  });
  it("links an agent-backed assistant session to its agent and names who it acted for", () => {
    renderTable(
      <ChatLogsTable
        chats={[
          {
            ...makeChat("chat_01HXQ1P84WV3S9J7Z52DKVE7NE"),
            userId: "gram-user-1",
            assistantId: "assistant-1",
            assistantName: "Helper",
            assistantAgentId: "agent-1",
          },
        ]}
        onDeleteChat={() => {
          /* test stub */
        }}
        onSelectChat={() => {
          /* test stub */
        }}
        isLoading={false}
        error={null}
      />,
    );

    const agentLink = screen.getByRole("link", { name: /Helper/ });
    expect(agentLink.getAttribute("href")).toContain(
      encodeURIComponent("agent:agent-1"),
    );
    expect(screen.getByText("for")).toBeTruthy();
    const delegateLink = screen.getByRole("link", { name: "Ada Lovelace" });
    expect(delegateLink.getAttribute("href")).toContain(
      encodeURIComponent("user:gram-user-1"),
    );
  });

  it("links an assistant session without an agent identity to the assistant", () => {
    renderTable(
      <ChatLogsTable
        chats={[
          {
            ...makeChat("chat_01HXQ1P84WV3S9J7Z52DKVE7NE"),
            assistantId: "assistant-1",
            assistantName: "Helper",
          },
        ]}
        onDeleteChat={() => {
          /* test stub */
        }}
        onSelectChat={() => {
          /* test stub */
        }}
        isLoading={false}
        error={null}
      />,
    );

    const assistantLink = screen.getByRole("link", { name: /Helper/ });
    expect(assistantLink.getAttribute("href")).toContain(
      "/assistants/assistant-1",
    );
    expect(screen.queryByText("for")).toBeNull();
  });
});
