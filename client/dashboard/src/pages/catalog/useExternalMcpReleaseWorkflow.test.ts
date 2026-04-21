import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mockEvolveDeployment = vi.fn();
const mockToolsetsCreate = vi.fn();
const mockToolsetsUpdateBySlug = vi.fn();
const mockToolsetsGetBySlug = vi.fn();

// Return a stable client reference to avoid re-render loops from useCallback deps
const mockClient = {
  deployments: { evolveDeployment: mockEvolveDeployment },
  toolsets: {
    create: mockToolsetsCreate,
    updateBySlug: mockToolsetsUpdateBySlug,
    getBySlug: mockToolsetsGetBySlug,
  },
};

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => mockClient,
}));

vi.mock("@gram/client/react-query/index.js", () => ({
  useDeployment: vi.fn(() => ({ data: undefined })),
  useDeploymentLogs: vi.fn(() => ({ data: undefined })),
  useLatestDeployment: vi.fn(() => ({ data: undefined })),
  useListToolsets: vi.fn(() => ({ data: undefined })),
}));

import {
  useDeployment,
  useDeploymentLogs,
  useLatestDeployment,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import type { Server } from "@/pages/catalog/hooks";
import {
  generateSlug,
  useExternalMcpReleaseWorkflow,
} from "./useExternalMcpReleaseWorkflow";

const mockLatest = vi.mocked(useLatestDeployment);
const mockDeployment = vi.mocked(useDeployment);
const mockLogs = vi.mocked(useDeploymentLogs);
const mockListToolsets = vi.mocked(useListToolsets);

function makeServer(overrides: Partial<Server> = {}): Server {
  return {
    description: "A test server",
    registryId: "reg-1",
    registrySpecifier: "test/server",
    version: "1.0.0",
    meta: {},
    ...overrides,
  } as Server;
}

// IMPORTANT: The hook has `useEffect([servers])` which means the servers array
// must be a stable reference across renders. Inline `[]` literals in the
// renderHook callback create a new array each render → infinite loop → OOM.
const EMPTY_SERVERS: Server[] = [];

// ---------------------------------------------------------------------------
// generateSlug
// ---------------------------------------------------------------------------

describe("generateSlug", () => {
  it("converts name to lowercase hyphenated slug", () => {
    expect(generateSlug("Pet Store")).toBe("pet-store");
  });

  it("uses last path segment", () => {
    expect(generateSlug("org/my-server")).toBe("my-server");
  });

  it("handles deep paths", () => {
    expect(generateSlug("a/b/c/my-tool")).toBe("my-tool");
  });

  it("strips leading and trailing hyphens", () => {
    expect(generateSlug("--hello--")).toBe("hello");
  });

  it("collapses non-alphanumeric runs into single hyphen", () => {
    expect(generateSlug("hello   world!!!foo")).toBe("hello-world-foo");
  });

  it("lowercases input", () => {
    expect(generateSlug("MyServer")).toBe("myserver");
  });

  it("handles trailing slash by falling back to full name", () => {
    expect(generateSlug("foo/")).toBe("foo");
  });

  it("handles empty string", () => {
    expect(generateSlug("")).toBe("");
  });
});

// ---------------------------------------------------------------------------
// useExternalMcpReleaseWorkflow
// ---------------------------------------------------------------------------

describe("useExternalMcpReleaseWorkflow", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockLatest.mockReturnValue({
      data: undefined,
      isLoading: false,
    } as ReturnType<typeof useLatestDeployment>);
    mockDeployment.mockReturnValue({
      data: undefined,
    } as ReturnType<typeof useDeployment>);
    mockLogs.mockReturnValue({
      data: undefined,
    } as ReturnType<typeof useDeploymentLogs>);
    mockListToolsets.mockReturnValue({
      data: undefined,
      isLoading: false,
    } as ReturnType<typeof useListToolsets>);
  });

  // -------------------------------------------------------------------------
  // initial state
  // -------------------------------------------------------------------------

  it("starts in configure phase with correct shape", () => {
    const { result } = renderHook(() =>
      useExternalMcpReleaseWorkflow({ servers: EMPTY_SERVERS }),
    );
    const state = result.current;
    expect(state.phase).toBe("configure");
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.serverConfigs).toEqual([]);
    expect(state.canDeploy).toBe(false);
  });

  it("passes projectSlug through", () => {
    const { result } = renderHook(() =>
      useExternalMcpReleaseWorkflow({
        servers: EMPTY_SERVERS,
        projectSlug: "my-proj",
      }),
    );
    expect(result.current.projectSlug).toBe("my-proj");
  });

  // -------------------------------------------------------------------------
  // serverConfigs initialization (via useEffect)
  // -------------------------------------------------------------------------

  it("initializes serverConfigs from servers using title", () => {
    const servers = [
      makeServer({ title: "My Server", registrySpecifier: "org/my-server" }),
    ];
    const { result } = renderHook(() =>
      useExternalMcpReleaseWorkflow({ servers }),
    );
    const state = result.current;
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.serverConfigs).toHaveLength(1);
    expect(state.serverConfigs[0].name).toBe("My Server");
    expect(state.serverConfigs[0].server).toBe(servers[0]);
  });

  it("falls back to registrySpecifier when title is missing", () => {
    const servers = [
      makeServer({ title: undefined, registrySpecifier: "org/fallback" }),
    ];
    const { result } = renderHook(() =>
      useExternalMcpReleaseWorkflow({ servers }),
    );
    const state = result.current;
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.serverConfigs[0].name).toBe("org/fallback");
  });

  it("re-initializes serverConfigs when servers prop changes", () => {
    const servers1 = [makeServer({ title: "First" })];
    const servers2 = [makeServer({ title: "Second" })];
    const { result, rerender } = renderHook(
      ({ servers }) => useExternalMcpReleaseWorkflow({ servers }),
      { initialProps: { servers: servers1 } },
    );
    let state = result.current;
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.serverConfigs[0].name).toBe("First");
    rerender({ servers: servers2 });
    state = result.current;
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.serverConfigs[0].name).toBe("Second");
  });

  // -------------------------------------------------------------------------
  // canDeploy
  // -------------------------------------------------------------------------

  it("canDeploy is true when all server names are non-empty", () => {
    const servers = [makeServer({ title: "Valid" })];
    const { result } = renderHook(() =>
      useExternalMcpReleaseWorkflow({ servers }),
    );
    const state = result.current;
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.canDeploy).toBe(true);
  });

  it("canDeploy is false when a server name is whitespace-only", () => {
    const servers = [
      makeServer({ title: "Valid" }),
      makeServer({ title: "  " }),
    ];
    const { result } = renderHook(() =>
      useExternalMcpReleaseWorkflow({ servers }),
    );
    const state = result.current;
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.canDeploy).toBe(false);
  });

  // -------------------------------------------------------------------------
  // updateServerConfig
  // -------------------------------------------------------------------------

  it("updates a server config name", () => {
    const servers = [makeServer({ title: "Original" })];
    const { result } = renderHook(() =>
      useExternalMcpReleaseWorkflow({ servers }),
    );
    act(() => {
      const state = result.current;
      if (state.phase !== "configure") throw new Error("unexpected phase");
      state.updateServerConfig(0, { name: "Renamed" });
    });
    const state = result.current;
    if (state.phase !== "configure") throw new Error("unexpected phase");
    expect(state.serverConfigs[0].name).toBe("Renamed");
  });

  // -------------------------------------------------------------------------
  // isServerAlreadyInstalled
  // -------------------------------------------------------------------------

  describe("isServerAlreadyInstalled", () => {
    it("is false when nothing is installed", () => {
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers: EMPTY_SERVERS }),
      );
      const server = makeServer({ registrySpecifier: "org/server" });
      expect(result.current.isServerAlreadyInstalled(server)).toBe(false);
    });

    it("matches specifiers attached to the latest deployment", () => {
      mockLatest.mockReturnValue({
        data: {
          deployment: {
            externalMcps: [
              { registryServerSpecifier: "org/server-a" },
              { registryServerSpecifier: "org/server-b" },
            ],
          },
        },
        isLoading: false,
      } as ReturnType<typeof useLatestDeployment>);

      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers: EMPTY_SERVERS }),
      );
      expect(
        result.current.isServerAlreadyInstalled(
          makeServer({ registrySpecifier: "org/server-a" }),
        ),
      ).toBe(true);
      expect(
        result.current.isServerAlreadyInstalled(
          makeServer({ registrySpecifier: "org/other" }),
        ),
      ).toBe(false);
    });

    it("matches specifiers persisted on existing toolset origins", () => {
      mockListToolsets.mockReturnValue({
        data: {
          toolsets: [
            {
              id: "ts-1",
              origin: { registrySpecifier: "collection/server-a" },
            },
            { id: "ts-2" },
          ],
        },
        isLoading: false,
      } as ReturnType<typeof useListToolsets>);

      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers: EMPTY_SERVERS }),
      );
      expect(
        result.current.isServerAlreadyInstalled(
          makeServer({ registrySpecifier: "collection/server-a" }),
        ),
      ).toBe(true);
    });

    it("matches collection-backed servers by toolsetId", () => {
      mockListToolsets.mockReturnValue({
        data: { toolsets: [{ id: "ts-1" }] },
        isLoading: false,
      } as ReturnType<typeof useListToolsets>);

      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers: EMPTY_SERVERS }),
      );
      expect(
        result.current.isServerAlreadyInstalled(
          makeServer({ registrySpecifier: "org/unused", toolsetId: "ts-1" }),
        ),
      ).toBe(true);
    });

    it("is false when the deployment has no externalMcps field", () => {
      mockLatest.mockReturnValue({
        data: { deployment: {} },
        isLoading: false,
      } as ReturnType<typeof useLatestDeployment>);

      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers: EMPTY_SERVERS }),
      );
      expect(
        result.current.isServerAlreadyInstalled(
          makeServer({ registrySpecifier: "org/server" }),
        ),
      ).toBe(false);
    });

    it("keeps already-installed single-remote servers in configure phase with fork-prefill name", () => {
      mockLatest.mockReturnValue({
        data: {
          deployment: {
            externalMcps: [{ registryServerSpecifier: "org/server" }],
          },
        },
        isLoading: false,
      } as ReturnType<typeof useLatestDeployment>);

      const servers = [
        makeServer({ title: "Existing", registrySpecifier: "org/server" }),
      ];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );

      const state = result.current;
      if (state.phase !== "configure") throw new Error("unexpected phase");
      expect(state.serverConfigs).toHaveLength(1);
      expect(state.serverConfigs[0].name).toBe("Existing Copy");
    });
  });

  // -------------------------------------------------------------------------
  // dependency hook arguments
  // -------------------------------------------------------------------------

  describe("dependency hook arguments", () => {
    it("passes gramProject to useLatestDeployment when projectSlug provided", () => {
      renderHook(() =>
        useExternalMcpReleaseWorkflow({
          servers: EMPTY_SERVERS,
          projectSlug: "my-proj",
        }),
      );
      expect(useLatestDeployment).toHaveBeenCalledWith({
        gramProject: "my-proj",
      });
    });

    it("passes undefined to useLatestDeployment without projectSlug", () => {
      renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers: EMPTY_SERVERS }),
      );
      expect(useLatestDeployment).toHaveBeenCalledWith(undefined);
    });

    it("disables deployment polling when no deploymentId", () => {
      renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers: EMPTY_SERVERS }),
      );
      expect(useDeployment).toHaveBeenCalledWith(
        expect.anything(),
        undefined,
        expect.objectContaining({ enabled: false }),
      );
    });

    it("disables log polling when no deploymentId", () => {
      renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers: EMPTY_SERVERS }),
      );
      expect(useDeploymentLogs).toHaveBeenCalledWith(
        expect.anything(),
        undefined,
        expect.objectContaining({ enabled: false }),
      );
    });
  });

  // -------------------------------------------------------------------------
  // startDeployment
  // -------------------------------------------------------------------------

  describe("startDeployment", () => {
    it("does nothing when canDeploy is false", async () => {
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers: EMPTY_SERVERS }),
      );
      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });
      expect(mockEvolveDeployment).not.toHaveBeenCalled();
      expect(result.current.phase).toBe("configure");
    });

    it("calls evolveDeployment and transitions to deploying", async () => {
      mockEvolveDeployment.mockResolvedValue({
        deployment: { id: "dep-123" },
      });
      mockLatest.mockReturnValue({
        data: { deployment: { id: "latest-dep" } },
        isLoading: false,
      } as ReturnType<typeof useLatestDeployment>);

      const server = makeServer({
        title: "Pet Store",
        registryId: "reg-pet",
        registrySpecifier: "org/pet-store",
      });
      const servers = [server];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );

      let state = result.current;
      if (state.phase !== "configure") throw new Error("unexpected phase");
      expect(state.serverConfigs[0].name).toBe("Pet Store");

      await act(async () => {
        state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      expect(result.current.phase).toBe("deploying");
      const deployingState = result.current;
      if (deployingState.phase !== "deploying")
        throw new Error("unexpected phase");
      expect(deployingState.deploymentId).toBe("dep-123");
      expect(mockEvolveDeployment).toHaveBeenCalledWith(
        {
          evolveForm: {
            deploymentId: "latest-dep",
            nonBlocking: true,
            upsertExternalMcps: [
              {
                registryId: "reg-pet",
                name: "Pet Store",
                slug: "pet-store",
                registryServerSpecifier: "org/pet-store",
              },
            ],
          },
        },
        undefined,
        undefined,
      );
    });

    it("passes gram-project header when projectSlug is set", async () => {
      mockEvolveDeployment.mockResolvedValue({
        deployment: { id: "dep-1" },
      });

      const servers = [makeServer({ title: "S" })];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers, projectSlug: "my-proj" }),
      );
      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      expect(mockEvolveDeployment).toHaveBeenCalledWith(
        expect.anything(),
        undefined,
        { headers: { "gram-project": "my-proj" } },
      );
    });

    it("deploys all servers, including already-installed ones, using name-derived slugs", async () => {
      mockLatest.mockReturnValue({
        data: {
          deployment: {
            id: "latest-dep",
            externalMcps: [
              {
                registryServerSpecifier: "org/existing",
                slug: "existing",
              },
            ],
          },
        },
        isLoading: false,
      } as ReturnType<typeof useLatestDeployment>);
      mockEvolveDeployment.mockResolvedValue({
        deployment: { id: "dep-123" },
      });

      const servers = [
        makeServer({ title: "Existing", registrySpecifier: "org/existing" }),
        makeServer({ title: "New", registrySpecifier: "org/new" }),
      ];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );

      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      expect(mockEvolveDeployment).toHaveBeenCalledWith(
        {
          evolveForm: {
            deploymentId: "latest-dep",
            nonBlocking: true,
            upsertExternalMcps: [
              expect.objectContaining({
                name: "Existing Copy",
                slug: "existing-copy",
                registryServerSpecifier: "org/existing",
              }),
              expect.objectContaining({
                name: "New",
                slug: "new",
                registryServerSpecifier: "org/new",
              }),
            ],
          },
        },
        undefined,
        undefined,
      );
    });

    it("creates a fork deployment for a collection-backed server that already has a toolset in the project", async () => {
      mockListToolsets.mockReturnValue({
        data: {
          toolsets: [{ id: "toolset-123" }],
        },
        isLoading: false,
      } as ReturnType<typeof useListToolsets>);
      mockEvolveDeployment.mockResolvedValue({
        deployment: { id: "dep-new" },
      });

      const servers = [
        makeServer({
          title: "Pulumi MCP Server",
          registrySpecifier: "local-dev-org.registry/pulumi-mcp-server",
          toolsetId: "toolset-123",
        }),
      ];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );

      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      expect(mockEvolveDeployment).toHaveBeenCalledWith(
        expect.objectContaining({
          evolveForm: expect.objectContaining({
            upsertExternalMcps: [
              expect.objectContaining({
                name: "Pulumi MCP Server Copy",
                slug: "pulumi-mcp-server-copy",
                registryServerSpecifier:
                  "local-dev-org.registry/pulumi-mcp-server",
              }),
            ],
          }),
        }),
        undefined,
        undefined,
      );
      expect(result.current.phase).toBe("deploying");
    });

    it("errors when two configs in the same batch derive to the same slug", async () => {
      const servers = [
        makeServer({ title: "My Server!", registrySpecifier: "org/a" }),
        makeServer({ title: "My Server?", registrySpecifier: "org/b" }),
      ];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );

      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      expect(mockEvolveDeployment).not.toHaveBeenCalled();
      expect(result.current.phase).toBe("error");
      const state = result.current;
      if (state.phase !== "error") throw new Error("unexpected phase");
      expect(state.error).toMatch(
        /both resolve to slug "my-server". Rename one to disambiguate\./,
      );
    });

    it("errors when a name's slug collides with an existing attachment", async () => {
      mockLatest.mockReturnValue({
        data: {
          deployment: {
            id: "latest-dep",
            externalMcps: [
              {
                registryServerSpecifier: "org/unrelated",
                slug: "my-server",
              },
            ],
          },
        },
        isLoading: false,
      } as ReturnType<typeof useLatestDeployment>);

      const servers = [
        makeServer({ title: "My Server", registrySpecifier: "org/my-server" }),
      ];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );

      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      expect(mockEvolveDeployment).not.toHaveBeenCalled();
      expect(result.current.phase).toBe("error");
      const state = result.current;
      if (state.phase !== "error") throw new Error("unexpected phase");
      expect(state.error).toMatch(/slug "my-server" already exists/);
    });

    it("transitions to complete when no deployment ID returned", async () => {
      mockEvolveDeployment.mockResolvedValue({});
      // Prevent toolset creation from progressing so we can inspect state
      mockToolsetsCreate.mockReturnValue(new Promise(() => {}));

      const servers = [makeServer({ title: "S" })];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );
      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      expect(result.current.phase).toBe("complete");
      const state = result.current;
      if (state.phase !== "complete") throw new Error("unexpected phase");
      expect(state.toolsetStatuses).toHaveLength(1);
    });

    it("transitions to error when evolveDeployment throws", async () => {
      mockEvolveDeployment.mockRejectedValue(new Error("Network error"));

      const servers = [makeServer({ title: "S" })];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );
      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      expect(result.current.phase).toBe("error");
      const state = result.current;
      if (state.phase !== "error") throw new Error("unexpected phase");
      expect(state.error).toBe("Network error");
    });
  });

  // -------------------------------------------------------------------------
  // deployment status transitions (deploying → complete / error)
  // -------------------------------------------------------------------------

  describe("deployment status transitions", () => {
    it("transitions to complete when deployment status becomes completed", async () => {
      mockEvolveDeployment.mockResolvedValue({
        deployment: { id: "dep-1" },
      });
      // Prevent toolset creation from progressing so we can inspect transition state
      mockToolsetsCreate.mockReturnValue(new Promise(() => {}));

      const server = makeServer({ title: "My Server" });
      const servers = [server];
      const { result, rerender } = renderHook(
        ({ servers }) => useExternalMcpReleaseWorkflow({ servers }),
        { initialProps: { servers } },
      );

      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });
      expect(result.current.phase).toBe("deploying");

      // Simulate deployment completing via the useDeployment mock
      mockDeployment.mockReturnValue({
        data: { status: "completed" },
      } as ReturnType<typeof useDeployment>);
      rerender({ servers });

      expect(result.current.phase).toBe("complete");
      const state = result.current;
      if (state.phase !== "complete") throw new Error("unexpected phase");
      expect(state.toolsetStatuses).toHaveLength(1);
      expect(state.toolsetStatuses[0].name).toBe("My Server");
      expect(state.toolsetStatuses[0].slug).toBe("my-server");
    });

    it("transitions to error when deployment status becomes failed", async () => {
      mockEvolveDeployment.mockResolvedValue({
        deployment: { id: "dep-1" },
      });

      const servers = [makeServer({ title: "S" })];
      const { result, rerender } = renderHook(
        ({ servers }) => useExternalMcpReleaseWorkflow({ servers }),
        { initialProps: { servers } },
      );

      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      mockDeployment.mockReturnValue({
        data: { status: "failed" },
      } as ReturnType<typeof useDeployment>);
      rerender({ servers });

      expect(result.current.phase).toBe("error");
      const state = result.current;
      if (state.phase !== "error") throw new Error("unexpected phase");
      expect(state.error).toBe(
        "Deployment failed. Check the logs for details.",
      );
    });
  });

  // -------------------------------------------------------------------------
  // toolset creation (on complete phase)
  // -------------------------------------------------------------------------

  describe("toolset creation", () => {
    it("creates toolsets with proxy URN when server has no tools", async () => {
      mockEvolveDeployment.mockResolvedValue({});
      mockToolsetsCreate.mockResolvedValue({ slug: "my-server" });
      mockToolsetsUpdateBySlug.mockResolvedValue({});
      mockToolsetsGetBySlug.mockResolvedValue({ mcpSlug: "mcp-my-server" });

      const servers = [
        makeServer({
          title: "My Server",
          registrySpecifier: "org/my-server",
        }),
      ];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );

      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      await vi.waitFor(() => {
        const state = result.current;
        if (state.phase !== "complete") throw new Error("unexpected phase");
        expect(state.toolsetStatuses[0].status).toBe("completed");
      });

      expect(mockToolsetsCreate).toHaveBeenCalledWith(
        {
          createToolsetRequestBody: {
            name: "My Server",
            description: "A test server",
            origin: {
              registrySpecifier: "org/my-server",
            },
            toolUrns: ["tools:externalmcp:my-server:proxy"],
          },
        },
        undefined,
        undefined,
      );
      expect(mockToolsetsUpdateBySlug).toHaveBeenCalledWith(
        {
          slug: "my-server",
          updateToolsetRequestBody: { mcpEnabled: true, mcpIsPublic: true },
        },
        undefined,
        undefined,
      );
      const state = result.current;
      if (state.phase !== "complete") throw new Error("unexpected phase");
      expect(state.toolsetStatuses[0].toolsetSlug).toBe("my-server");
      expect(state.toolsetStatuses[0].mcpSlug).toBe("mcp-my-server");
    });

    it("creates a fork by deploying a new attachment under a distinct slug", async () => {
      mockLatest.mockReturnValue({
        data: {
          deployment: {
            id: "latest-dep",
            externalMcps: [
              {
                registryServerSpecifier: "org/my-server",
                slug: "my-server",
              },
            ],
          },
        },
        isLoading: false,
      } as ReturnType<typeof useLatestDeployment>);
      mockEvolveDeployment.mockResolvedValue({
        deployment: { id: "dep-fork" },
      });
      mockToolsetsCreate.mockResolvedValue({ slug: "my-server-fork" });
      mockToolsetsUpdateBySlug.mockResolvedValue({});
      mockToolsetsGetBySlug.mockResolvedValue({
        mcpSlug: "mcp-my-server-fork",
      });

      const servers = [
        makeServer({
          title: "My Server",
          registrySpecifier: "org/my-server",
        }),
      ];
      const { result, rerender } = renderHook(
        ({ servers }) => useExternalMcpReleaseWorkflow({ servers }),
        { initialProps: { servers } },
      );

      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      expect(mockEvolveDeployment).toHaveBeenCalledWith(
        expect.objectContaining({
          evolveForm: expect.objectContaining({
            upsertExternalMcps: [
              expect.objectContaining({
                name: "My Server Copy",
                slug: "my-server-copy",
                registryServerSpecifier: "org/my-server",
              }),
            ],
          }),
        }),
        undefined,
        undefined,
      );

      // Simulate deployment completion so toolset creation runs
      mockDeployment.mockReturnValue({
        data: { status: "completed" },
      } as ReturnType<typeof useDeployment>);
      rerender({ servers });

      await vi.waitFor(() => {
        const state = result.current;
        if (state.phase !== "complete") throw new Error("unexpected phase");
        expect(state.toolsetStatuses[0].status).toBe("completed");
      });

      expect(mockToolsetsCreate).toHaveBeenCalledWith(
        {
          createToolsetRequestBody: {
            name: "My Server Copy",
            description: "A test server",
            origin: {
              registrySpecifier: "org/my-server",
            },
            toolUrns: ["tools:externalmcp:my-server-copy:proxy"],
          },
        },
        undefined,
        undefined,
      );
    });

    it("creates toolsets with per-tool URNs when server has tools", async () => {
      mockEvolveDeployment.mockResolvedValue({});
      mockToolsetsCreate.mockResolvedValue({ slug: "my-server" });
      mockToolsetsUpdateBySlug.mockResolvedValue({});
      mockToolsetsGetBySlug.mockResolvedValue({ mcpSlug: "mcp-slug" });

      const servers = [
        makeServer({
          title: "My Server",
          registrySpecifier: "org/my-server",
          tools: [{ name: "listPets" }, { name: "getPet" }],
        } as Partial<Server>),
      ];

      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );
      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      await vi.waitFor(() => {
        const state = result.current;
        if (state.phase !== "complete") throw new Error("unexpected phase");
        expect(state.toolsetStatuses[0].status).toBe("completed");
      });

      expect(mockToolsetsCreate).toHaveBeenCalledWith(
        expect.objectContaining({
          createToolsetRequestBody: expect.objectContaining({
            origin: {
              registrySpecifier: "org/my-server",
            },
            toolUrns: [
              "tools:externalmcp:my-server:listPets",
              "tools:externalmcp:my-server:getPet",
            ],
          }),
        }),
        undefined,
        undefined,
      );
    });

    it("marks toolset as failed when creation throws", async () => {
      mockEvolveDeployment.mockResolvedValue({});
      mockToolsetsCreate.mockRejectedValue(new Error("quota exceeded"));

      const servers = [makeServer({ title: "S" })];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );
      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      await vi.waitFor(() => {
        const state = result.current;
        if (state.phase !== "complete") throw new Error("unexpected phase");
        expect(state.toolsetStatuses[0].status).toBe("failed");
      });

      const state = result.current;
      if (state.phase !== "complete") throw new Error("unexpected phase");
      expect(state.toolsetStatuses[0].error).toBe("quota exceeded");
    });

    it("passes gram-project header during toolset creation", async () => {
      mockEvolveDeployment.mockResolvedValue({});
      mockToolsetsCreate.mockResolvedValue({ slug: "s" });
      mockToolsetsUpdateBySlug.mockResolvedValue({});
      mockToolsetsGetBySlug.mockResolvedValue({ mcpSlug: "m" });

      const servers = [makeServer({ title: "S" })];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers, projectSlug: "proj-1" }),
      );
      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });

      await vi.waitFor(() => {
        const state = result.current;
        if (state.phase !== "complete") throw new Error("unexpected phase");
        expect(state.toolsetStatuses[0].status).toBe("completed");
      });

      const reqOpts = { headers: { "gram-project": "proj-1" } };
      expect(mockToolsetsCreate).toHaveBeenCalledWith(
        expect.anything(),
        undefined,
        reqOpts,
      );
      expect(mockToolsetsUpdateBySlug).toHaveBeenCalledWith(
        expect.anything(),
        undefined,
        reqOpts,
      );
      expect(mockToolsetsGetBySlug).toHaveBeenCalledWith(
        expect.anything(),
        undefined,
        reqOpts,
      );
    });
  });

  // -------------------------------------------------------------------------
  // reset
  // -------------------------------------------------------------------------

  describe("reset", () => {
    it("returns to configure phase and clears state", async () => {
      mockEvolveDeployment.mockRejectedValue(new Error("fail"));

      const servers = [makeServer({ title: "S" })];
      const { result } = renderHook(() =>
        useExternalMcpReleaseWorkflow({ servers }),
      );

      // Get into error state
      await act(async () => {
        const state = result.current;
        if (state.phase !== "configure") throw new Error("unexpected phase");
        await state.startDeployment();
      });
      expect(result.current.phase).toBe("error");

      act(() => result.current.reset());

      expect(result.current.phase).toBe("configure");
      const state = result.current;
      if (state.phase !== "configure") throw new Error("unexpected phase");
      expect(state.serverConfigs[0].name).toBe("S");
    });
  });
});
