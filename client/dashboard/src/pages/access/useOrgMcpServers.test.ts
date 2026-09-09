import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useOrgMcpServers } from "./useOrgMcpServers";

const mocks = vi.hoisted(() => ({
  toolsets: {} as Record<string, unknown>,
  mcpServers: {} as Record<string, unknown>,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org_example",
    projects: [{ id: "project_one", name: "Project one", slug: "project-one" }],
  }),
}));
vi.mock("@gram/client/react-query/listToolsetsForOrg.js", () => ({
  useListToolsetsForOrg: () => mocks.toolsets,
}));
vi.mock("@gram/client/react-query/listMcpServersForOrg.js", () => ({
  useListMcpServersForOrg: () => mocks.mcpServers,
}));

const toolset = {
  id: "server_one",
  name: "Server one",
  slug: "server-one",
  projectId: "project_one",
  tools: [{ id: "tool_one", name: "search", type: "http" }],
};
const mcpServerRow = {
  id: "server_remote",
  projectId: "project_one",
  name: "Remote server",
  slug: "remote-server",
  remoteMcpServerId: "remote_one",
};

function half(overrides: Record<string, unknown>) {
  return {
    data: undefined,
    isSuccess: false,
    isError: false,
    refetch: vi.fn(),
    ...overrides,
  };
}

afterEach(() => vi.clearAllMocks());

describe("org MCP server inventory", () => {
  it("publishes rows only once both halves succeed", () => {
    mocks.toolsets = half({ data: { toolsets: [toolset] }, isSuccess: true });
    mocks.mcpServers = half({
      data: { mcpServers: [mcpServerRow] },
      isSuccess: true,
    });
    const { result } = renderHook(() => useOrgMcpServers(true));
    expect(result.current.settled).toBe(true);
    expect(result.current.isError).toBe(false);
    expect(
      result.current.groups.flatMap((group) =>
        group.servers.map((server) => server.id),
      ),
    ).toEqual(["server_one", "server_remote"]);
  });
  it.each(["toolsets", "mcpServers"] as const)(
    "withholds the whole inventory when the %s half fails",
    (failing) => {
      const succeeding = failing === "toolsets" ? "mcpServers" : "toolsets";
      // The succeeding half carries real rows: serving them alone is exactly
      // the partial inventory a picker must never narrow against.
      mocks[succeeding] = half({
        data:
          succeeding === "toolsets"
            ? { toolsets: [toolset] }
            : { mcpServers: [mcpServerRow] },
        isSuccess: true,
      });
      mocks[failing] = half({ isError: true });
      const { result } = renderHook(() => useOrgMcpServers(true));
      expect(result.current.groups).toEqual([]);
      expect(result.current.settled).toBe(false);
      expect(result.current.isError).toBe(true);
    },
  );
  it("withdraws a previously complete inventory when a refetch fails", () => {
    mocks.toolsets = half({ data: { toolsets: [toolset] }, isSuccess: true });
    // React Query keeps the last success alongside the refetch error, so
    // isSuccess alone would keep serving a half the caller can no longer trust.
    mocks.mcpServers = half({
      data: { mcpServers: [mcpServerRow] },
      isSuccess: true,
      isError: true,
    });
    const { result } = renderHook(() => useOrgMcpServers(true));
    expect(result.current.groups).toEqual([]);
    expect(result.current.isError).toBe(true);
  });
  it("reports no error while disabled, even after a cached failure", () => {
    mocks.toolsets = half({ isError: true });
    mocks.mcpServers = half({ isSuccess: true, data: { mcpServers: [] } });
    const { result, rerender } = renderHook(({ on }) => useOrgMcpServers(on), {
      initialProps: { on: true },
    });
    expect(result.current.isError).toBe(true);
    // The query is disabled but keeps its cached error state; a caller that is
    // not reading the inventory must not be told the inventory failed.
    rerender({ on: false });
    expect(result.current.isError).toBe(false);
    expect(result.current.groups).toEqual([]);
  });
  it("retries both halves", () => {
    mocks.toolsets = half({ isError: true });
    mocks.mcpServers = half({ isSuccess: true, data: { mcpServers: [] } });
    const { result } = renderHook(() => useOrgMcpServers(true));
    result.current.refetch();
    expect(mocks.toolsets.refetch).toHaveBeenCalledTimes(1);
    expect(mocks.mcpServers.refetch).toHaveBeenCalledTimes(1);
  });
  it("reports an unresolved inventory as unsettled, not empty", () => {
    mocks.toolsets = half({});
    mocks.mcpServers = half({});
    const { result } = renderHook(() => useOrgMcpServers(false));
    expect(result.current.groups).toEqual([]);
    expect(result.current.settled).toBe(false);
    expect(result.current.isError).toBe(false);
  });
});
