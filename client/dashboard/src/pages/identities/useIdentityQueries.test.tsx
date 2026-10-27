import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useAgentChats, useCanReadAgentChats } from "./useIdentityQueries";

const mocks = vi.hoisted(() => ({ chatRead: false, listChats: vi.fn() }));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "owner" } }),
  useProject: () => ({ id: "project", slug: "default" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScopeInProject: () => mocks.chatRead }),
}));
vi.mock("@gram/client/react-query/listChats.js", () => ({
  useListChats: (...args: unknown[]) => mocks.listChats(...args),
}));
const agent = { id: "agent", ownerUserId: "owner" } as ManagedAgent;
const from = new Date("2026-01-01T00:00:00Z");
const to = new Date("2026-01-08T00:00:00Z");
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.chatRead = false;
});

it("lets the owner without chat:read load only the selected agent's sessions", () => {
  const { result } = renderHook(() => {
    useAgentChats(agent, from, to);
    return useCanReadAgentChats(agent);
  });
  expect(result.current).toBe(true);
  expect(mocks.listChats).toHaveBeenCalledWith(
    { agentId: "agent", from, to, limit: 5, gramProject: "default" },
    undefined,
    { enabled: true, throwOnError: false },
  );
});

it("stops querying when an owned agent is latched for reassignment", () => {
  const { result, rerender } = renderHook(
    ({ target }) => {
      useAgentChats(target, from, to);
      return useCanReadAgentChats(target);
    },
    { initialProps: { target: agent } },
  );
  expect(result.current).toBe(true);
  rerender({ target: { ...agent, ownerReassignmentRequiredAt: new Date() } });
  expect(result.current).toBe(false);
  expect(mocks.listChats).toHaveBeenLastCalledWith(
    expect.anything(),
    undefined,
    { enabled: false, throwOnError: false },
  );
});

it("does not treat another agent's owner as the viewer", () => {
  const { result } = renderHook(() =>
    useCanReadAgentChats({ ...agent, ownerUserId: "other-owner" }),
  );
  expect(result.current).toBe(false);
});

it("allows an explicit chat reader after owner reassignment", () => {
  mocks.chatRead = true;
  const { result } = renderHook(() =>
    useCanReadAgentChats({ ...agent, ownerReassignmentRequiredAt: new Date() }),
  );
  expect(result.current).toBe(true);
});
