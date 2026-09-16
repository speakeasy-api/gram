import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

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
import { useGatewayMemberRows } from "./useGatewayMemberRows";

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
