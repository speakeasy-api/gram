import { agentRestrictionLabel } from "./fleet-model";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import type { KillswitchSchedule } from "@gram/client/models/components/killswitchschedule.js";
import { BrowserRouter, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AgentRestrictions, AgentRestrictionRecord } from "./AgentRestrictions";

const mocks = vi.hoisted(() => ({
  error: undefined as Error | undefined,
  agentId: "agent-12345678",
  list: vi.fn(),
  refetch: vi.fn(),
  schedule: { start: "now", end: "until_lifted" } as KillswitchSchedule,
  isLoading: false,
  hasNextPage: false,
  fetchNextPage: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session" }),
}));
vi.mock("@gram/client/react-query/killswitches.js", () => ({
  useKillswitchesInfinite: (...args: unknown[]) => {
    mocks.list(...args);
    return {
      error: mocks.error,
      isLoading: mocks.isLoading,
      hasNextPage: mocks.hasNextPage,
      isFetchingNextPage: false,
      fetchNextPage: mocks.fetchNextPage,
      data: mocks.error
        ? undefined
        : {
            pages: [
              {
                result: {
                  items: [
                    {
                      id: "restriction-1",
                      agentId: mocks.agentId,
                      status: "active",
                      schedule: mocks.schedule,
                      scope: { type: "all_servers" },
                    },
                  ],
                },
              },
            ],
          },
      refetch: mocks.refetch,
    };
  },
}));
vi.mock("@gram/client/react-query/killswitch.js", () => ({
  useKillswitch: () => ({
    data: { principalKind: "agent", agentId: mocks.agentId },
  }),
}));
vi.mock("@gram/client/react-query/killswitchMCPServers.js", () => ({
  useKillswitchMCPServers: () => ({ data: { servers: [] } }),
}));
vi.mock("@/components/killswitch/KillswitchRecord", () => ({
  KillswitchRecord: ({
    subjectAgent,
    onClose,
  }: {
    subjectAgent: { name: string; canEdit: boolean };
    onClose: () => void;
  }) => (
    <section>
      <h1>{subjectAgent.name}</h1>
      <button>Release restriction</button>
      {subjectAgent.canEdit && <button>Edit</button>}
      <button onClick={onClose}>Close</button>
    </section>
  ),
}));
function Location() {
  return <output>{useLocation().search}</output>;
}
function list() {
  window.history.replaceState(null, "", "/?selected=agent:a&source=agent");
  return render(
    <BrowserRouter>
      <AgentRestrictions agents={[]} inventoryAvailable={false} />
      <Location />
    </BrowserRouter>,
  );
}
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});
beforeEach(() => {
  window.history.replaceState(null, "", "/");
  mocks.error = undefined;
  mocks.schedule = { start: "now", end: "until_lifted" };
  mocks.isLoading = false;
  mocks.hasNextPage = false;
  vi.clearAllMocks();
});

describe("Agent restriction recovery", () => {
  it("refreshes status at a loaded restriction's schedule boundary", async () => {
    vi.useFakeTimers();
    const now = new Date("2026-09-01T12:00:00Z");
    vi.setSystemTime(now);
    mocks.schedule = {
      start: "scheduled",
      startsAt: new Date(now.getTime() + 1_000),
      end: "until_lifted",
    };
    list();
    await act(() => vi.advanceTimersByTime(999));
    expect(mocks.refetch).not.toHaveBeenCalled();
    await act(() => vi.advanceTimersByTime(101));
    expect(mocks.refetch).toHaveBeenCalledOnce();
  });
  it("loads the next page without treating the loaded count as the total", () => {
    mocks.hasNextPage = true;
    list();
    expect(screen.getByText("1 loaded restriction")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Load more restrictions" }),
    );
    expect(mocks.fetchNextPage).toHaveBeenCalledOnce();
  });
  it("keeps failures distinct from an empty list", () => {
    mocks.error = new Error("unavailable");
    list();
    expect(screen.getByRole("alert").textContent).toContain("Couldn’t refresh");
    expect(screen.queryByText("No agent restrictions.")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(mocks.refetch).toHaveBeenCalled();
  });
  it("opens one exact restriction and preserves origin params when closing", () => {
    list();
    expect(mocks.list.mock.calls[0]?.[1]).toMatchObject({
      principalKind: "agent",
    });
    fireEvent.click(screen.getByRole("button", { name: "View restriction" }));
    expect(
      screen.getAllByRole("button", { name: "Release restriction" }),
    ).toHaveLength(1);
    expect(screen.getByRole("status").textContent).toContain(
      "restriction=restriction-1",
    );
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.getByRole("status").textContent).toBe(
      "?selected=agent%3Aa&source=agent",
    );
  });
  it("refuses another agent's restriction in a scoped inspector", () => {
    const close = vi.fn();
    render(
      <BrowserRouter>
        <AgentRestrictionRecord
          id="restriction-1"
          expectedAgentId="different"
          agents={[]}
          inventoryAvailable={false}
          onSelect={() => {}}
          onClose={() => {
            close();
          }}
        />
      </BrowserRouter>,
    );
    expect(screen.getByRole("alert").textContent).toContain("different agent");
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(close).toHaveBeenCalledOnce();
    expect(
      screen.queryByRole("button", { name: "Release restriction" }),
    ).toBeNull();
  });
  it("uses cautious identity labels and permits release without inventory", () => {
    expect(agentRestrictionLabel(mocks.agentId, [], false)).toBe(
      "Agent · agent-12",
    );
    expect(agentRestrictionLabel(mocks.agentId, [], true)).toBe(
      "Deleted or unavailable agent · agent-12",
    );
    render(
      <BrowserRouter>
        <AgentRestrictionRecord
          id="restriction-1"
          agents={[]}
          inventoryAvailable={false}
          onSelect={() => {}}
          onClose={() => {}}
        />
      </BrowserRouter>,
    );
    expect(
      screen.getByRole("button", { name: "Release restriction" }),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
  });
});
