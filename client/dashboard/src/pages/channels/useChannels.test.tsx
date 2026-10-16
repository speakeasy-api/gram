import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { channelsRequest, type ChannelsSnapshot } from "./api";
import { useChannelsWorkspace } from "./useChannels";

vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  channelsRequest: vi.fn(),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "test-project",
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

beforeEach(() => {
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
});
