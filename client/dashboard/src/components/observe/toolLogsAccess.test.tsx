import { renderHook } from "@testing-library/react";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FacetGroup } from "./LogsFacetRail";
import {
  isOwnScopeSession,
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

  it("reports grants still loading so callers can hold their notice", () => {
    state.grantsLoading = true;

    expect(renderHook(useToolLogsAccess).result.current).toEqual({
      projectWide: false,
      isLoading: true,
    });
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
  const groups: FacetGroup[] = [
    {
      id: "server",
      label: "Server",
      values: [{ value: "hosted:a", label: "A", count: 12, selected: false }],
    },
    {
      id: "user",
      label: "User",
      values: [
        { value: "viewer@example.com", label: "Me", count: 3, selected: false },
        {
          value: "other@example.com",
          label: "Other",
          count: 9,
          selected: true,
        },
      ],
    },
    {
      id: "agent",
      label: "Agent",
      values: [
        { value: "agent-1", label: "Mine", count: 2, selected: false },
        { value: "agent-2", label: "Theirs", count: 5, selected: false },
      ],
    },
    {
      id: "role",
      label: "Role",
      values: [{ value: "role-1", label: "Admins", selected: false }],
    },
  ];

  it("offers only the viewer and their agents, without project-wide counts", () => {
    const narrowed = ownScopeFacetGroups(
      groups,
      "viewer@example.com",
      new Set(["agent-1"]),
    );

    expect(narrowed.map((group) => group.id)).toEqual([
      "server",
      "user",
      "agent",
    ]);
    expect(narrowed.flatMap((group) => group.values)).toEqual([
      { value: "hosted:a", label: "A", selected: false },
      { value: "viewer@example.com", label: "Me", selected: false },
      { value: "agent-1", label: "Mine", selected: false },
    ]);
  });
});
