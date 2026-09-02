import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ChatLogsTable } from "./ChatLogsTable";

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
  useListChatSessionLinks: () => ({ data: { links: [] } }),
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
    cleanup();
    vi.restoreAllMocks();
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
});
