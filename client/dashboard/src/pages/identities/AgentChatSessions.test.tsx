import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation, useNavigate } from "react-router";
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
vi.mock("@gram/client/react-query/chatDelete.js", () => ({
  useChatDeleteMutation: () => ({ mutate: vi.fn() }),
}));
// The real drawer loads the transcript; this one says which session is open
// and closes the way the real one does.
vi.mock("@/pages/chatLogs/ChatDetailPanel", () => ({
  ChatDetailSheet: ({
    chatId,
    onClose,
  }: {
    chatId: string | null;
    onClose: () => void;
  }) =>
    chatId ? (
      <div role="dialog" aria-label="Session">
        Open session {chatId}
        <button onClick={onClose}>Close session</button>
      </div>
    ) : null,
}));

/** The address the panel's page is on, so a test can see what a click did. */
function Address(): JSX.Element {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <output aria-label="address">{`${location.pathname}${location.search}`}</output>
      <button onClick={() => void navigate(-1)}>Back</button>
      <button onClick={() => void navigate(1)}>Forward</button>
    </>
  );
}

function renderPanel(entry: string) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[entry]}>
        <AgentChatSessionsPanel agent={agent} limit={3} />
        <Address />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

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
  renderPanel("/agent?range=7d&tab=overview");
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
  // A session opens on the agent's own page, keeping its params.
  expect(hrefs).toContain("/agent?range=7d&tab=overview&chatId=chat_one");
});

it("opens a session in a drawer on the agent's page, and back and forward close and reopen it", () => {
  renderPanel("/agent?range=7d");
  const address = () => screen.getByLabelText("address").textContent;
  expect(screen.queryByRole("dialog")).toBeNull();

  fireEvent.click(screen.getByRole("link", { name: /Fix the build/ }));
  expect(address()).toBe("/agent?range=7d&chatId=chat_one");
  expect(screen.getByRole("dialog").textContent).toContain("chat_one");
  // The agent's list stays behind the drawer.
  expect(screen.getByText("Fix the build")).toBeTruthy();

  fireEvent.click(screen.getByRole("button", { name: "Close session" }));
  expect(address()).toBe("/agent?range=7d");
  expect(screen.queryByRole("dialog")).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: "Back" }));
  expect(address()).toBe("/agent?range=7d&chatId=chat_one");
  expect(screen.getByRole("dialog")).toBeTruthy();

  fireEvent.click(screen.getByRole("button", { name: "Back" }));
  expect(address()).toBe("/agent?range=7d");
  expect(screen.queryByRole("dialog")).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: "Forward" }));
  expect(screen.getByRole("dialog")).toBeTruthy();
});

it("opens the session a shared link names", () => {
  renderPanel("/agent?chatId=chat_shared");
  expect(screen.getByRole("dialog").textContent).toContain("chat_shared");
});

it("explains the refusal instead of claiming the agent has no sessions", () => {
  mocks.canRead.mockReturnValue(false);
  mocks.chats.mockReturnValue({});
  renderPanel("/agent");
  expect(screen.getByText(/needs the chat:read permission/)).toBeTruthy();
  expect(screen.queryByText(/No sessions from this agent/)).toBeNull();
});

it("shows a failed read as an error rather than no sessions", () => {
  mocks.chats.mockReturnValue({ isError: true, refetch: vi.fn() });
  renderPanel("/agent");
  expect(screen.getByText("This panel could not be loaded.")).toBeTruthy();
  expect(screen.queryByText(/No sessions from this agent/)).toBeNull();
});

it.each(["/agent", "/agent?range=invalid"])(
  "carries the effective seven-day window from %s to the full session list",
  (entry) => {
    renderPanel(entry);
    expect(
      screen.getByRole("link", { name: /Agent Sessions/ }).getAttribute("href"),
    ).toBe(
      "/org/projects/project/agent-sessions?range=7d&agentId=agent_example",
    );
  },
);
