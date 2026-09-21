import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const queries = vi.hoisted(() => ({
  members: {
    data: { members: [] },
    isLoading: false,
    isError: false,
    isFetching: false,
    dataUpdatedAt: 1,
    refetch: vi.fn(),
  },
  servers: {
    data: { mcpServers: [] },
    isFetching: false,
    dataUpdatedAt: 1,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  },
}));
vi.mock("@gram/client/react-query/metaMcpMembers.js", () => ({
  useMetaMcpMembers: () => queries.members,
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => queries.servers,
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project",
}));
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { addCandidateBatch } from "./memberRows";
import {
  useReconcileWrappers,
  useGatewayMemberRows,
} from "./useGatewayMemberRows";

beforeEach(() => {
  queries.members.data = { members: [] };
  queries.servers.data = { mcpServers: [] };
  for (const query of [queries.members, queries.servers]) {
    query.isFetching = false;
    query.isLoading = false;
    query.isError = false;
    query.dataUpdatedAt = 1;
    query.refetch.mockReset();
  }
});
afterEach(cleanup);

it("exposes only successful settled membership freshness, even with unchanged rows", () => {
  const { result, rerender } = renderHook(() =>
    useGatewayMemberRows("gateway"),
  );
  const rows = result.current.rows;
  expect(result.current.membersUpdatedAt).toBe(1);
  queries.members.isFetching = true;
  rerender();
  expect(result.current.membersUpdatedAt).toBe(0);
  queries.members.isFetching = false;
  queries.members.isError = true;
  rerender();
  expect(result.current.membersUpdatedAt).toBe(0);
  queries.members.isError = false;
  queries.members.dataUpdatedAt = 2;
  rerender();
  expect(result.current.rows).toBe(rows);
  expect(result.current.membersUpdatedAt).toBe(2);
  // A server-list refresh must not masquerade as a membership refresh.
  queries.servers.data = { mcpServers: [] };
  rerender();
  expect(result.current.membersUpdatedAt).toBe(2);
});

it("exposes only successful settled full server-list freshness", () => {
  const { result, rerender } = renderHook(() =>
    useGatewayMemberRows("gateway"),
  );
  expect(result.current.serversUpdatedAt).toBe(1);
  for (const flag of ["isFetching", "isError", "isLoading"] as const) {
    queries.servers[flag] = true;
    rerender();
    expect(result.current.serversUpdatedAt).toBe(0);
    queries.servers[flag] = false;
  }
  queries.servers.dataUpdatedAt = 2;
  rerender();
  expect(result.current.serversUpdatedAt).toBe(2);
});

it("replaces deleted wrappers after fresh lists, retaining live wrappers and failed retry slots", async () => {
  const state = {
    wrappers: new Map<string, string>(),
    orders: new Map<string, number>(),
    completed: new Set<string>(),
  };
  const candidate = {
    kind: "toolset" as const,
    toolset: { id: "tools", name: "Hosted" } as ToolsetEntry,
  };
  const createWrapper = vi
    .fn()
    .mockResolvedValueOnce("deleted")
    .mockResolvedValue("replacement");
  const attach = vi.fn().mockRejectedValueOnce(new Error("Attach failed"));
  const { result, rerender } = renderHook(
    ({ servers, updatedAt, busy }) =>
      useReconcileWrappers(state, servers, updatedAt, busy),
    { initialProps: { servers: [] as McpServer[], updatedAt: 1, busy: false } },
  );
  rerender({ servers: [], updatedAt: 1, busy: true });
  await addCandidateBatch([candidate], 4, state, { createWrapper, attach });
  // A successful response during writes may predate wrapper creation.
  rerender({ servers: [], updatedAt: 2, busy: true });
  expect(state.wrappers.get("toolset-tools")).toBe("deleted");
  result.current(); // Writes settled, before invalidation/refetch.
  rerender({ servers: [], updatedAt: 2, busy: false });
  expect(state.wrappers.get("toolset-tools")).toBe("deleted");
  // Failed/loading/in-flight queries expose zero freshness; cached data stays protected.
  rerender({ servers: [], updatedAt: 0, busy: false });
  expect(state.wrappers.get("toolset-tools")).toBe("deleted");
  rerender({
    servers: [{ id: "deleted" } as McpServer],
    updatedAt: 3,
    busy: false,
  });
  expect(state.wrappers.get("toolset-tools")).toBe("deleted");
  // Another tab deletes the wrapper; an authoritative full refresh now omits it.
  rerender({ servers: [], updatedAt: 4, busy: true });
  expect(state.wrappers.get("toolset-tools")).toBe("deleted");
  rerender({ servers: [], updatedAt: 4, busy: false });
  expect(state.wrappers.has("toolset-tools")).toBe(false);
  expect(state.orders.get("toolset-tools")).toBe(4);
  await addCandidateBatch([candidate], 8, state, { createWrapper, attach });
  expect(createWrapper).toHaveBeenCalledTimes(2);
  expect(attach).toHaveBeenLastCalledWith("replacement", 4);
  // Deleting a completed wrapper must also retire its optimistic success.
  rerender({ servers: [], updatedAt: 5, busy: false });
  expect(state.completed.size).toBe(0);
  expect(state.orders.size).toBe(0);
});
