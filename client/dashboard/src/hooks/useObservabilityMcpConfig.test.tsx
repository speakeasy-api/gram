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
beforeEach(() => vi.clearAllMocks());
describe("shared insights MCP requests", () => {
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
