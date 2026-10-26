import { renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useKeysMissingPermissions } from "./useAgentKeyPermissions";

const mocks = vi.hoisted(() => ({ policyError: false, keysError: false }));
const key = {
  id: "key",
  name: "Build key",
  createdAt: new Date("2026-01-01"),
  delegatedGrants: {
    requested: [
      {
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "server" },
      },
    ],
  },
};
const policy = [
  {
    effect: "allow",
    scope: "project:read",
    selector: { resourceKind: "project", resourceId: "*" },
    createdAt: new Date("2026-02-01"),
    updatedAt: new Date("2026-02-01"),
  },
];
vi.mock("@/contexts/Auth", () => ({ useOrganization: () => ({ id: "org" }) }));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ agents: { listPolicyGrants: vi.fn() } }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: policy, isError: mocks.policyError }),
}));
vi.mock("@gram/client/react-query/listAPIKeys", () => ({
  useListAPIKeys: () => ({ data: { keys: [key] }, isError: mocks.keysError }),
}));
const agent = {
  id: "agent",
  permissions: { write: true, authorize: true },
} as ManagedAgent;
beforeEach(() => {
  mocks.policyError = false;
  mocks.keysError = false;
});

it("reports a key missing a later permission when both reads are authorized", () => {
  const { result } = renderHook(() => useKeysMissingPermissions(agent));
  expect(result.current).toEqual([key]);
});
it.each(["write", "authorize"] as const)(
  "hides retained key data after losing %s permission",
  (permission) => {
    const { result } = renderHook(() =>
      useKeysMissingPermissions({
        ...agent,
        permissions: { ...agent.permissions, [permission]: false },
      }),
    );
    expect(result.current).toEqual([]);
  },
);
it.each(["policyError", "keysError"] as const)(
  "hides retained data after %s",
  (failure) => {
    mocks[failure] = true;
    const { result } = renderHook(() => useKeysMissingPermissions(agent));
    expect(result.current).toEqual([]);
  },
);
