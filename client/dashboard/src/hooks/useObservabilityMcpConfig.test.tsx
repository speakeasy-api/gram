import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  grants: [] as Array<{
    scope: string;
    selectors?: Array<Record<string, string>>;
  }>,
  toolsets: vi.fn(() => ({})),
  servers: vi.fn(() => ({})),
  endpoints: vi.fn(() => ({})),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ projects: [{ id: "project-a", slug: "example" }] }),
  useSession: () => ({ session: "" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ projectSlug: "example" }),
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => ({ grants: state.grants }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/listToolsets.js", () => ({
  useListToolsets: state.toolsets,
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: state.servers,
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: state.endpoints,
}));
import {
  useNoToolsetsConfigured,
  useObservabilityMcpConfig,
} from "./useObservabilityMcpConfig";
beforeEach(() => {
  vi.clearAllMocks();
  state.toolsets.mockReturnValue({});
  state.servers.mockReturnValue({});
  state.endpoints.mockReturnValue({});
});
describe("shared insights MCP requests", () => {
  it("removes cached entries when discovery access is revoked", () => {
    state.grants = [{ scope: "mcp:read" }];
    state.toolsets.mockReturnValue({
      data: { toolsets: [{ slug: "tools", mcpSlug: "tools" }] },
    });
    state.servers.mockReturnValue({ data: { mcpServers: [] } });
    state.endpoints.mockReturnValue({ data: { mcpEndpoints: [] } });
    const { result, rerender } = renderHook(() =>
      useObservabilityMcpConfig({ toolsToInclude: () => true }),
    );
    expect(result.current.mcps).toHaveLength(1);
    state.grants = [];
    rerender();
    expect(result.current.mcps).toEqual([]);
  });

  it("drops cached endpoints but retains readable toolsets for a server-specific grant", () => {
    state.grants = [{ scope: "mcp:read" }];
    state.toolsets.mockReturnValue({
      data: { toolsets: [{ slug: "tools", mcpSlug: "tools" }] },
    });
    state.servers.mockReturnValue({
      data: { mcpServers: [{ id: "server-a", slug: "server" }] },
    });
    state.endpoints.mockReturnValue({
      data: { mcpEndpoints: [{ slug: "endpoint", mcpServerId: "server-a" }] },
    });
    const { result, rerender } = renderHook(() =>
      useObservabilityMcpConfig({ toolsToInclude: () => true }),
    );
    expect(result.current.mcps).toHaveLength(2);
    state.grants = [
      {
        scope: "mcp:read",
        selectors: [{ resourceId: "server-a", projectId: "project-a" }],
      },
    ];
    rerender();
    expect(result.current.mcps).toHaveLength(1);
    expect(result.current.mcps?.[0]?.name).toBe("tools");
  });
  it("disables every MCP request for skill-only viewers", () => {
    state.grants = [{ scope: "skill:read" }, { scope: "skill:write" }];
    renderHook(() => useObservabilityMcpConfig({ toolsToInclude: () => true }));
    renderHook(() => useNoToolsetsConfigured("example"));
    expect(state.toolsets).toHaveBeenLastCalledWith(
      { gramProject: "example" },
      undefined,
      { enabled: false },
    );
    expect(state.servers).toHaveBeenLastCalledWith(
      { gramProject: "example" },
      undefined,
      { enabled: false },
    );
    expect(state.endpoints).toHaveBeenCalledWith(
      { gramProject: "example" },
      undefined,
      { enabled: false },
    );
  });
  it("preserves discovery for viewers with MCP access on the target project", () => {
    state.grants = [
      { scope: "mcp:read", selectors: [{ projectId: "project-a" }] },
    ];
    renderHook(() => useObservabilityMcpConfig({ toolsToInclude: () => true }));
    expect(state.toolsets).toHaveBeenCalledWith(
      { gramProject: "example" },
      undefined,
      { enabled: true },
    );
    expect(state.endpoints).toHaveBeenCalledWith(
      { gramProject: "example" },
      undefined,
      { enabled: true },
    );
  });
});
