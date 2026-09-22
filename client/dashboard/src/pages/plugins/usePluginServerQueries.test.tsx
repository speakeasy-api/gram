import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  grants: [] as Array<{
    scope: string;
    selectors?: Array<Record<string, string>>;
  }>,
  toolsets: vi.fn(),
  servers: vi.fn(),
  endpoints: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project-a" }) }));
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
beforeEach(() => vi.clearAllMocks());
describe("plugin server query permissions", () => {
  it("does not request MCP APIs for org-read-only viewers", () => {
    state.grants = [{ scope: "org:read" }];
    renderHook(usePluginServerQueries);
    expect(state.toolsets).toHaveBeenCalledWith(
      { gramProject: "project-a" },
      undefined,
      {
        enabled: false,
      },
    );
    expect(state.servers).toHaveBeenCalledWith(
      { gramProject: "project-a" },
      undefined,
      {
        enabled: false,
      },
    );
    expect(state.endpoints).toHaveBeenCalledWith(
      { gramProject: "project-a" },
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
      { gramProject: "project-a" },
      undefined,
      { enabled: true },
    );
    expect(state.servers).toHaveBeenCalledWith(
      { gramProject: "project-a" },
      undefined,
      {
        enabled: true,
      },
    );
    expect(state.endpoints).toHaveBeenCalledWith(
      { gramProject: "project-a" },
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
      { gramProject: "project-a" },
      undefined,
      {
        enabled: true,
      },
    );
    expect(state.servers).toHaveBeenCalledWith(
      { gramProject: "project-a" },
      undefined,
      { enabled: true },
    );
    expect(state.endpoints).toHaveBeenCalledWith(
      { gramProject: "project-a" },
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
      { gramProject: "project-a" },
      undefined,
      { enabled: false },
    );
    expect(state.servers).toHaveBeenCalledWith(
      { gramProject: "project-a" },
      undefined,
      {
        enabled: false,
      },
    );
    expect(state.endpoints).toHaveBeenCalledWith(
      { gramProject: "project-a" },
      undefined,
      {
        enabled: false,
      },
    );
  });
});
