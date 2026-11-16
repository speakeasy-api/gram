import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  channelsRequest,
  ChannelsRequestError,
  type ChannelMessage,
  type ChannelsSnapshot,
} from "./api";
import { useChannelsWorkspace } from "./useChannels";

vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  channelsRequest: vi.fn(),
}));

const context = vi.hoisted(() => ({
  orgId: "test-org",
  orgSlug: "test-org",
  projectId: "project-1",
  projectSlug: "test-project",
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: context.orgId, slug: context.orgSlug }),
  useProject: () => ({ id: context.projectId, slug: context.projectSlug }),
}));
const request = vi.mocked(channelsRequest);
const empty: ChannelsSnapshot = {
  enabled: true,
  instances: {},
  threads: {},
  messages: [],
  preferences: { followed: [], muted: [] },
  viewer: { canConnect: true, canReadAll: false, canAdmin: false },
  cursor: 0,
};

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

function message(id: number, threadId: string): ChannelMessage {
  return {
    id,
    threadId,
    instanceId: "instance-1",
    subjectId: "identity-1",
    text: `message-${id}`,
    createdAt: "2026-01-01T00:00:00Z",
  };
}

function sharedWrapper(client: QueryClient) {
  return function Provider({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
  };
}

const queryKey = ["channels", "test-org", "identity-1", "project-1"];

beforeEach(() => {
  context.orgId = "test-org";
  context.orgSlug = "test-org";
  context.projectId = "project-1";
  context.projectSlug = "test-project";
  request.mockReset();
  request.mockImplementation(async (_project, operation) => {
    if (operation === "channels_dashboard_session")
      return {
        instanceId: "session-1",
        label: "avery on dashboard",
        note: "",
        expiresAt: "2099-01-01T00:00:00Z",
      };
    if (operation) return { id: 1 };
    return empty;
  });
});

describe("Channels workspace", () => {
  it("opens one browser session for messages sent at the same moment", async () => {
    const { result } = renderHook(
      () => useChannelsWorkspace("test-org", "identity-1"),
      { wrapper },
    );

    await act(async () => {
      await Promise.all([
        result.current.run({
          operation: "channels_post",
          withSession: true,
          args: { threadId: "thread-1", text: "first", idempotencyKey: "a" },
        }),
        result.current.run({
          operation: "channels_post",
          withSession: true,
          args: { threadId: "thread-1", text: "second", idempotencyKey: "b" },
        }),
      ]);
    });

    const sessions = request.mock.calls.filter(
      (call) => call[1] === "channels_dashboard_session",
    );
    expect(sessions).toHaveLength(1);
    const posts = request.mock.calls.filter(
      (call) => call[1] === "channels_post",
    );
    expect(posts).toHaveLength(2);
    expect(
      posts.map((call) => (call[2] as { instanceId: string }).instanceId),
    ).toEqual(["session-1", "session-1"]);
  });

  it("can open a session again after the first attempt fails", async () => {
    request.mockImplementationOnce(async () => empty);
    request.mockImplementationOnce(async () => {
      throw new Error("storage unavailable");
    });
    const { result } = renderHook(
      () => useChannelsWorkspace("test-org", "identity-1"),
      { wrapper },
    );

    await act(async () => {
      expect(
        await result.current.run({
          operation: "channels_post",
          withSession: true,
          args: {},
        }),
      ).toBeUndefined();
    });
    await act(async () => {
      expect(
        await result.current.run({
          operation: "channels_post",
          withSession: true,
          args: {},
        }),
      ).toBeDefined();
    });
    expect(
      request.mock.calls.filter(
        (call) => call[1] === "channels_dashboard_session",
      ),
    ).toHaveLength(2);
  });

  it("forgets paged private conversations and history when read-all is removed", async () => {
    const privateSnapshot: ChannelsSnapshot = {
      ...empty,
      viewer: { ...empty.viewer, canReadAll: true },
      threads: { public: { id: "public", title: "Public" } },
      messages: [message(10, "public")],
      cursor: 10,
      threadsHasMore: true,
      threadsNextAfter: "page-1",
    };
    const downgraded: ChannelsSnapshot = {
      ...privateSnapshot,
      viewer: empty.viewer,
      messages: [message(20, "public")],
      cursor: 20,
      // Still paginated: clearing pages must not depend on completeness.
      threadsHasMore: true,
      threadsNextAfter: "new-page-1",
    };
    const oldHistory = deferred<{
      messages: ChannelMessage[];
      cursor: number;
      hasMore: boolean;
    }>();
    const oldConversations = deferred<{
      threads: ChannelsSnapshot["threads"];
      hasMore: boolean;
    }>();
    let snapshot = privateSnapshot;
    let conversationReads = 0;
    request.mockImplementation(async (_project, operation, args) => {
      if (!operation) return snapshot;
      if (operation === "channels_conversations") {
        if (conversationReads++ > 0) return oldConversations.promise;
        return {
          threads: {
            private: { id: "private", title: "Private" },
            pending: { id: "pending", title: "Pending private" },
          },
          hasMore: true,
          nextAfter: "page-2",
        };
      }
      if (operation === "channels_history") {
        if (args?.threadId === "pending") return oldHistory.promise;
        return {
          messages: [message(1, "private")],
          cursor: 10,
          hasMore: false,
        };
      }
      return { messages: [], cursor: 20 };
    });
    const { result } = renderHook(
      () => useChannelsWorkspace("test-org", "identity-1"),
      { wrapper },
    );
    await waitFor(() => expect(result.current.data?.cursor).toBe(10));
    act(() => result.current.loadMoreConversations());
    await waitFor(() =>
      expect(result.current.data?.threads.private).toBeDefined(),
    );
    act(() => result.current.openConversation("private"));
    await waitFor(() =>
      expect(result.current.store.byThread.private?.messages).toEqual([
        message(1, "private"),
      ]),
    );
    act(() => {
      result.current.openConversation("pending");
      result.current.loadMoreConversations();
    });
    expect(result.current.moreConversations.loading).toBe(true);
    snapshot = downgraded;
    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.data?.cursor).toBe(20));
    expect(Object.keys(result.current.data!.threads)).toEqual(["public"]);
    expect(Object.keys(result.current.store.byThread)).toEqual(["public"]);
    expect(result.current.store.byThread.public?.messages).toEqual([
      message(20, "public"),
    ]);
    await act(async () => {
      oldHistory.resolve({
        messages: [message(2, "pending")],
        cursor: 10,
        hasMore: false,
      });
      oldConversations.resolve({
        threads: { late: { id: "late", title: "Late private" } },
        hasMore: false,
      });
    });
    expect(Object.keys(result.current.data!.threads)).toEqual(["public"]);
    expect(Object.keys(result.current.store.byThread)).toEqual(["public"]);
    expect(result.current.moreConversations.loading).toBe(false);
    expect(result.current.moreConversations.hasMore).toBe(true);
  });

  it("sanitizes the shared cache after a history 403, including an older in-flight snapshot", async () => {
    const secretSnapshot: ChannelsSnapshot = {
      ...empty,
      threads: { secret: { id: "secret", title: "Private cached thread" } },
      messages: [message(10, "secret")],
      cursor: 10,
    };
    const oldSnapshot = deferred<ChannelsSnapshot>();
    const recoverySnapshot = deferred<ChannelsSnapshot>();
    const history = deferred<never>();
    let snapshots = 0;
    request.mockImplementation(async (_project, operation) => {
      if (!operation) {
        snapshots += 1;
        if (snapshots === 1) return secretSnapshot;
        if (snapshots === 2) return oldSnapshot.promise;
        return recoverySnapshot.promise;
      }
      if (operation === "channels_history") return history.promise;
      return { messages: [], cursor: 10 };
    });
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const alternateProjectKey = [
      "channels",
      "test-org",
      "identity-1",
      "alternate-project",
    ];
    client.setQueryData(alternateProjectKey, secretSnapshot);
    const provider = sharedWrapper(client);
    const first = renderHook(
      () => useChannelsWorkspace("test-org", "identity-1"),
      { wrapper: provider },
    );
    await waitFor(() =>
      expect(first.result.current.store.byThread.secret?.messages).toHaveLength(
        1,
      ),
    );
    act(() => {
      first.result.current.refresh();
      first.result.current.openConversation("secret");
    });
    await waitFor(() => expect(snapshots).toBe(2));
    await act(async () =>
      history.reject(new ChannelsRequestError("Forbidden", 403)),
    );
    await waitFor(() => expect(first.result.current.accessDenied).toBe(true));
    const expectSanitizedCache = () => {
      for (const key of [queryKey]) {
        const cached = client.getQueryData<ChannelsSnapshot>(key);
        expect(cached?.messages ?? []).toEqual([]);
        expect(cached?.threads ?? {}).toEqual({});
      }
    };
    expectSanitizedCache();
    expect(client.getQueryData(alternateProjectKey)).toEqual(secretSnapshot);
    await act(async () => oldSnapshot.resolve(secretSnapshot));
    expectSanitizedCache();
    expect(first.result.current.store.byThread).toEqual({});
    first.unmount();
    const second = renderHook(
      () => useChannelsWorkspace("test-org", "identity-1"),
      { wrapper: provider },
    );
    expect(second.result.current.store.byThread).toEqual({});
    expect(second.result.current.data?.messages ?? []).toEqual([]);
    await act(async () => {
      second.result.current.refresh();
    });
    await act(async () => recoverySnapshot.resolve({ ...empty, cursor: 20 }));
    await waitFor(() => expect(second.result.current.data?.cursor).toBe(20));
    expect(second.result.current.accessDenied).toBe(false);
    expect(second.result.current.store.byThread).toEqual({});
    second.unmount();
    client.clear();
  });

  it("ignores history started before a stale incremental read resets retention", async () => {
    const initial: ChannelsSnapshot = {
      ...empty,
      threads: { public: { id: "public", title: "Public" } },
      messages: [message(10, "public")],
      cursor: 10,
    };
    const retained = {
      ...initial,
      messages: [message(100, "public")],
      cursor: 100,
    };
    const history = deferred<{
      messages: ChannelMessage[];
      cursor: number;
      hasMore: boolean;
    }>();
    let snapshot = initial;
    request.mockImplementation(async (_project, operation) => {
      if (!operation) return snapshot;
      if (operation === "channels_history") return history.promise;
      if (operation === "channels_read")
        return { messages: [], cursor: 100, stale: true };
      return {};
    });
    vi.useFakeTimers();
    const { result, unmount } = renderHook(
      () => useChannelsWorkspace("test-org", "identity-1"),
      { wrapper },
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(result.current.store.byThread.public?.messages).toEqual(
      initial.messages,
    );
    act(() => result.current.openConversation("public"));
    snapshot = retained;
    try {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
      });
      // Query notifications are scheduled separately from the poll promise.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1);
      });
      expect(result.current.store.byThread.public?.messages).toEqual(
        retained.messages,
      );
      await act(async () =>
        history.resolve({
          messages: [message(1, "public")],
          cursor: 10,
          hasMore: false,
        }),
      );
      expect(result.current.store.byThread.public?.messages).toEqual(
        retained.messages,
      );
      expect(result.current.store.byThread.public?.loading).toBe(false);
      expect(result.current.store.cursor).toBe(100);
    } finally {
      unmount();
      vi.useRealTimers();
    }
  });

  it("polls from the last incremental cursor rather than a bounded snapshot tail", async () => {
    const initial: ChannelsSnapshot = {
      ...empty,
      threads: { public: { id: "public", title: "Public" } },
      messages: [message(100, "public")],
      cursor: 100,
    };
    const arriving = Array.from({ length: 100 }, (_, index) =>
      message(index + 101, "public"),
    );
    let snapshot = initial;
    request.mockImplementation(async (_project, operation) => {
      if (!operation) return snapshot;
      if (operation === "channels_history")
        return { messages: initial.messages, cursor: 100, hasMore: false };
      if (operation === "channels_read")
        return { messages: arriving, cursor: 200, hasMore: false };
      return {};
    });
    vi.useFakeTimers();
    const { result, unmount } = renderHook(
      () => useChannelsWorkspace("test-org", "identity-1"),
      { wrapper },
    );
    try {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1);
      });
      act(() => result.current.openConversation("public"));
      await act(async () => {});
      expect(result.current.store.byThread.public?.hasMore).toBe(false);
      snapshot = { ...initial, messages: arriving.slice(50), cursor: 200 };
      act(() => result.current.refresh());
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1);
      });
      expect(result.current.data?.cursor).toBe(200);
      expect(result.current.store.byThread.public?.messages).toEqual([
        message(100, "public"),
        ...arriving.slice(50),
      ]);
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
      });
      const polls = request.mock.calls.filter(
        (call) => call[1] === "channels_read",
      );
      expect(polls).toHaveLength(1);
      expect(polls[0]?.[2]).toMatchObject({ cursor: 100 });
      expect(result.current.store.byThread.public?.messages).toEqual([
        message(100, "public"),
        ...arriving,
      ]);
      expect(result.current.store.cursor).toBe(200);
    } finally {
      unmount();
      vi.useRealTimers();
    }
  });

  it("reports a first-snapshot 401 without staying in the loading state", async () => {
    const denied = new ChannelsRequestError("Unauthorized", 401);
    request.mockRejectedValue(denied);
    const { result } = renderHook(
      () => useChannelsWorkspace("test-org", "identity-1"),
      { wrapper },
    );
    await waitFor(() => expect(result.current.accessDenied).toBe(true));
    expect(result.current.isLoading).toBe(false);
    expect(result.current.error).toBe(denied);
    expect(result.current.data).toBeUndefined();
    expect(result.current.store.byThread).toEqual({});
  });
  it("does not let a stale admin mutation restore content after a history denial", async () => {
    const secret: ChannelsSnapshot = {
      ...empty,
      cursor: 10,
      messages: [message(10, "secret")],
      threads: { secret: { id: "secret", title: "Secret" } },
    };
    const admin = deferred<ChannelsSnapshot>();
    const history = deferred<never>();
    request.mockImplementation(async (_project, operation) => {
      if (!operation) return secret;
      if (operation === "channels_admin") return admin.promise;
      if (operation === "channels_history") return history.promise;
      return {};
    });
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result } = renderHook(
      () => useChannelsWorkspace("test-org", "identity-1"),
      {
        wrapper: sharedWrapper(client),
      },
    );
    await waitFor(() => expect(result.current.data?.cursor).toBe(10));
    let pending!: Promise<unknown>;
    act(() => {
      pending = result.current.run({
        operation: "channels_admin",
        args: { enabled: true },
      });
      result.current.openConversation("secret");
    });
    await waitFor(() =>
      expect(
        request.mock.calls.some((call) => call[1] === "channels_admin"),
      ).toBe(true),
    );
    await act(async () =>
      history.reject(new ChannelsRequestError("Forbidden", 403)),
    );
    await waitFor(() => expect(result.current.accessDenied).toBe(true));
    await act(async () => {
      admin.resolve(secret);
      await pending;
    });
    expect(client.getQueryData<ChannelsSnapshot>(queryKey)?.messages).toEqual(
      [],
    );
    expect(result.current.store.byThread).toEqual({});
  });

  it("sanitizes the shared snapshot when an action first discovers revocation", async () => {
    request.mockImplementation(async (_project, operation) => {
      if (operation) throw new ChannelsRequestError("Forbidden", 403);
      return { ...empty, cursor: 10, messages: [message(10, "public")] };
    });
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result } = renderHook(
      () => useChannelsWorkspace("test-org", "identity-1"),
      {
        wrapper: sharedWrapper(client),
      },
    );
    await waitFor(() => expect(result.current.data?.cursor).toBe(10));
    await act(async () => {
      await result.current.run({
        operation: "channels_follow",
        args: { threadId: "public", follow: true },
      });
    });
    expect(result.current.accessDenied).toBe(true);
    expect(client.getQueryData<ChannelsSnapshot>(queryKey)?.messages).toEqual(
      [],
    );
    expect(result.current.store.byThread).toEqual({});
  });
});

describe("project boundaries", () => {
  it.each(["project", "organization"] as const)(
    "discards protected reads and an opening session on a %s switch",
    async (boundary) => {
      const secret: ChannelsSnapshot = {
        ...empty,
        threads: { secret: { id: "secret", title: "Private old project" } },
        messages: [message(10, "secret")],
        cursor: 10,
      };
      const oldSnapshot = deferred<ChannelsSnapshot>();
      const oldHistory = deferred<unknown>();
      const oldSession = deferred<unknown>();
      const nextSnapshot = deferred<ChannelsSnapshot>();
      let reads = 0;
      request.mockImplementation(async (project, operation) => {
        const old =
          project.organizationSlug === "test-org" &&
          project.projectSlug === "test-project";
        if (!operation) {
          if (!old) return nextSnapshot.promise;
          return ++reads === 1 ? secret : oldSnapshot.promise;
        }
        if (operation === "channels_history") return oldHistory.promise;
        if (operation === "channels_dashboard_session") {
          if (old) return oldSession.promise;
          return {
            instanceId: "next-session",
            expiresAt: "2099-01-01T00:00:00Z",
          };
        }
        return { messages: [], cursor: 10 };
      });
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      const { result, rerender } = renderHook(
        () => useChannelsWorkspace(context.orgId, "identity-1"),
        { wrapper: sharedWrapper(client) },
      );
      await waitFor(() =>
        expect(result.current.store.byThread.secret?.messages).toHaveLength(1),
      );
      let sent!: Promise<unknown>;
      act(() => {
        result.current.openConversation("secret");
        result.current.refresh();
        sent = result.current
          .run({
            operation: "channels_post",
            args: { threadId: "secret", text: "old draft" },
            withSession: true,
          })
          .catch((error) => error);
      });
      await waitFor(() =>
        expect(
          request.mock.calls.some(
            ([, operation]) => operation === "channels_dashboard_session",
          ),
        ).toBe(true),
      );
      if (boundary === "organization") {
        context.orgId = "other-org";
        context.orgSlug = "other-org";
      } else {
        context.projectId = "project-2";
        context.projectSlug = "other-project";
      }
      rerender();
      expect(result.current.data).toBeUndefined();
      expect(result.current.store.byThread).toEqual({});
      expect(result.current.sessionInstanceId).toBeUndefined();
      expect(client.getQueryData(queryKey)).toBeUndefined();
      await act(async () => {
        oldSnapshot.resolve(secret);
        oldHistory.resolve({
          messages: [message(1, "secret")],
          cursor: 1,
          hasMore: false,
        });
        oldSession.resolve({
          instanceId: "old-session",
          expiresAt: "2099-01-01T00:00:00Z",
        });
        await sent;
      });
      expect(result.current.store.byThread).toEqual({});
      expect(result.current.sessionInstanceId).toBeUndefined();
      expect(client.getQueryData(queryKey)).toBeUndefined();
      expect(
        request.mock.calls.some(
          ([, operation]) => operation === "channels_post",
        ),
      ).toBe(false);
      await act(async () => nextSnapshot.resolve(empty));
      await waitFor(() => expect(result.current.data).toEqual(empty));
      await act(async () => {
        await result.current.run({
          operation: "channels_post",
          args: { threadId: "next-thread", text: "new message" },
          withSession: true,
        });
      });
      expect(request).toHaveBeenCalledWith(
        { organizationSlug: context.orgSlug, projectSlug: context.projectSlug },
        "channels_post",
        {
          instanceId: "next-session",
          threadId: "next-thread",
          text: "new message",
        },
      );
      expect(result.current.sessionInstanceId).toBe("next-session");
    },
  );
});
