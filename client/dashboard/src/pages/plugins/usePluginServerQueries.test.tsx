import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  projectId: "project-a",
  session: "session-a",
  grants: [] as Array<{
    scope: string;
    selectors?: Array<Record<string, string>>;
  }>,
  toolsets: vi.fn(),
  servers: vi.fn(),
  endpoints: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: state.session }),
  useProject: () => ({ id: state.projectId, slug: state.projectId }),
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => ({ grants: state.grants }),
}));
vi.mock("@gram/client/react-query/listToolsets", () => ({
  useListToolsets: state.toolsets,
}));
vi.mock("@gram/client/react-query/mcpServers", () => ({
  useMcpServers: state.servers,
}));
vi.mock("@gram/client/react-query/mcpEndpoints", () => ({
  useMcpEndpoints: state.endpoints,
}));
import { usePluginServerQueries } from "./usePluginServerQueries";
beforeEach(() => {
  vi.clearAllMocks();
  state.projectId = "project-a";
  state.session = "session-a";
});
describe("plugin server query permissions", () => {
  it("discards cached privileged metadata after project or permission changes", () => {
    state.projectId = "project-a";
    state.grants = [
      { scope: "mcp:read", selectors: [{ projectId: "project-a" }] },
    ];
    for (const query of [state.toolsets, state.servers, state.endpoints])
      query.mockReturnValue({ data: { secret: "cached-admin-metadata" } });
    const { result, rerender } = renderHook(usePluginServerQueries);
    expect(result.current.toolsetsQuery.data).toBeDefined();
    state.projectId = "project-b";
    rerender();
    expect(result.current.toolsetsQuery.data).toBeUndefined();
    expect(result.current.serversQuery.data).toBeUndefined();
    expect(result.current.endpointsQuery.data).toBeUndefined();
    state.projectId = "project-a";
    state.grants = [];
    state.session = "session-b";
    rerender();
    expect(state.toolsets).toHaveBeenLastCalledWith(
      { gramProject: "project-a", gramSession: "session-b" },
      undefined,
      { enabled: false },
    );
    expect(result.current.toolsetsQuery.data).toBeUndefined();
  });
  it("does not request MCP APIs for org-read-only viewers", () => {
    state.grants = [{ scope: "org:read" }];
    renderHook(usePluginServerQueries);
    expect(state.toolsets).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      {
        enabled: false,
      },
    );
    expect(state.servers).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      {
        enabled: false,
      },
    );
    expect(state.endpoints).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      {
        enabled: false,
      },
    );
  });
  it("keeps endpoint listing disabled for one MCP resource grant", () => {
    state.grants = [
      {
        scope: "mcp:read",
        selectors: [{ projectId: "project-a", resourceId: "server-a" }],
      },
    ];
    renderHook(usePluginServerQueries);
    expect(state.toolsets).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      { enabled: true },
    );
    expect(state.servers).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      {
        enabled: true,
      },
    );
    expect(state.endpoints).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      {
        enabled: false,
      },
    );
  });
  it("allows project-wide MCP discovery", () => {
    state.grants = [
      { scope: "mcp:read", selectors: [{ projectId: "project-a" }] },
    ];
    renderHook(usePluginServerQueries);
    expect(state.toolsets).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      {
        enabled: true,
      },
    );
    expect(state.servers).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      { enabled: true },
    );
    expect(state.endpoints).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      {
        enabled: true,
      },
    );
  });
  it("does not use MCP grants from another project", () => {
    state.grants = [
      { scope: "mcp:read", selectors: [{ projectId: "project-b" }] },
    ];
    renderHook(usePluginServerQueries);
    expect(state.toolsets).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      { enabled: false },
    );
    expect(state.servers).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      {
        enabled: false,
      },
    );
    expect(state.endpoints).toHaveBeenCalledWith(
      { gramProject: "project-a", gramSession: "session-a" },
      undefined,
      {
        enabled: false,
      },
    );
  });
});
