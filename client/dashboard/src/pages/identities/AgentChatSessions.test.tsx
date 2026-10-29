import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { AgentChatSessionsPanel } from "./AgentChatSessions";

const mocks = vi.hoisted(() => ({ chats: vi.fn(), canRead: vi.fn() }));
const from = new Date("2026-01-01T00:00:00Z");
const to = new Date("2026-01-08T00:00:00Z");
vi.mock("./useIdentityQueries", () => ({
  retryFailed: () => () => {},
  useIdentityWindow: () => ({ from, to }),
  useCanReadAgentChats: () => mocks.canRead(),
  useAgentChats: (...args: unknown[]) => mocks.chats(...args),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

const agent = {
  id: "agent_example",
  ownerUserId: "owner_example",
} as ManagedAgent;

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.canRead.mockReturnValue(true);
  mocks.chats.mockReturnValue({
    data: {
      chats: [
        {
          id: "chat_one",
          title: "Fix the build",
          numMessages: 4,
          lastMessageTimestamp: new Date("2026-01-07T00:00:00Z"),
        },
      ],
      total: 7,
    },
  });
});

it("lists the agent's sessions, each opening its transcript, and hands off to the filtered list", () => {
  render(
    <MemoryRouter initialEntries={["/agent?range=7d&tab=overview"]}>
      <AgentChatSessionsPanel agent={agent} limit={3} />
    </MemoryRouter>,
  );
  expect(mocks.chats).toHaveBeenCalledWith(agent, from, to, 3);
  expect(screen.getByText("Fix the build")).toBeTruthy();
  expect(screen.getByText("7 sessions this period")).toBeTruthy();

  const hrefs = screen
    .getAllByRole("link")
    .map((link) => link.getAttribute("href"));
  // The window carries over, and unrelated params on the agent page do not.
  expect(hrefs).toContain(
    "/org/projects/project/agent-sessions?range=7d&agentId=agent_example",
  );
  expect(hrefs).toContain(
    "/org/projects/project/agent-sessions?chatId=chat_one",
  );
});

it("explains the refusal instead of claiming the agent has no sessions", () => {
  mocks.canRead.mockReturnValue(false);
  mocks.chats.mockReturnValue({});
  render(
    <MemoryRouter>
      <AgentChatSessionsPanel agent={agent} limit={3} />
    </MemoryRouter>,
  );
  expect(screen.getByText(/needs the chat:read permission/)).toBeTruthy();
  expect(screen.queryByText(/No sessions from this agent/)).toBeNull();
});

it("shows a failed read as an error rather than no sessions", () => {
  mocks.chats.mockReturnValue({ isError: true, refetch: vi.fn() });
  render(
    <MemoryRouter>
      <AgentChatSessionsPanel agent={agent} limit={3} />
    </MemoryRouter>,
  );
  expect(screen.getByText("This panel could not be loaded.")).toBeTruthy();
  expect(screen.queryByText(/No sessions from this agent/)).toBeNull();
});

it.each(["/agent", "/agent?range=invalid"])(
  "carries the effective seven-day window from %s to the full session list",
  (entry) => {
    render(
      <MemoryRouter initialEntries={[entry]}>
        <AgentChatSessionsPanel agent={agent} limit={3} />
      </MemoryRouter>,
    );
    expect(
      screen.getByRole("link", { name: /Agent Sessions/ }).getAttribute("href"),
    ).toBe(
      "/org/projects/project/agent-sessions?range=7d&agentId=agent_example",
    );
  },
);
