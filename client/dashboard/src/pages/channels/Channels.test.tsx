import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import Channels from "./Channels";
import { POLL_INTERVAL_MS, SNAPSHOT_INTERVAL_MS } from "./useChannels";
import {
  channelsRequest,
  ChannelsRequestError,
  type ChannelMessage,
  type ChannelsSnapshot,
} from "./api";

vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  channelsRequest: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "test-org" }),
  useUser: () => ({ id: "identity-1", email: "avery@example.test" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "test-project",
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: {
      members: [
        { id: "identity-1", name: "Avery Quinn", email: "avery@example.test" },
        {
          id: "identity-2",
          name: "Jordan Blake",
          email: "jordan@example.test",
        },
      ],
    },
  }),
}));
vi.mock("@/components/page-templates", () => {
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  return { FullBleedPage: Wrapper };
});

const request = vi.mocked(channelsRequest);
const online = "2099-01-01T00:00:00Z";
const SESSION = "session-1";
let snapshot: ChannelsSnapshot;
/**
 * Messages the server holds but does not put in the snapshot's tail, so the
 * page has to ask for them a page at a time.
 */
let archive: ChannelMessage[];
const HISTORY_PAGE = 50;

function mount(thread: string | null = "thread-1") {
  const client = new QueryClient({
    // Match the application's error-boundary defaults: Channels handles its
    // own failures instead of letting a background request replace the route.
    defaultOptions: {
      queries: { retry: false, throwOnError: true },
      mutations: { retry: false, throwOnError: true },
    },
  });
  const search = thread ? `?thread=${thread}` : "";
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <MemoryRouter initialEntries={[`/org/project/channels${search}`]}>
          <Channels />
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

/** The session the server opens or reclaims for the signed-in account. */
function withBrowserSession() {
  snapshot.instances[SESSION] = {
    id: SESSION,
    subjectId: "identity-1",
    label: "avery on dashboard",
    note: "",
    origin: "dashboard",
    expiresAt: online,
  };
}

/** Everything the server holds, oldest first: the archive plus the tail. */
function all(): ChannelMessage[] {
  return [...archive, ...snapshot.messages].sort((a, b) => a.id - b.id);
}

/**
 * Stands in for the server's bounded operations: the snapshot carries a tail,
 * `channels_read` carries what arrived after a cursor, and `channels_history`
 * pages one conversation backwards.
 */
async function respond(
  _project: string,
  operation?: string,
  args?: Record<string, unknown>,
): Promise<unknown> {
  if (operation === "channels_dashboard_session")
    return {
      instanceId: SESSION,
      label: "avery on dashboard",
      note: "",
      expiresAt: online,
    };
  if (operation === "channels_create_thread")
    return { id: "thread-2", title: "New topic" };
  if (operation === "channels_dm")
    return {
      id: "dm-1",
      title: "Direct message",
      participants: ["identity-1", "identity-2"],
      instanceIds: [SESSION, "instance-2"],
    };
  if (operation === "channels_post")
    return { id: 2, threadId: "thread-1", text: "sent" };
  if (operation === "channels_read") {
    const cursor = (args?.cursor as number | undefined) ?? 0;
    const after = all().filter((message) => message.id > cursor);
    return {
      messages: after,
      cursor: after[after.length - 1]?.id ?? cursor,
      hasMore: false,
    };
  }
  if (operation === "channels_history") {
    const threadId = args?.threadId as string;
    const before = args?.before as number | undefined;
    const limit = (args?.limit as number | undefined) ?? HISTORY_PAGE;
    const eligible = all().filter(
      (message) =>
        message.threadId === threadId &&
        (before === undefined || message.id < before),
    );
    const page = eligible.slice(Math.max(0, eligible.length - limit));
    const hasMore = page.length < eligible.length;
    return {
      messages: page,
      cursor: page[page.length - 1]?.id ?? 0,
      hasMore,
      ...(hasMore ? { nextBefore: page[0]!.id } : {}),
    };
  }
  return snapshot;
}

function operations(): string[] {
  return request.mock.calls
    .map((call) => call[1])
    .filter((operation): operation is string => typeof operation === "string");
}

function composer(): HTMLTextAreaElement {
  return screen.getByRole("textbox", {
    name: "Message",
  }) as HTMLTextAreaElement;
}

/** Queries scoped to one region: a conversation's title, preview and body all
 * legitimately show the same words. */
function inConversation() {
  return within(screen.getByLabelText("Thread messages"));
}
function inList() {
  return within(screen.getByLabelText("Conversations"));
}
function openConversation(title: RegExp) {
  fireEvent.click(inList().getByRole("button", { name: title }));
}

/** A conversation longer than the snapshot's tail, so paging is required. */
function withLongHistory(count: number) {
  archive = Array.from({ length: count }, (_, index) => ({
    id: index + 1,
    threadId: "thread-1",
    instanceId: "instance-2",
    subjectId: "identity-2",
    text: `Earlier ${index + 1}`,
    createdAt: "2026-01-01T00:00:00Z",
  }));
  snapshot.messages = [
    {
      id: count + 1,
      threadId: "thread-1",
      instanceId: "instance-2",
      subjectId: "identity-2",
      text: "Newest in the tail",
      createdAt: "2026-01-01T09:00:00Z",
    },
  ];
  snapshot.hasMore = true;
  snapshot.cursor = count + 1;
}

/** Gives the message list a measurable geometry jsdom does not provide. */
function measure(
  pane: HTMLElement,
  height: { value: number },
  clientHeight: number,
) {
  Object.defineProperties(pane, {
    scrollHeight: { configurable: true, get: () => height.value },
    clientHeight: { configurable: true, value: clientHeight },
  });
}

function historyCalls(): Record<string, unknown>[] {
  return request.mock.calls
    .filter((call) => call[1] === "channels_history")
    .map((call) => call[2] as Record<string, unknown>);
}

function readCursors(): unknown[] {
  return request.mock.calls
    .filter((call) => call[1] === "channels_read")
    .map((call) => (call[2] as { cursor?: unknown }).cursor);
}

beforeEach(() => {
  localStorage.clear();
  archive = [];
  snapshot = {
    enabled: true,
    instances: {
      "instance-2": {
        id: "instance-2",
        subjectId: "identity-2",
        label: "claude on laptop",
        note: "Reviewing the deploy",
        expiresAt: online,
      },
    },
    threads: { "thread-1": { id: "thread-1", title: "Planning" } },
    messages: [
      {
        id: 1,
        instanceId: "instance-2",
        subjectId: "identity-2",
        threadId: "thread-1",
        text: '<img src=x onerror="alert(1)"> [link](javascript:alert(1))',
        createdAt: "2026-01-01T00:00:00Z",
      },
    ],
    hasMore: false,
    preferences: { followed: [], muted: [] },
    viewer: { canConnect: true, canReadAll: false, canAdmin: false },
    cursor: 1,
  };
  request.mockReset();
  request.mockImplementation(respond);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("Channels", () => {
  it("opens the account's browser session on the first message, then reuses it", async () => {
    vi.useFakeTimers();
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(screen.getByRole("heading", { name: /Planning/ })).toBeDefined();
    expect(operations()).not.toContain("channels_dashboard_session");

    fireEvent.change(composer(), { target: { value: "Ready to review" } });
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    // The session is named by the server from the account; the dashboard asks
    // for it without proposing an identity of its own.
    expect(request).toHaveBeenCalledWith(
      "test-project",
      "channels_dashboard_session",
      { leaseSeconds: 300 },
    );
    expect(request).toHaveBeenCalledWith(
      "test-project",
      "channels_post",
      expect.objectContaining({
        instanceId: SESSION,
        threadId: "thread-1",
        text: "Ready to review",
        idempotencyKey: expect.any(String),
      }),
    );
    expect(composer().value).toBe("");

    // The snapshot has not confirmed the session yet, and must not have to:
    // the lease the server just issued is what the next send speaks through.
    fireEvent.change(composer(), { target: { value: "Straight after" } });
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(
      operations().filter(
        (operation) => operation === "channels_dashboard_session",
      ),
    ).toHaveLength(1);

    // Once the session is visible in the snapshot, sending reuses it.
    withBrowserSession();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(screen.getAllByText("avery on dashboard").length).toBeGreaterThan(0);
    fireEvent.change(composer(), { target: { value: "Second message" } });
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(
      operations().filter(
        (operation) => operation === "channels_dashboard_session",
      ),
    ).toHaveLength(1);
  });

  it("opens the newest conversation when the window shows both panes", async () => {
    vi.useFakeTimers();
    vi.stubGlobal(
      "matchMedia",
      (query: string) =>
        ({ matches: true, media: query }) as unknown as MediaQueryList,
    );
    snapshot.threads["thread-2"] = { id: "thread-2", title: "Implementation" };
    snapshot.messages.push({
      id: 2,
      threadId: "thread-2",
      instanceId: "instance-2",
      subjectId: "identity-2",
      text: "Latest activity",
      createdAt: "2026-01-02T00:00:00Z",
    });
    mount(null);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(
      screen.getByRole("heading", { name: /Implementation/ }),
    ).toBeDefined();
    vi.unstubAllGlobals();
  });

  it("shows an administrator whose own access is withheld only what they can change", async () => {
    vi.useFakeTimers();
    // The server answers such an administrator with configuration only.
    snapshot.viewer = { canConnect: false, canReadAll: false, canAdmin: true };
    snapshot.grants = {
      "identity-1": { canConnect: false, canReadAll: false },
    };
    snapshot.threads = {};
    snapshot.messages = [];
    snapshot.instances = {};
    mount(null);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 2);
    });

    expect(
      screen.getByRole("heading", { name: "You can't take part in Channels" }),
    ).toBeDefined();
    expect(screen.queryByRole("textbox", { name: "Message" })).toBeNull();
    expect(screen.queryByRole("button", { name: /New thread/ })).toBeNull();
    // Reads would be refused, so none are attempted.
    expect(operations()).not.toContain("channels_read");
    expect(operations()).not.toContain("channels_dashboard_session");
  });

  it("lets an administrator take part as soon as their own exception is removed", async () => {
    vi.useFakeTimers();
    snapshot.viewer = { canConnect: false, canReadAll: false, canAdmin: true };
    snapshot.grants = {
      "identity-1": { canConnect: false, canReadAll: false },
    };
    const full = { ...snapshot, messages: [...snapshot.messages] };
    snapshot.threads = {};
    snapshot.messages = [];
    mount(null);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    // Snapshot reads after this point never land, so only the administrative
    // answer itself can change what the page offers.
    request.mockImplementation(async (project, operation, args) => {
      if (operation === undefined) return new Promise(() => {});
      if (operation === "channels_admin")
        return {
          ...full,
          grants: {},
          viewer: { canConnect: true, canReadAll: false, canAdmin: true },
        };
      return respond(project, operation, args);
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Open Channels settings" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(request).toHaveBeenCalledWith("test-project", "channels_admin", {
      subjectId: "identity-1",
      remove: true,
    });
    expect(
      screen.queryByRole("heading", {
        name: "You can't take part in Channels",
        hidden: true,
      }),
    ).toBeNull();
    expect(
      screen.getByRole("textbox", { name: "Message", hidden: true }),
    ).toBeDefined();
  });

  it("clears what was on screen as soon as an administrator turns Channels off", async () => {
    vi.useFakeTimers();
    snapshot.viewer = { canConnect: true, canReadAll: false, canAdmin: true };
    snapshot.grants = {};
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(inConversation().getByText(/onerror/)).toBeDefined();

    request.mockImplementation(async (project, operation, args) => {
      if (operation === undefined) return new Promise(() => {});
      if (operation === "channels_admin")
        return {
          ...snapshot,
          enabled: false,
          threads: {},
          messages: [],
          instances: {},
        };
      return respond(project, operation, args);
    });
    fireEvent.click(screen.getByRole("button", { name: "Channels settings" }));
    fireEvent.click(
      screen.getByRole("switch", {
        name: "Channels available to this organization",
      }),
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(
      screen.getByRole("heading", {
        name: "Channels is off for this organization",
        hidden: true,
      }),
    ).toBeDefined();
    expect(screen.queryByText(/onerror/)).toBeNull();
  });

  it("never opens a session for someone who is only reading", async () => {
    vi.useFakeTimers();
    snapshot.threads["thread-2"] = { id: "thread-2", title: "Implementation" };
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    openConversation(/Implementation/);
    fireEvent.click(screen.getByRole("button", { name: "Follow" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(operations()).not.toContain("channels_dashboard_session");
    expect(operations()).not.toContain("channels_connect");
    expect(operations()).toContain("channels_follow");
  });

  it("keeps a failed message in the composer and retries it with the same key", async () => {
    mount();
    await screen.findByRole("heading", { name: /Planning/ });
    fireEvent.change(composer(), { target: { value: "Needs to survive" } });
    composer().focus();
    let posted = false;
    request.mockImplementation(async (project, operation) => {
      if (operation === "channels_post" && !posted) {
        posted = true;
        throw new Error("Channels is briefly unavailable");
      }
      return respond(project, operation);
    });
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      "Channels is briefly unavailable",
    );
    expect(composer().value).toBe("Needs to survive");
    expect(document.activeElement).toBe(composer());

    fireEvent.click(screen.getByRole("button", { name: "Send message" }));
    await waitFor(() => expect(composer().value).toBe(""));
    const posts = request.mock.calls.filter(
      (call) => call[1] === "channels_post",
    );
    expect(posts).toHaveLength(2);
    expect(posts[0]![2]).toEqual(posts[1]![2]);
  });

  it("keeps a draft edited after a failed send when the retry succeeds", async () => {
    mount();
    await screen.findByRole("heading", { name: /Planning/ });
    fireEvent.change(composer(), { target: { value: "First version" } });
    let posted = false;
    request.mockImplementation(async (project, operation) => {
      if (operation === "channels_post" && !posted) {
        posted = true;
        throw new Error("Channels is briefly unavailable");
      }
      return respond(project, operation);
    });
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));
    await screen.findByRole("alert");

    fireEvent.change(composer(), { target: { value: "Second version" } });
    fireEvent.click(screen.getByRole("button", { name: "Try again now" }));
    await waitFor(() =>
      expect(
        request.mock.calls.filter((call) => call[1] === "channels_post"),
      ).toHaveLength(2),
    );
    const posts = request.mock.calls.filter(
      (call) => call[1] === "channels_post",
    );
    expect(posts[0]![2]).toEqual(posts[1]![2]);
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    expect(composer().value).toBe("Second version");
  });

  it("follows long incoming messages but preserves intentional scrollback", async () => {
    vi.useFakeTimers();
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    const pane = screen.getByLabelText("Thread messages");
    let height = 1000;
    Object.defineProperties(pane, {
      scrollHeight: { configurable: true, get: () => height },
      clientHeight: { configurable: true, value: 400 },
    });
    pane.scrollTop = 600;
    fireEvent.scroll(pane);
    const append = async (id: number) => {
      snapshot = {
        ...snapshot,
        cursor: id,
        messages: [
          ...snapshot.messages,
          {
            id,
            threadId: "thread-1",
            instanceId: "instance-2",
            subjectId: "identity-2",
            text: `Incoming ${id}`,
            createdAt: "2026-01-01T00:01:00Z",
          },
        ],
      };
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000);
      });
    };
    height = 1600;
    await append(2);
    expect(pane.scrollTop).toBe(1600);
    pane.scrollTop = 200;
    fireEvent.scroll(pane);
    height = 2000;
    await append(3);
    expect(pane.scrollTop).toBe(200);
    pane.scrollTop = 1600;
    fireEvent.scroll(pane);
    height = 2400;
    await append(4);
    expect(pane.scrollTop).toBe(2400);
  });

  it("preserves composition, selection, caret and scroll while polling reconciles new messages", async () => {
    vi.useFakeTimers();
    withBrowserSession();
    snapshot.threads["thread-2"] = { id: "thread-2", title: "Implementation" };
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(
      screen.queryByRole("status", { name: "Loading conversations" }),
    ).toBeNull();

    // Hold a snapshot refresh open across the viewer's work, so a request
    // made before their selection completes after it.
    let completeRefresh!: (value: ChannelsSnapshot) => void;
    request.mockImplementation(async (project, operation, args) => {
      if (operation === undefined)
        return new Promise<ChannelsSnapshot>((resolve) => {
          completeRefresh = resolve;
        });
      return respond(project, operation, args);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(SNAPSHOT_INTERVAL_MS);
    });

    // Changing the selection and composing during an in-flight poll must win
    // over the older request's completion.
    openConversation(/Implementation/);
    const field = composer();
    fireEvent.change(field, { target: { value: "Unsent draft" } });
    field.focus();
    field.setSelectionRange(3, 7);
    const messages = screen.getByLabelText("Thread messages");
    const conversations = screen.getByLabelText("Conversations");
    messages.scrollTop = 135;
    conversations.scrollTop = 72;
    // The held refresh answers with what the server had when it was asked;
    // the message that arrives afterwards reaches the page through a read.
    const asked = { ...snapshot, messages: [...snapshot.messages] };
    snapshot.messages.push({
      id: 2,
      threadId: "thread-2",
      instanceId: "instance-2",
      subjectId: "identity-2",
      text: "Arrived during composition",
      createdAt: "2026-01-01T00:01:00Z",
    });
    snapshot.cursor = 2;
    await act(async () => {
      completeRefresh(asked);
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });

    expect(
      inConversation().getByText("Arrived during composition"),
    ).toBeDefined();
    expect(
      screen.getByRole("heading", { name: /Implementation/ }),
    ).toBeDefined();
    expect(composer()).toBe(field);
    expect(field.value).toBe("Unsent draft");
    expect(document.activeElement).toBe(field);
    expect([field.selectionStart, field.selectionEnd]).toEqual([3, 7]);
    expect(screen.getByLabelText("Thread messages")).toBe(messages);
    expect(messages.scrollTop).toBe(135);
    expect(conversations.scrollTop).toBe(72);
  });

  it("keeps cached conversations and drafts mounted after a failed poll and recovery", async () => {
    vi.useFakeTimers();
    withBrowserSession();
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    const field = composer();
    fireEvent.change(field, { target: { value: "Keep this draft" } });
    field.focus();
    const messages = screen.getByLabelText("Thread messages");
    messages.scrollTop = 80;
    request.mockRejectedValueOnce(new Error("Temporary refresh failure"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(screen.getByRole("alert").textContent).toContain(
      "Temporary refresh failure",
    );
    expect(composer()).toBe(field);
    expect(document.activeElement).toBe(field);
    expect(field.value).toBe("Keep this draft");
    expect(messages.scrollTop).toBe(80);
    expect(screen.getByRole("heading", { name: /Planning/ })).toBeDefined();
    expect(
      screen.queryByRole("status", { name: "Loading conversations" }),
    ).toBeNull();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    expect(screen.queryByRole("alert")).toBeNull();
    expect(composer()).toBe(field);
    expect(field.value).toBe("Keep this draft");
    expect(document.activeElement).toBe(field);
    expect(messages.scrollTop).toBe(80);
  });

  // Access can be withdrawn between any two requests, so whichever read
  // discovers it has to close the page down — not only the snapshot.
  it.each([
    [401, "channels_read"],
    [403, "channels_read"],
    [401, undefined],
    [403, undefined],
  ])(
    "hides cached conversations when %s arrives from %s",
    async (status, failing) => {
      vi.useFakeTimers();
      mount();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1);
      });
      expect(
        inConversation().getByText(snapshot.messages[0]!.text),
      ).toBeDefined();
      request.mockImplementation(async (project, operation, args) => {
        if (operation === failing)
          throw new ChannelsRequestError("Access revoked", status as number);
        return respond(project, operation, args);
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(SNAPSHOT_INTERVAL_MS);
        await vi.advanceTimersByTimeAsync(1);
      });
      const alert = screen.getByRole("alert").textContent ?? "";
      expect(alert).toContain("Access revoked");
      // Only one of these is something an administrator can restore.
      expect(alert).toContain(
        status === 401 ? "Sign in again" : "Ask an organization admin",
      );
      expect(screen.queryByText(snapshot.messages[0]!.text)).toBeNull();
      expect(screen.queryByRole("textbox", { name: "Message" })).toBeNull();
      expect(
        screen.queryByRole("status", { name: "Loading conversations" }),
      ).toBeNull();
    },
  );

  it("names the people behind agents and renders message text as plain text", async () => {
    const { container } = mount();
    await screen.findAllByText("claude on laptop");
    expect(screen.getAllByText("Jordan Blake").length).toBeGreaterThan(0);
    expect(screen.queryByText(/identity-2/)).toBeNull();
    expect(
      inConversation().getByText(snapshot.messages[0]!.text),
    ).toBeDefined();
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector('a[href^="javascript:"]')).toBeNull();
  });

  it("starts one direct conversation per agent and reuses it afterwards", async () => {
    vi.useFakeTimers();
    mount(null);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Message claude on laptop" }),
    );
    withBrowserSession();
    snapshot.threads["dm-1"] = {
      id: "dm-1",
      title: "Direct message",
      participants: ["identity-1", "identity-2"],
      instanceIds: [SESSION, "instance-2"],
    };
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(request).toHaveBeenCalledWith("test-project", "channels_dm", {
      instanceId: SESSION,
      recipientInstanceId: "instance-2",
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Message claude on laptop" }),
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(
      operations().filter((operation) => operation === "channels_dm"),
    ).toHaveLength(1);
    // The reused conversation is named after the other endpoint, not an ID.
    expect(
      screen.getAllByRole("heading", { name: /claude on laptop/ }).length,
    ).toBeGreaterThan(0);
  });

  it("creates a thread everyone can read", async () => {
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "New thread" }));
    fireEvent.change(
      screen.getByRole("textbox", { name: "New thread title" }),
      { target: { value: "New topic" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Create thread" }));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "test-project",
        "channels_create_thread",
        { instanceId: SESSION, title: "New topic" },
      ),
    );
  });

  it("lets an administrator turn Channels on where its state is shown", async () => {
    snapshot.enabled = false;
    snapshot.grants = {};
    snapshot.viewer.canAdmin = true;
    mount();
    fireEvent.click(
      await screen.findByRole("button", { name: "Turn on Channels" }),
    );
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("test-project", "channels_admin", {
        enabled: true,
      }),
    );
    expect(screen.queryByRole("textbox", { name: "Message" })).toBeNull();
  });

  it("reports an authentication failure instead of inventing an identity", async () => {
    request.mockRejectedValue(new Error("authentication required"));
    mount();
    expect((await screen.findByRole("alert")).textContent).toContain(
      "authentication required",
    );
    expect(screen.queryByRole("textbox", { name: "Message" })).toBeNull();
    expect(operations()).not.toContain("channels_dashboard_session");
  });
  it("reads forward from the last message it has, not the whole archive", async () => {
    vi.useFakeTimers();
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS + 1);
    });
    // The first read starts where the snapshot's tail ended.
    expect(readCursors()).toEqual([1]);

    snapshot.messages.push({
      id: 2,
      threadId: "thread-1",
      instanceId: "instance-2",
      subjectId: "identity-2",
      text: "Said after the snapshot",
      createdAt: "2026-01-01T00:02:00Z",
    });
    snapshot.cursor = 2;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    expect(inConversation().getByText("Said after the snapshot")).toBeDefined();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    // Each read asks only for what it has not seen, so nothing is re-sent.
    expect(readCursors()).toEqual([1, 1, 2]);
  });

  it("catches up at once when a read reports more already waiting", async () => {
    vi.useFakeTimers();
    let served = 0;
    request.mockImplementation(async (project, operation, args) => {
      if (operation === "channels_read") {
        served++;
        if (served === 1)
          return {
            messages: [
              {
                id: 2,
                threadId: "thread-1",
                instanceId: "instance-2",
                subjectId: "identity-2",
                text: "First of a backlog",
                createdAt: "2026-01-01T00:02:00Z",
              },
            ],
            cursor: 2,
            hasMore: true,
          };
        return {
          messages:
            served === 2
              ? [
                  {
                    id: 3,
                    threadId: "thread-1",
                    instanceId: "instance-2",
                    subjectId: "identity-2",
                    text: "Rest of the backlog",
                    createdAt: "2026-01-01T00:03:00Z",
                  },
                ]
              : [],
          cursor: served === 2 ? 3 : 3,
          hasMore: false,
        };
      }
      return respond(project, operation, args);
    });
    mount();
    // One interval, plus the immediate follow-up the first read asked for.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS + 1);
    });
    expect(served).toBe(2);
    expect(inConversation().getByText("First of a backlog")).toBeDefined();
    expect(inConversation().getByText("Rest of the backlog")).toBeDefined();
  });

  it("does not trade requests with a server whose snapshot and read disagree", async () => {
    vi.useFakeTimers();
    // A snapshot that hands out a cursor its own read then rejects as stale.
    // Each call makes progress on its own, so only bounding the reaction to a
    // floor already seen stops the two endpoints chasing each other.
    const FLOOR = 900;
    snapshot.messages = [];
    snapshot.cursor = 0;
    request.mockImplementation(async (project, operation, args) => {
      if (operation === "channels_read")
        return { messages: [], cursor: FLOOR, hasMore: true, stale: true };
      return respond(project, operation, args);
    });
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 12);
    });

    const snapshots = request.mock.calls.filter(
      (call) => call[1] === undefined,
    );
    const reads = request.mock.calls.filter(
      (call) => call[1] === "channels_read",
    );
    // Reads stay on their interval rather than spinning, and the stale floor
    // is only worth one extra snapshot however long this goes on.
    expect(reads.length).toBeLessThanOrEqual(13);
    expect(snapshots.length).toBeLessThanOrEqual(8);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("loads older messages a page at a time without moving the reader's view", async () => {
    vi.useFakeTimers();
    withLongHistory(120);
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    // Opening a conversation reads that conversation's own newest page.
    expect(historyCalls()).toEqual([{ threadId: "thread-1", limit: 50 }]);
    expect(inConversation().getByText("Earlier 72")).toBeDefined();
    expect(inConversation().queryByText("Earlier 71")).toBeNull();

    const pane = screen.getByLabelText("Thread messages");
    const height = { value: 2000 };
    measure(pane, height, 400);
    pane.scrollTop = 300;
    fireEvent.scroll(pane);

    fireEvent.click(
      screen.getByRole("button", { name: /Load older messages/ }),
    );
    height.value = 3000;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });

    // The page above arrived, and the reader is still looking at what they
    // were looking at: the same distance from the end of the conversation.
    expect(historyCalls()[1]).toEqual({
      threadId: "thread-1",
      limit: 50,
      before: 72,
    });
    expect(inConversation().getByText("Earlier 71")).toBeDefined();
    expect(pane.scrollTop).toBe(1300);
  });

  it("reads the conversations an organization has beyond the first page", async () => {
    vi.useFakeTimers();
    snapshot.threadsHasMore = true;
    snapshot.threadsNextAfter = "page-2";
    request.mockImplementation(async (project, operation, args) => {
      if (operation === "channels_conversations")
        return {
          threads:
            args?.after === "page-2"
              ? { "thread-older": { id: "thread-older", title: "Older topic" } }
              : {
                  "thread-oldest": {
                    id: "thread-oldest",
                    title: "Oldest topic",
                  },
                },
          hasMore: args?.after === "page-2",
          ...(args?.after === "page-2" ? { nextAfter: "page-3" } : {}),
        };
      return respond(project, operation, args);
    });
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    // Nothing is read until the reader asks: a long list must not fan out.
    expect(operations()).not.toContain("channels_conversations");

    fireEvent.click(
      screen.getByRole("button", { name: /Load more conversations/ }),
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(request).toHaveBeenCalledWith(
      "test-project",
      "channels_conversations",
      { after: "page-2", limit: 100 },
    );
    // One press is one read, however many times React renders it.
    expect(
      operations().filter(
        (operation) => operation === "channels_conversations",
      ),
    ).toHaveLength(1);
    expect(inList().getByText("Older topic")).toBeDefined();
    // The conversations already listed are still listed.
    expect(inList().getByText("Planning")).toBeDefined();

    // Each page says where the next one starts, and the offer stops when the
    // organization has no more to give.
    fireEvent.click(
      screen.getByRole("button", { name: /Load more conversations/ }),
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(request).toHaveBeenCalledWith(
      "test-project",
      "channels_conversations",
      { after: "page-3", limit: 100 },
    );
    expect(inList().getByText("Oldest topic")).toBeDefined();
    expect(
      screen.queryByRole("button", { name: /Load more conversations/ }),
    ).toBeNull();
  });

  it("offers to read the rest of the conversations again after a failure", async () => {
    vi.useFakeTimers();
    snapshot.threadsHasMore = true;
    snapshot.threadsNextAfter = "page-2";
    let failed = false;
    request.mockImplementation(async (project, operation, args) => {
      if (operation === "channels_conversations") {
        if (!failed) {
          failed = true;
          throw new ChannelsRequestError("conversations unavailable", 503);
        }
        return {
          threads: {
            "thread-older": { id: "thread-older", title: "Older topic" },
          },
          hasMore: false,
        };
      }
      return respond(project, operation, args);
    });
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    fireEvent.click(
      screen.getByRole("button", { name: /Load more conversations/ }),
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(inList().getByText(/conversations unavailable/)).toBeDefined();
    // The failure belongs to the list, not to the page: nothing else moved.
    expect(screen.getByRole("heading", { name: /Planning/ })).toBeDefined();

    fireEvent.click(inList().getByRole("button", { name: /Try again/ }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(inList().getByText("Older topic")).toBeDefined();
    expect(inList().queryByText(/conversations unavailable/)).toBeNull();
  });

  it("says nothing about more conversations when the list is complete", async () => {
    vi.useFakeTimers();
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 3);
    });
    expect(
      screen.queryByRole("button", { name: /Load more conversations/ }),
    ).toBeNull();
    expect(operations()).not.toContain("channels_conversations");
  });

  it("reads a conversation's newest page once while that read is in flight", async () => {
    vi.useFakeTimers();
    withLongHistory(120);
    // Hold the opening read open, so the pane renders knowing only that it
    // has not been told whether older messages exist.
    let deliver!: (page: unknown) => void;
    request.mockImplementation(async (project, operation, args) => {
      if (operation === "channels_history")
        return new Promise((resolve) => {
          deliver = resolve;
        });
      return respond(project, operation, args);
    });
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(historyCalls()).toHaveLength(1);

    // An empty list is already scrolled to its top. That must not count as the
    // reader asking for the page above, or it races the opening read.
    fireEvent.scroll(screen.getByLabelText("Thread messages"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(historyCalls()).toHaveLength(1);

    await act(async () => {
      deliver({
        messages: all().slice(-50),
        cursor: 121,
        hasMore: true,
        nextBefore: 72,
      });
      await vi.advanceTimersByTimeAsync(1);
    });
    // Once there is a known page above, reading upwards asks for it.
    fireEvent.scroll(screen.getByLabelText("Thread messages"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(historyCalls()).toHaveLength(2);
    expect(historyCalls()[1]).toMatchObject({ before: 72 });
  });

  it("asks for history only for the conversation that is open", async () => {
    vi.useFakeTimers();
    withLongHistory(60);
    snapshot.threads["thread-2"] = { id: "thread-2", title: "Implementation" };
    snapshot.threads["thread-3"] = { id: "thread-3", title: "Retro" };
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 3);
    });
    expect(historyCalls()).toEqual([{ threadId: "thread-1", limit: 50 }]);

    openConversation(/Implementation/);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(historyCalls()).toHaveLength(2);
    expect(historyCalls()[1]).toMatchObject({ threadId: "thread-2" });
  });

  it("stays quiet when opening a conversation cannot reconcile its history", async () => {
    vi.useFakeTimers();
    withLongHistory(60);
    request.mockImplementation(async (project, operation, args) => {
      if (operation === "channels_history")
        throw new ChannelsRequestError("history unavailable", 500);
      return respond(project, operation, args);
    });
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    // The tail is on screen, so a background reconciliation failure is not
    // the viewer's problem — but the offer to read further stays.
    expect(inConversation().getByText("Newest in the tail")).toBeDefined();
    expect(screen.queryByText(/could not be loaded/)).toBeNull();
    const button = screen.getByRole("button", {
      name: /Load older messages/,
    });

    // Asking for it is different: a failure the viewer caused is reported.
    fireEvent.click(button);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(inConversation().getByText(/history unavailable/)).toBeDefined();
    expect(screen.getByRole("button", { name: /Try again/ })).toBeDefined();
  });

  it("stops offering older messages once retention has removed them", async () => {
    vi.useFakeTimers();
    withLongHistory(60);
    request.mockImplementation(async (project, operation, args) => {
      if (operation === "channels_history")
        return {
          messages: snapshot.messages,
          cursor: snapshot.cursor,
          hasMore: false,
          stale: true,
        };
      return respond(project, operation, args);
    });
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(inConversation().getByText(/no longer kept/)).toBeDefined();
    expect(
      screen.queryByRole("button", { name: /Load older messages/ }),
    ).toBeNull();
  });

  it("offers a way back to the latest message, and counts what arrived while reading back", async () => {
    vi.useFakeTimers();
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    const pane = screen.getByLabelText("Thread messages");
    const height = { value: 1000 };
    measure(pane, height, 400);
    expect(screen.queryByRole("button", { name: /Jump to latest/ })).toBeNull();

    pane.scrollTop = 200;
    fireEvent.scroll(pane);
    expect(
      screen.getByRole("button", { name: /Jump to latest/ }),
    ).toBeDefined();

    snapshot.messages.push({
      id: 2,
      threadId: "thread-1",
      instanceId: "instance-2",
      subjectId: "identity-2",
      text: "Arrived while reading back",
      createdAt: "2026-01-01T00:02:00Z",
    });
    snapshot.cursor = 2;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    // The reader stayed where they were, and is told what they have not seen.
    expect(pane.scrollTop).toBe(200);
    const jump = screen.getByRole("button", { name: /1 new message/ });

    fireEvent.click(jump);
    expect(pane.scrollTop).toBe(1000);
    expect(screen.queryByRole("button", { name: /new message/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Jump to latest/ })).toBeNull();
  });

  it("returns a reader to the end of the conversation when they send", async () => {
    vi.useFakeTimers();
    withBrowserSession();
    mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    const pane = screen.getByLabelText("Thread messages");
    const height = { value: 1000 };
    measure(pane, height, 400);
    pane.scrollTop = 200;
    fireEvent.scroll(pane);
    expect(
      screen.getByRole("button", { name: /Jump to latest/ }),
    ).toBeDefined();

    fireEvent.change(composer(), { target: { value: "Back to the end" } });
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));
    snapshot.messages.push({
      id: 2,
      threadId: "thread-1",
      instanceId: SESSION,
      subjectId: "identity-1",
      text: "Back to the end",
      createdAt: "2026-01-01T00:02:00Z",
    });
    snapshot.cursor = 2;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    // Their own message is not something they have to be told about.
    expect(screen.queryByRole("button", { name: /Jump to latest/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /new message/ })).toBeNull();
    expect(pane.scrollTop).toBe(1000);
  });

  it("tells a person what their agent has to do to hear about a conversation", async () => {
    mount();
    // The control is only live once the page knows Channels is available.
    await screen.findByRole("heading", { name: /Planning/ });
    fireEvent.click(screen.getByRole("button", { name: "Channels settings" }));
    const sheet = within(await screen.findByRole("dialog"));
    expect(sheet.getByText(/channels:\/\/messages/)).toBeDefined();
    expect(sheet.getByText(/channels_follow/)).toBeDefined();
    expect(
      sheet.getByText(/Following without subscribing delivers nothing/),
    ).toBeDefined();
  });

  it("reports a send the organization throttled as worth retrying", async () => {
    mount();
    await screen.findByRole("heading", { name: /Planning/ });
    fireEvent.change(composer(), { target: { value: "One too many" } });
    request.mockImplementation(async (project, operation, args) => {
      if (operation === "channels_post")
        throw new ChannelsRequestError("write burst limit reached", 429);
      return respond(project, operation, args);
    });
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Too many messages at once");
    expect(alert.textContent).toContain("will not be duplicated");
    // Throttling is not revocation: the conversation stays readable.
    expect(composer().value).toBe("One too many");
    expect(screen.getByRole("heading", { name: /Planning/ })).toBeDefined();
  });

  it("refuses to send a message the server would reject as too long", async () => {
    mount();
    await screen.findByRole("heading", { name: /Planning/ });
    // Each of these characters is three bytes, so the byte limit is reached
    // well before the character count suggests.
    fireEvent.change(composer(), { target: { value: "あ".repeat(11_000) } });
    expect(screen.getByRole("button", { name: "Send message" })).toHaveProperty(
      "disabled",
      true,
    );
    expect(screen.getByText(/Too long to send/)).toBeDefined();
  });
});
