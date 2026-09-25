import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, useLocation } from "react-router";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import Fleet from "./Fleet";
import type { FleetRow } from "./fleet-model";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";

const mocks = vi.hoisted(() => ({
  query: vi.fn(),
  listArgs: vi.fn(),
  badges: vi.fn(),
  agents: [] as unknown[],
  assistants: [] as unknown[],
  projectId: "project",
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org" }),
  useProject: () => ({
    id: mocks.projectId,
    slug: mocks.projectId,
    name: "Project",
  }),
  useSession: () => ({ user: { id: "user" }, session: "session" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ killswitches: { batchAgentBadges: mocks.badges } }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "enabled" }),
}));
vi.mock("@/hooks/useKillswitchAccess", () => ({
  useKillswitchAccess: () => ({ canAccess: true }),
}));
vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: () => ({ data: mocks.agents, isSuccess: true }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ agents: { href: () => "/agents" } }),
}));
vi.mock("@gram/client/react-query/assistantsList.js", () => ({
  useAssistantsList: () => ({ data: { assistants: mocks.assistants } }),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));
vi.mock("@gram/client/react-query/listChats.js", async () => {
  const { useQuery } = await import("@tanstack/react-query");
  return {
    useListChats: (
      request: { from: Date; offset: number; search?: string },
      _: unknown,
      options: object,
    ) => {
      mocks.listArgs(request);
      return useQuery({
        queryKey: ["chats", request],
        queryFn: () =>
          mocks.query(request) as Promise<{
            chats: ChatOverview[];
            total: number;
          }>,
        ...options,
      });
    },
  };
});
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("@/components/page-templates", () => ({
  ResourceListPage: ({ children }: { children: ReactNode }) => (
    <main>{children}</main>
  ),
}));
vi.mock("@/components/page-layout", async () => {
  const { Toolbar } = await import("@/components/ui/Toolbar");
  return { Page: { Toolbar } };
});
vi.mock("./FleetCollection", () => ({
  FleetCollection: ({
    rows,
    onSelect,
  }: {
    rows: FleetRow[];
    onSelect: (id: string) => void;
  }) => (
    <section aria-label="Collection">
      {rows.map((row) => (
        <button
          key={row.id}
          data-fleet-row={row.id}
          onClick={() => onSelect(row.id)}
        >
          {row.title}
        </button>
      ))}
    </section>
  ),
}));
vi.mock("./FleetInspector", () => ({
  FleetInspector: ({ row }: { row: FleetRow }) => (
    <aside className="fleet-inspector" aria-label="Inspector">
      {row.title}
    </aside>
  ),
}));
vi.mock("./AgentRestrictions", () => ({
  AgentRestrictions: () => (
    <section aria-label="Restrictions">
      <button>Release restriction</button>
    </section>
  ),
}));

const NOW = new Date("2026-09-01T12:00:00Z");
function capture(timestamp = NOW): ChatOverview {
  return {
    id: "capture",
    title: "Captured task",
    createdAt: NOW,
    updatedAt: NOW,
    lastMessageTimestamp: timestamp,
    numMessages: 2,
  };
}
function Location() {
  return <output aria-label="URL">{useLocation().search}</output>;
}
function mount(search = "") {
  window.history.replaceState(null, "", `/fleet${search}`);
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  const content = () => (
    <QueryClientProvider client={client}>
      <BrowserRouter>
        <TooltipProvider>
          <Fleet />
          <Location />
        </TooltipProvider>
      </BrowserRouter>
    </QueryClientProvider>
  );
  const rendered = render(content());
  return { client, refresh: () => rendered.rerender(content()) };
}
async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1);
  });
}
beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  mocks.query.mockResolvedValue({ chats: [capture()], total: 120 });
  mocks.badges.mockResolvedValue({ badges: [] });
  mocks.agents = [];
  mocks.assistants = [];
  mocks.projectId = "project";
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

it("keeps the selected inspector and page during a rolling fetch, then resets an exhausted page", async () => {
  mount("?offset=50&selected=session%3Acapture");
  await settle();
  expect(mocks.listArgs.mock.calls[0]?.[0]).toMatchObject({
    offset: 50,
    from: new Date("2026-08-31T12:00:00Z"),
  });
  expect(screen.getByRole("complementary", { name: "Inspector" })).toBeTruthy();
  let finish: (value: {
    chats: ChatOverview[];
    total: number;
  }) => void = () => {};
  mocks.query.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  await act(async () => {
    await vi.advanceTimersByTimeAsync(30_000);
  });
  expect(screen.getByRole("complementary", { name: "Inspector" })).toBeTruthy();
  expect(screen.getByLabelText("URL").textContent).toContain("offset=50");
  await act(async () => {
    finish({ chats: [], total: 20 });
  });
  await settle();
  expect(screen.getByLabelText("URL").textContent).not.toContain("offset=");
  expect(screen.getByLabelText("URL").textContent).toContain(
    "selected=session%3Acapture",
  );
});

it("does not reuse captures across a new search and keeps the selected URL for return", async () => {
  mount("?selected=session%3Acapture");
  await settle();
  mocks.query.mockImplementationOnce(() => new Promise(() => {}));
  fireEvent.change(screen.getByPlaceholderText("Search Fleet"), {
    target: { value: "other" },
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(300);
  });
  expect(screen.queryByRole("complementary", { name: "Inspector" })).toBeNull();
  expect(screen.getByLabelText("URL").textContent).toContain(
    "selected=session%3Acapture",
  );
  expect(screen.getByLabelText("URL").textContent).toContain("q=other");
});

it("filters stale placeholder rows on each tick without taking focus from search", async () => {
  mocks.query.mockResolvedValueOnce({
    chats: [capture(new Date(NOW.getTime() - 24 * 60 * 60 * 1000 + 10_000))],
    total: 1,
  });
  mount("?selected=session%3Acapture");
  await settle();
  expect(screen.getByRole("complementary", { name: "Inspector" })).toBeTruthy();
  const search = screen.getByPlaceholderText("Search Fleet");
  search.focus();
  mocks.query.mockImplementationOnce(() => new Promise(() => {}));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(30_000);
  });
  expect(screen.queryByRole("complementary", { name: "Inspector" })).toBeNull();
  expect(
    screen.getByText(/selected item is outside the last 24 hours/),
  ).toBeTruthy();
  expect(document.activeElement).toBe(search);
  expect(screen.getByLabelText("URL").textContent).toContain(
    "selected=session%3Acapture",
  );
});

it("keeps restriction recovery available for agents outside the activity window", async () => {
  mocks.agents = [
    {
      id: "old",
      name: "Old agent",
      ownerUserId: "user",
      lifecycle: "active",
      createdAt: NOW,
      updatedAt: NOW,
      lastCredentialUsedAt: new Date("2026-08-01"),
      permissions: { read: true, authorize: true },
    },
  ];
  mount("?tab=restrictions");
  await settle();
  expect(
    screen.getByRole("button", { name: "Release restriction" }),
  ).toBeTruthy();
  expect(mocks.badges).not.toHaveBeenCalled();
});

it("keeps an explicitly observed assistant when searching its name rather than its chat title", async () => {
  mocks.assistants = [
    {
      id: "assistant",
      name: "Release helper",
      projectId: "project",
      createdAt: NOW,
      updatedAt: NOW,
    },
  ];
  mocks.query.mockResolvedValueOnce({
    chats: [{ ...capture(), assistantId: "assistant" }],
    total: 120,
  });
  mount();
  await settle();
  expect(screen.getByRole("button", { name: "Release helper" })).toBeTruthy();
  mocks.query.mockResolvedValueOnce({ chats: [], total: 0 });
  fireEvent.change(screen.getByPlaceholderText("Search Fleet"), {
    target: { value: "Release helper" },
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(300);
  });
  await settle();
  expect(screen.getByRole("button", { name: "Release helper" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Captured task" })).toBeNull();
});

it("retains assistant evidence loaded on page two after returning to page one", async () => {
  mocks.assistants = [
    {
      id: "assistant",
      name: "Release helper",
      projectId: "project",
      createdAt: NOW,
      updatedAt: NOW,
    },
  ];
  mount();
  await settle();
  expect(screen.queryByRole("button", { name: "Release helper" })).toBeNull();
  mocks.query.mockResolvedValueOnce({
    chats: [{ ...capture(), assistantId: "assistant" }],
    total: 120,
  });
  fireEvent.click(screen.getByRole("button", { name: "Next sessions" }));
  await settle();
  expect(screen.getByRole("button", { name: "Release helper" })).toBeTruthy();
  mocks.query.mockResolvedValueOnce({ chats: [capture()], total: 120 });
  fireEvent.click(screen.getByRole("button", { name: "Previous sessions" }));
  await settle();
  expect(screen.getByRole("button", { name: "Release helper" })).toBeTruthy();
  expect(screen.getByLabelText("URL").textContent).toContain("offset=0");
});

it("does not ingest the previous project's cached captures after a URL context switch", async () => {
  mocks.assistants = [
    {
      id: "assistant",
      name: "Release helper",
      projectId: "project",
      createdAt: NOW,
      updatedAt: NOW,
    },
  ];
  mocks.query.mockResolvedValueOnce({
    chats: [{ ...capture(), assistantId: "assistant" }],
    total: 120,
  });
  const { refresh } = mount();
  await settle();
  expect(screen.getByRole("button", { name: "Release helper" })).toBeTruthy();
  // Reusing the id in the registry makes stale evidence ingestion observable.
  mocks.projectId = "other-project";
  mocks.assistants = [
    {
      id: "assistant",
      name: "Other helper",
      projectId: "other-project",
      createdAt: NOW,
      updatedAt: NOW,
    },
  ];
  mocks.query.mockResolvedValueOnce({ chats: [], total: 0 });
  refresh();
  expect(screen.queryByRole("button", { name: "Other helper" })).toBeNull();
  await settle();
  expect(screen.queryByRole("button", { name: "Other helper" })).toBeNull();
  expect(mocks.listArgs.mock.calls.at(-1)?.[0]).toMatchObject({
    gramProject: "other-project",
  });
  mocks.query.mockResolvedValueOnce({
    chats: [{ ...capture(), assistantId: "assistant" }],
    total: 1,
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(30_000);
  });
  await settle();
  expect(screen.getByRole("button", { name: "Other helper" })).toBeTruthy();
});

it("composes a debounced search clear with a source change before a render", async () => {
  mount("?q=Release");
  await settle();
  const search = screen.getByPlaceholderText("Search Fleet");
  fireEvent.change(search, { target: { value: "" } });
  act(() => {
    vi.advanceTimersByTime(300);
    fireEvent.click(screen.getByRole("button", { name: "Registered agents" }));
  });
  await settle();
  expect(screen.getByLabelText("URL").textContent).toContain("source=agent");
  expect(screen.getByLabelText("URL").textContent).not.toContain("q=");
  expect((search as HTMLInputElement).value).toBe("");
});

it("finishes a search clear when source is clicked before debounce and data rerenders continue", async () => {
  const { refresh } = mount("?q=Release");
  await settle();
  const search = screen.getByPlaceholderText("Search Fleet");
  fireEvent.change(search, { target: { value: "" } });
  fireEvent.click(screen.getByRole("button", { name: "Registered agents" }));
  // Background data updates must not continually restart the pending debounce.
  for (let tick = 0; tick < 5; tick++) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    refresh();
  }
  expect(screen.getByLabelText("URL").textContent).toContain("source=agent");
  expect(screen.getByLabelText("URL").textContent).not.toContain("q=");
  expect((search as HTMLInputElement).value).toBe("");
});
