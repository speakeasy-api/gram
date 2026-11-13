import { renderHook } from "@testing-library/react";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FacetGroup } from "./LogsFacetRail";
import {
  isOwnScopeSession,
  isOwnedAgent,
  ownScopeFacetGroups,
  ownedAgentIds,
  useOwnedAgentIds,
  useToolLogsAccess,
} from "./toolLogsAccess";

const state = vi.hoisted(() => ({
  projectId: "project-1",
  grants: new Set<string>(),
  grantsLoading: false,
  agents: [] as Partial<ManagedAgent>[],
  agentsError: false,
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: state.projectId }),
  useSession: () => ({ user: { id: "viewer", email: "viewer@example.com" } }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: state.grantsLoading,
    hasScope: (scope: string, resourceId?: string) =>
      state.grants.has(`${scope}@${resourceId ?? ""}`),
  }),
}));

vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: () => ({
    data: state.agents,
    isError: state.agentsError,
  }),
}));

function agent(overrides: Partial<ManagedAgent>): ManagedAgent {
  return {
    id: "agent-1",
    ownerUserId: "viewer",
    ...overrides,
  } as ManagedAgent;
}

beforeEach(() => {
  state.projectId = "project-1";
  state.grants = new Set();
  state.grantsLoading = false;
  state.agents = [];
  state.agentsError = false;
});

describe("useToolLogsAccess", () => {
  it("is project-wide only with log:read on the active project", () => {
    expect(renderHook(useToolLogsAccess).result.current.projectWide).toBe(
      false,
    );

    state.grants.add("log:read@project-1");
    expect(renderHook(useToolLogsAccess).result.current.projectWide).toBe(true);
  });

  it("never treats a missing project as every project", () => {
    state.projectId = "";
    state.grants.add("log:read@");

    expect(renderHook(useToolLogsAccess).result.current.projectWide).toBe(
      false,
    );
  });

  it("opens the page to project readers only", () => {
    expect(renderHook(useToolLogsAccess).result.current.canOpen).toBe(false);

    state.grants.add("project:read@project-1");
    expect(renderHook(useToolLogsAccess).result.current.canOpen).toBe(true);
  });

  it("reports grants still loading, with nothing granted until they arrive", () => {
    state.grantsLoading = true;

    expect(renderHook(useToolLogsAccess).result.current).toEqual({
      canOpen: false,
      projectWide: false,
      isLoading: true,
    });
  });
});

describe("isOwnedAgent", () => {
  it("is the owner's unless the agent is latched", () => {
    expect(isOwnedAgent(agent({}), "viewer")).toBe(true);
    expect(isOwnedAgent(agent({ ownerUserId: "someone-else" }), "viewer")).toBe(
      false,
    );
    expect(
      isOwnedAgent(
        agent({ ownerReassignmentRequiredAt: new Date("2026-01-01") }),
        "viewer",
      ),
    ).toBe(false);
  });
});

describe("ownedAgentIds", () => {
  it("keeps the viewer's agents and drops latched or other people's", () => {
    const owned = ownedAgentIds(
      [
        agent({ id: "mine" }),
        agent({ id: "theirs", ownerUserId: "someone-else" }),
        agent({
          id: "latched",
          ownerReassignmentRequiredAt: new Date("2026-01-01T00:00:00Z"),
        }),
      ],
      "viewer",
    );

    expect([...owned]).toEqual(["mine"]);
  });

  it("reads the agents through the hook and ignores a failed list", () => {
    state.agents = [agent({ id: "mine" })];
    expect([
      ...renderHook(() => useOwnedAgentIds(true)).result.current,
    ]).toEqual(["mine"]);

    state.agentsError = true;
    expect(renderHook(() => useOwnedAgentIds(true)).result.current.size).toBe(
      0,
    );
  });
});

describe("isOwnScopeSession", () => {
  const owned = new Set(["agent-1"]);

  it("accepts the viewer's own session", () => {
    expect(isOwnScopeSession({ userId: "viewer" }, "viewer", owned)).toBe(true);
  });

  it("accepts a session driven by an agent the viewer owns", () => {
    expect(
      isOwnScopeSession({ userId: "", agentId: "agent-1" }, "viewer", owned),
    ).toBe(true);
  });

  it("refuses other people's sessions and agents, and a session not loaded yet", () => {
    expect(isOwnScopeSession({ userId: "other" }, "viewer", owned)).toBe(false);
    expect(
      isOwnScopeSession(
        { userId: "other", agentId: "agent-2" },
        "viewer",
        owned,
      ),
    ).toBe(false);
    expect(isOwnScopeSession(undefined, "viewer", owned)).toBe(false);
  });

  it("does not match an empty owner against an empty viewer", () => {
    expect(isOwnScopeSession({ userId: "" }, "", owned)).toBe(false);
  });
});

describe("ownScopeFacetGroups", () => {
  // What the page builds in own scope: nothing fetched, so a group carries
  // only a link's selection, with a zero count, as the page adds it back.
  const groups: FacetGroup[] = [
    {
      id: "server",
      label: "Server",
      values: [
        { value: "hosted:a", label: "hosted:a", count: 0, selected: true },
      ],
    },
    { id: "client", label: "Client", values: [] },
    {
      id: "user",
      label: "User",
      values: [
        {
          value: "viewer@example.com",
          label: "viewer@example.com",
          count: 0,
          selected: true,
        },
        {
          value: "other@example.com",
          label: "other@example.com",
          count: 0,
          selected: true,
        },
      ],
    },
    { id: "agent", label: "Agent", values: [] },
    {
      id: "role",
      label: "Role",
      values: [{ value: "role-1", label: "Admins", selected: false }],
    },
  ];
  const own = {
    user: [{ value: "viewer@example.com", label: "Me", selected: true }],
    agent: [{ value: "agent-1", label: "Mine", selected: false }],
  };

  it("offers the viewer and their agents, keeps a link's selection, and drops roles, counts and empty groups", () => {
    const narrowed = ownScopeFacetGroups(groups, own);

    expect(narrowed.map((group) => group.id)).toEqual([
      "server",
      "user",
      "agent",
    ]);
    expect(narrowed.flatMap((group) => group.values)).toEqual([
      { value: "hosted:a", label: "hosted:a", selected: true },
      { value: "viewer@example.com", label: "Me", selected: true },
      {
        value: "other@example.com",
        label: "other@example.com",
        selected: true,
      },
      { value: "agent-1", label: "Mine", selected: false },
    ]);
  });
});
