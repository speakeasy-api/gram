import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { KillswitchUserBadgeLink } from "./KillswitchUserBadgeLink";
import { MCP_TOOL_CALLS_CAPABILITY } from "./killswitch-routing";
import {
  BADGE_REFRESH_INTERVAL_MS,
  canonicalUserId,
  killswitchCreateHref,
  killswitchStatusHref,
  mcpSessionsUserHref,
  useKillswitchUserBadges,
} from "./KillswitchUserStatus";

const state = vi.hoisted(() => ({
  canAccess: true,
  session: "session-1",
  organizationId: "org-1",
  mutationHookFactory: vi.fn(),
  mutateAsync: vi.fn(),
}));

vi.mock("@/hooks/useKillswitchAccess", () => ({
  useKillswitchAccess: () => ({
    canAccess: state.canAccess,
    isLoading: false,
    reason: state.canAccess ? "allowed" : "scope",
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({
    session: state.session,
    organization: { id: state.organizationId },
  }),
}));
vi.mock("@gram/client/react-query/batchKillswitchUserBadges.js", () => ({
  useBatchKillswitchUserBadgesMutation: () => state.mutationHookFactory(),
}));

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});
beforeEach(() => {
  state.canAccess = true;
  state.session = "session-1";
  state.organizationId = "org-1";
  state.mutateAsync.mockReset().mockResolvedValue({ badges: [] });
  state.mutationHookFactory
    .mockReset()
    .mockReturnValue({ mutateAsync: state.mutateAsync });
});

function BadgeHarness({
  userIds,
  enabled = true,
}: {
  userIds: string[];
  enabled?: boolean;
}): JSX.Element {
  const result = useKillswitchUserBadges(userIds, enabled);
  return (
    <>
      <output data-testid="badges">
        {[...result.badges.keys()].join(",")}
      </output>
      <output data-testid="unavailable">
        {[...result.unavailableUserIds].join(",")}
      </output>
      {result.loader}
    </>
  );
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("Killswitch user status", () => {
  it("accepts only canonical user subjects and builds exact contextual URLs", () => {
    expect(canonicalUserId("user:user-1")).toBe("user-1");
    expect(canonicalUserId("apikey:user-1")).toBeUndefined();
    expect(canonicalUserId("anonymous:user-1")).toBeUndefined();
    expect(canonicalUserId("user:user-1:other")).toBeUndefined();

    expect(killswitchStatusHref("/org/killswitch", "user-1")).toBe(
      "/org/killswitch?user=user-1",
    );
    expect(mcpSessionsUserHref("/org/mcp-sessions", "user-1")).toBe(
      "/org/mcp-sessions?subjectUrn=user%3Auser-1",
    );
    expect(
      killswitchCreateHref("/org/killswitch", {
        userId: "user-1",
        capabilityKey: MCP_TOOL_CALLS_CAPABILITY,
        originatingMcpServerId: "server-1",
      }),
    ).toBe(
      "/org/killswitch?create=1&createUser=user-1&createCapability=mcp_tool_calls&originServer=server-1",
    );
  });

  it("bounds chunk concurrency and preserves successes when one chunk fails", async () => {
    const requests = [
      deferred<{
        badges: Array<{
          userId: string;
          affected: boolean;
          affectedNow: boolean;
          scheduled: boolean;
        }>;
      }>(),
      deferred<{
        badges: Array<{
          userId: string;
          affected: boolean;
          affectedNow: boolean;
          scheduled: boolean;
        }>;
      }>(),
      deferred<{
        badges: Array<{
          userId: string;
          affected: boolean;
          affectedNow: boolean;
          scheduled: boolean;
        }>;
      }>(),
    ];
    state.mutateAsync.mockImplementation(
      () => requests[state.mutateAsync.mock.calls.length - 1]?.promise,
    );
    const uniqueIds = Array.from({ length: 205 }, (_, i) => `user-${i + 1}`);
    render(
      <StrictMode>
        <BadgeHarness userIds={[...uniqueIds, "user-1", "user-205"]} />
      </StrictMode>,
    );

    await waitFor(() => expect(state.mutateAsync).toHaveBeenCalledTimes(2));
    const firstTwoChunks = state.mutateAsync.mock.calls.map(
      ([request]) =>
        request.request.killswitchBatchUserBadgesRequest.userIds as string[],
    );
    expect(firstTwoChunks.every((chunk) => chunk.length <= 100)).toBe(true);

    await act(async () => {
      requests[0]!.resolve({
        badges: [
          {
            userId: firstTwoChunks[0]![0]!,
            affected: true,
            affectedNow: true,
            scheduled: false,
          },
        ],
      });
      requests[1]!.reject(new Error("chunk unavailable"));
    });
    await waitFor(() => expect(state.mutateAsync).toHaveBeenCalledTimes(3));
    const thirdChunk = state.mutateAsync.mock.calls[2]![0].request
      .killswitchBatchUserBadgesRequest.userIds as string[];
    await act(async () => {
      requests[2]!.resolve({
        badges: [
          {
            userId: thirdChunk[0]!,
            affected: true,
            affectedNow: false,
            scheduled: true,
          },
        ],
      });
    });

    await waitFor(() =>
      expect(screen.getByTestId("badges").textContent).toContain(
        thirdChunk[0]!,
      ),
    );
    expect(screen.getByTestId("badges").textContent?.split(",")).toContain(
      "user-1",
    );
    expect(screen.getByTestId("unavailable").textContent?.split(",")).toEqual(
      firstTwoChunks[1],
    );
    expect(
      state.mutateAsync.mock.calls.flatMap(
        ([request]) => request.request.killswitchBatchUserBadgesRequest.userIds,
      ),
    ).toEqual([...uniqueIds].sort());
  });

  it("keys results by session and organization and suppresses them when disabled or denied", async () => {
    state.mutateAsync.mockResolvedValue({
      badges: [
        {
          userId: "user-1",
          affected: true,
          affectedNow: true,
          scheduled: false,
        },
      ],
    });
    const view = render(<BadgeHarness userIds={["user-1"]} />);
    await waitFor(() =>
      expect(screen.getByTestId("badges").textContent).toBe("user-1"),
    );

    view.rerender(<BadgeHarness userIds={["user-1"]} enabled={false} />);
    expect(screen.getByTestId("badges").textContent).toBe("");

    view.rerender(<BadgeHarness userIds={["user-1"]} />);
    await waitFor(() => expect(state.mutateAsync).toHaveBeenCalledTimes(2));
    state.organizationId = "org-2";
    view.rerender(<BadgeHarness userIds={["user-1"]} />);
    expect(screen.getByTestId("badges").textContent).toBe("");
    await waitFor(() => expect(state.mutateAsync).toHaveBeenCalledTimes(3));

    state.session = "session-2";
    view.rerender(<BadgeHarness userIds={["user-1"]} />);
    expect(screen.getByTestId("badges").textContent).toBe("");
    await waitFor(() => expect(state.mutateAsync).toHaveBeenCalledTimes(4));

    state.canAccess = false;
    view.rerender(<BadgeHarness userIds={["user-1"]} />);
    expect(screen.getByTestId("badges").textContent).toBe("");
  });

  it("refreshes status on a bounded interval", async () => {
    vi.useFakeTimers();
    render(<BadgeHarness userIds={["user-1"]} />);
    await act(async () => Promise.resolve());
    expect(state.mutateAsync).toHaveBeenCalledTimes(1);

    await act(async () => {
      vi.advanceTimersByTime(BADGE_REFRESH_INTERVAL_MS);
      await Promise.resolve();
    });
    expect(state.mutateAsync).toHaveBeenCalledTimes(2);
  });

  it("does not instantiate the badge API hook before access is allowed", () => {
    state.canAccess = false;
    render(<BadgeHarness userIds={["user-1"]} />);
    expect(state.mutationHookFactory).not.toHaveBeenCalled();
    expect(state.mutateAsync).not.toHaveBeenCalled();
  });

  it("links effective, scheduled, and unavailable states with a 24px hit target", () => {
    const view = render(
      <MemoryRouter>
        <div>
          <KillswitchUserBadgeLink
            href="/org/killswitch?user=user-1"
            badge={{
              userId: "user-1",
              affected: true,
              affectedNow: true,
              scheduled: true,
            }}
          />
          <KillswitchUserBadgeLink
            href="/org/killswitch?user=user-2"
            badge={{
              userId: "user-2",
              affected: true,
              affectedNow: false,
              scheduled: true,
            }}
          />
          <KillswitchUserBadgeLink
            href="/org/killswitch?user=user-3"
            unavailable
          />
        </div>
      </MemoryRouter>,
    );
    const links = view.getAllByRole("link");
    expect(links.map((link) => link.textContent)).toEqual([
      "Killswitched",
      "Scheduled killswitch",
      "Killswitch status unavailable",
    ]);
    expect(links.every((link) => link.classList.contains("min-h-6"))).toBe(
      true,
    );
  });
});
