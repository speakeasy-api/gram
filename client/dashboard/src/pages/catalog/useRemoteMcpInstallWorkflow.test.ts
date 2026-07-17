import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mockCreateServer = vi.fn();
const mockDeleteServer = vi.fn();
const mockCreateServerHeader = vi.fn();
const mockDiscoverProtectedResourceMetadata = vi.fn();
const mockMcpServersCreate = vi.fn();
const mockMcpEndpointsCreate = vi.fn();
const mockAuthedFetch = vi.fn();

// Return a stable client reference to avoid re-render loops from useCallback deps
const mockClient = {
  remoteMcp: {
    createServer: mockCreateServer,
    deleteServer: mockDeleteServer,
    createServerHeader: mockCreateServerHeader,
    discoverProtectedResourceMetadata: mockDiscoverProtectedResourceMetadata,
  },
  mcpServers: {
    create: mockMcpServersCreate,
  },
  mcpEndpoints: {
    create: mockMcpEndpointsCreate,
  },
};

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => mockClient,
  useSlugs: () => ({ orgSlug: "test-org", projectSlug: "test-project" }),
}));

vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({ fetch: mockAuthedFetch }),
}));

vi.mock("sonner", () => ({
  toast: { warning: vi.fn(), success: vi.fn(), error: vi.fn() },
}));

vi.mock("@gram/client/react-query/remoteMcpServers.js", () => ({
  useRemoteMcpServers: vi.fn(() => ({ data: undefined })),
  invalidateAllRemoteMcpServers: vi.fn(() => Promise.resolve()),
}));
vi.mock("@gram/client/react-query/remoteMcpServerHeaders.js", () => ({
  invalidateAllRemoteMcpServerHeaders: vi.fn(() => Promise.resolve()),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  invalidateAllMcpServers: vi.fn(() => Promise.resolve()),
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  invalidateAllMcpEndpoints: vi.fn(() => Promise.resolve()),
}));
vi.mock("@gram/client/react-query/userSessionIssuers.js", () => ({
  invalidateAllUserSessionIssuers: vi.fn(() => Promise.resolve()),
}));
vi.mock("@gram/client/react-query/remoteSessionIssuers.js", () => ({
  invalidateAllRemoteSessionIssuers: vi.fn(() => Promise.resolve()),
}));
vi.mock("@gram/client/react-query/remoteSessionClients.js", () => ({
  invalidateAllRemoteSessionClients: vi.fn(() => Promise.resolve()),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

import type { ExternalMCPRemote } from "@gram/client/models/components/externalmcpremote.js";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import { useRemoteMcpInstallWorkflow } from "./useRemoteMcpInstallWorkflow";

const mockUseRemoteMcpServers = vi.mocked(useRemoteMcpServers);

function remote(url: string, headerNames: string[] = []): ExternalMCPRemote {
  return {
    url,
    transportType: "streamable-http",
    headers: headerNames.length
      ? headerNames.map((name) => ({ name, isSecret: true, isRequired: true }))
      : undefined,
  };
}

function makeServer(overrides: Partial<PulseMCPServer> = {}): PulseMCPServer {
  return {
    description: "A test server",
    registryId: "reg-1",
    registrySpecifier: "test/server",
    version: "1.0.0",
    meta: {},
    toolCount: 0,
    isReadOnly: false,
    supportsDcr: false,
    remotes: [remote("https://mcp.example.com/mcp")],
    ...overrides,
  } as PulseMCPServer;
}

// IMPORTANT: The hook re-partitions on server-array identity changes, so the
// servers array must be a stable reference across renders. Inline `[]`
// literals in the renderHook callback create a new array each render →
// infinite loop → OOM.
const EMPTY_SERVERS: PulseMCPServer[] = [];

describe("useRemoteMcpInstallWorkflow", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseRemoteMcpServers.mockReturnValue({
      data: undefined,
    } as ReturnType<typeof useRemoteMcpServers>);
    mockCreateServer.mockImplementation((request) =>
      Promise.resolve({
        id: "rms-1",
        slug: "rms-slug",
        url: request.createServerForm.url,
        name: request.createServerForm.name,
        transportType: "streamable-http",
      }),
    );
    mockMcpServersCreate.mockResolvedValue({
      id: "mcp-server-1",
      slug: "mcp-server-slug",
      projectId: "proj-1",
      userSessionIssuerId: undefined,
    });
    mockCreateServerHeader.mockResolvedValue({ id: "header-1" });
    // No OAuth metadata → auto-config skips silently.
    mockDiscoverProtectedResourceMetadata.mockResolvedValue({
      available: false,
    });
    mockMcpEndpointsCreate.mockResolvedValue({
      id: "endpoint-1",
      slug: "test-org-abc123",
    });
    mockDeleteServer.mockResolvedValue(undefined);
  });

  // -------------------------------------------------------------------------
  // initial state
  // -------------------------------------------------------------------------

  it("starts in configure phase with no configs", () => {
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers: EMPTY_SERVERS }),
    );
    const state = result.current;
    expect(state.phase).toBe("configure");
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.serverConfigs).toEqual([]);
    expect(state.canInstall).toBe(false);
  });

  it("initializes configs from servers using title with specifier fallback", () => {
    const servers = [
      makeServer({ title: "My Server" }),
      makeServer({ title: undefined, registrySpecifier: "org/fallback" }),
    ];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers }),
    );
    const state = result.current;
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.serverConfigs.map((c) => c.name)).toEqual([
      "My Server",
      "org/fallback",
    ]);
    expect(state.canInstall).toBe(true);
  });

  it("blocks install when no server has a compatible remote", () => {
    const servers = [makeServer({ remotes: [] })];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers }),
    );
    const state = result.current;
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.canInstall).toBe(false);
  });

  it("reports endpoint-less servers as failed and installs the rest", async () => {
    const servers = [
      makeServer({ title: "No Endpoint", remotes: [] }),
      makeServer({
        title: "Good",
        remotes: [remote("https://good.example/mcp")],
      }),
    ];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers }),
    );
    const state = result.current;
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.canInstall).toBe(true);

    await startInstall(result);

    await waitFor(() => expect(result.current.phase).toBe("complete"));
    expect(mockCreateServer).toHaveBeenCalledTimes(1);
    const complete = result.current;
    if (complete.phase !== "complete") throw new Error("unexpected phase");
    const byName = new Map(complete.statuses.map((s) => [s.name, s]));
    expect(byName.get("Good")).toMatchObject({ status: "completed" });
    expect(byName.get("No Endpoint")).toMatchObject({ status: "failed" });
    expect(byName.get("No Endpoint")!.error).toContain(
      "compatible remote endpoint",
    );
  });

  // -------------------------------------------------------------------------
  // multi-remote partitioning
  // -------------------------------------------------------------------------

  it("routes multi-remote servers through the selectRemotes phase", () => {
    const servers = [
      makeServer({
        remotes: [
          remote("https://a.example/mcp"),
          remote("https://b.example/mcp"),
        ],
      }),
    ];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers }),
    );
    expect(result.current.phase).toBe("selectRemotes");
  });

  it("skips selectRemotes and installs every endpoint with autoSelectRemotes", () => {
    const servers = [
      makeServer({
        remotes: [
          remote("https://a.example/mcp"),
          remote("https://b.example/mcp"),
        ],
      }),
    ];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers, autoSelectRemotes: true }),
    );
    const state = result.current;
    expect(state.phase).toBe("configure");
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.serverConfigs[0]!.remotes).toHaveLength(2);
  });

  it("moves to configure once endpoints are selected", () => {
    const servers = [
      makeServer({
        title: "Multi",
        remotes: [
          remote("https://a.example/mcp"),
          remote("https://b.example/mcp"),
        ],
      }),
    ];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers }),
    );
    let state = result.current;
    if (state.phase !== "selectRemotes") throw new Error("unexpected phase");
    act(() => {
      if (result.current.phase !== "selectRemotes") return;
      result.current.updateCurrentConfig({
        selectedRemoteUrls: new Set(["https://a.example/mcp"]),
      });
    });
    act(() => {
      if (result.current.phase !== "selectRemotes") return;
      result.current.nextServer();
    });
    state = result.current;
    expect(state.phase).toBe("configure");
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.serverConfigs).toHaveLength(1);
    expect(state.serverConfigs[0]!.remotes.map((r) => r.url)).toEqual([
      "https://a.example/mcp",
    ]);
  });

  // -------------------------------------------------------------------------
  // install
  // -------------------------------------------------------------------------

  async function startInstall(result: {
    current: ReturnType<typeof useRemoteMcpInstallWorkflow>;
  }) {
    await act(async () => {
      if (result.current.phase !== "configure") {
        throw new Error("unexpected phase");
      }
      await result.current.startInstall();
    });
  }

  it("creates a remote MCP server, a private mcp server, and a default endpoint", async () => {
    const servers = [makeServer({ title: "My Server" })];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers }),
    );

    await startInstall(result);

    await waitFor(() => expect(result.current.phase).toBe("complete"));
    expect(mockCreateServer).toHaveBeenCalledWith(
      {
        createServerForm: {
          name: "My Server",
          url: "https://mcp.example.com/mcp",
          transportType: "streamable-http",
        },
      },
      undefined,
      undefined,
    );
    expect(mockMcpServersCreate).toHaveBeenCalledWith(
      {
        createMcpServerForm: expect.objectContaining({
          name: "My Server",
          remoteMcpServerId: "rms-1",
          visibility: "private",
        }),
      },
      undefined,
      undefined,
    );
    expect(mockMcpEndpointsCreate).toHaveBeenCalledTimes(1);

    const state = result.current;
    if (state.phase !== "complete") throw new Error("unexpected phase");
    expect(state.statuses).toHaveLength(1);
    expect(state.statuses[0]).toMatchObject({
      status: "completed",
      mcpServerId: "mcp-server-1",
      mcpServerParam: "mcp-server-slug",
    });
    expect(state.statuses[0]!.mcpEndpointUrl).toContain("/mcp/test-org-abc123");
  });

  it("sends gram-project request options for cross-project installs", async () => {
    const servers = [makeServer()];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers, projectSlug: "other-proj" }),
    );

    await startInstall(result);

    await waitFor(() => expect(result.current.phase).toBe("complete"));
    expect(mockCreateServer).toHaveBeenCalledWith(
      expect.anything(),
      undefined,
      { headers: { "gram-project": "other-proj" } },
    );
  });

  it("creates header records only for filled-in values", async () => {
    const url = "https://mcp.example.com/mcp";
    const servers = [
      makeServer({ remotes: [remote(url, ["X-API-Key", "X-Optional"])] }),
    ];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers }),
    );

    act(() => {
      if (result.current.phase !== "configure") return;
      result.current.setHeaderValue(0, url, "X-API-Key", "secret-value");
    });

    await startInstall(result);

    await waitFor(() => expect(result.current.phase).toBe("complete"));
    expect(mockCreateServerHeader).toHaveBeenCalledTimes(1);
    expect(mockCreateServerHeader).toHaveBeenCalledWith(
      {
        createServerHeaderForm: {
          remoteMcpServerId: "rms-1",
          name: "X-API-Key",
          description: undefined,
          isSecret: true,
          isRequired: true,
          value: "secret-value",
        },
      },
      undefined,
      undefined,
    );
  });

  it("never creates an Authorization header record, even without DCR", async () => {
    const url = "https://mcp.example.com/mcp";
    const servers = [
      makeServer({
        supportsDcr: false,
        remotes: [remote(url, ["Authorization"])],
      }),
    ];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers }),
    );

    // Even a (theoretically) entered value for a filtered header is ignored.
    act(() => {
      if (result.current.phase !== "configure") return;
      result.current.setHeaderValue(0, url, "Authorization", "Bearer x");
    });

    await startInstall(result);

    await waitFor(() => expect(result.current.phase).toBe("complete"));
    expect(mockCreateServerHeader).not.toHaveBeenCalled();
  });

  it("rolls back the remote MCP server when linking the mcp server fails", async () => {
    mockMcpServersCreate.mockRejectedValue(new Error("boom"));
    const servers = [makeServer()];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers }),
    );

    await startInstall(result);

    await waitFor(() => expect(result.current.phase).toBe("complete"));
    expect(mockDeleteServer).toHaveBeenCalledWith(
      { id: "rms-1" },
      undefined,
      undefined,
    );
    const state = result.current;
    if (state.phase !== "complete") throw new Error("unexpected phase");
    expect(state.statuses[0]).toMatchObject({ status: "failed" });
    expect(state.statuses[0]!.error).toContain("boom");
  });

  it("continues installing remaining servers after one fails", async () => {
    mockMcpServersCreate
      .mockRejectedValueOnce(new Error("first fails"))
      .mockResolvedValueOnce({ id: "mcp-server-2", slug: "second-slug" });
    const servers = [
      makeServer({
        title: "First",
        remotes: [remote("https://a.example/mcp")],
      }),
      makeServer({
        title: "Second",
        remotes: [remote("https://b.example/mcp")],
      }),
    ];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers }),
    );

    await startInstall(result);

    await waitFor(() => expect(result.current.phase).toBe("complete"));
    const state = result.current;
    if (state.phase !== "complete") throw new Error("unexpected phase");
    expect(state.statuses.map((s) => s.status)).toEqual([
      "failed",
      "completed",
    ]);
  });

  it("creates one server per selected endpoint with per-endpoint names", async () => {
    const servers = [
      makeServer({
        title: "Salesforce",
        remotes: [
          remote("https://sf.example/core"),
          remote("https://sf.example/health-cloud"),
        ],
      }),
    ];
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers, autoSelectRemotes: true }),
    );

    await startInstall(result);

    await waitFor(() => expect(result.current.phase).toBe("complete"));
    expect(mockCreateServer).toHaveBeenCalledTimes(2);
    const names = mockCreateServer.mock.calls.map(
      (call) => call[0].createServerForm.name,
    );
    expect(names).toEqual([
      "Salesforce Salesforce Core",
      "Salesforce Health Cloud",
    ]);
  });

  // -------------------------------------------------------------------------
  // installed indicator
  // -------------------------------------------------------------------------

  it("reports servers installed by URL match, ignoring trailing slashes", () => {
    mockUseRemoteMcpServers.mockReturnValue({
      data: {
        remoteMcpServers: [{ id: "x", url: "https://mcp.example.com/mcp/" }],
      },
    } as ReturnType<typeof useRemoteMcpServers>);

    const installed = makeServer();
    const notInstalled = makeServer({
      registrySpecifier: "test/other",
      remotes: [remote("https://other.example/mcp")],
    });
    const { result } = renderHook(() =>
      useRemoteMcpInstallWorkflow({ servers: EMPTY_SERVERS }),
    );

    expect(result.current.isServerAlreadyInstalled(installed)).toBe(true);
    expect(result.current.isServerAlreadyInstalled(notInstalled)).toBe(false);
  });
});
