import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import type { ProjectGuideOperationReport } from "./projectGuideMachine";

const queryHooks = vi.hoisted(() => ({
  activity: vi.fn(),
  catalog: vi.fn(),
  endpoints: vi.fn(),
  plugins: vi.fn(),
  remoteServers: vi.fn(),
  servers: vi.fn(),
}));
const workflowHook = vi.hoisted(() => vi.fn());
const requestProject = vi.hoisted(() => ({ slug: "request-project" }));
const refetchActivity = vi.hoisted(() => vi.fn(() => Promise.resolve()));
const startInstall = vi.hoisted(() => vi.fn(() => Promise.resolve()));
const resetInstall = vi.hoisted(() => vi.fn());

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => requestProject.slug,
}));
vi.mock("@/pages/catalog/hooks", () => ({
  useListMCPCatalog: queryHooks.catalog,
}));
vi.mock("@/pages/catalog/useRemoteMcpInstallWorkflow", () => ({
  useRemoteMcpInstallWorkflow: workflowHook,
}));
vi.mock("@gram/client/react-query/getMcpServerActivity.js", () => ({
  useGetMcpServerActivity: queryHooks.activity,
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: queryHooks.endpoints,
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: queryHooks.servers,
}));
vi.mock("@gram/client/react-query/plugins.js", () => ({
  usePlugins: queryHooks.plugins,
}));
vi.mock("@gram/client/react-query/remoteMcpServers.js", () => ({
  useRemoteMcpServers: queryHooks.remoteServers,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    logs: { href: () => "/projects/request-project/logs" },
    mcp: {
      x: {
        overview: {
          href: (slug: string) => `/projects/request-project/mcp/${slug}`,
        },
      },
    },
  }),
}));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://api.example",
}));

import { useMcpGuideOperations } from "./useMcpGuideOperations";

function catalogServer(
  overrides: Partial<PulseMCPServer> = {},
): PulseMCPServer {
  return {
    description: "Read-only test server",
    registryId: "registry",
    registrySpecifier: "example/read-only",
    version: "1.0.0",
    title: "Linear",
    meta: {},
    toolCount: 2,
    isReadOnly: true,
    supportsDcr: true,
    remotes: [
      {
        transportType: "streamable-http",
        url: "https://upstream.example/mcp",
      },
    ],
    ...overrides,
  } as PulseMCPServer;
}

const SERVER = catalogServer();
const SERVER_SCOPE = {
  path: "third-party-mcp" as const,
  step: 0,
  attempt: 0,
  runId: 1,
};

function queryResult<T>(data: T) {
  return {
    data,
    isError: false,
    isFetching: false,
    isPending: false,
    refetch: vi.fn(() => Promise.resolve({ data, isError: false })),
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  requestProject.slug = "request-project";
  queryHooks.catalog.mockReturnValue(
    queryResult({
      servers: [
        catalogServer({ title: "Other", registrySpecifier: "other/server" }),
        catalogServer({ title: "Linear" }),
        catalogServer({ title: "Manual", supportsDcr: false }),
        catalogServer({ title: "Writable", isReadOnly: false }),
        catalogServer({
          title: "Figma",
          registrySpecifier: "com.figma.mcp/mcp",
        }),
        catalogServer({
          title: "stdio",
          remotes: [
            { transportType: "sse", url: "https://not-supported.example/sse" },
          ],
        }),
      ],
    }),
  );
  queryHooks.servers.mockReturnValue(queryResult({ mcpServers: [] }));
  queryHooks.remoteServers.mockReturnValue(
    queryResult({ remoteMcpServers: [] }),
  );
  queryHooks.endpoints.mockReturnValue(queryResult({ mcpEndpoints: [] }));
  queryHooks.plugins.mockReturnValue(queryResult({ plugins: [] }));
  queryHooks.activity.mockReturnValue({
    ...queryResult({ activity: [] }),
    refetch: refetchActivity,
  });
  workflowHook.mockReturnValue({
    phase: "configure",
    canInstall: true,
    serverConfigs: [],
    startInstall,
    reset: resetInstall,
    isServerAlreadyInstalled: () => false,
  });
});

function setExistingServer({ calls = 0 }: { calls?: number } = {}): void {
  queryHooks.servers.mockReturnValue(
    queryResult({
      mcpServers: [
        {
          id: "mcp-server-id",
          slug: "linear-governed",
          name: "Linear",
          remoteMcpServerId: "remote-id",
        },
      ],
    }),
  );
  queryHooks.remoteServers.mockReturnValue(
    queryResult({
      remoteMcpServers: [
        {
          id: "remote-id",
          url: "https://upstream.example/mcp",
        },
      ],
    }),
  );
  queryHooks.endpoints.mockReturnValue(
    queryResult({
      mcpEndpoints: [
        {
          id: "endpoint-id",
          slug: "linear-endpoint",
          mcpServerId: "mcp-server-id",
        },
      ],
    }),
  );
  queryHooks.plugins.mockReturnValue(
    queryResult({
      plugins: [
        {
          id: "default-plugin",
          isDefault: true,
          servers: [{ mcpServerId: "mcp-server-id" }],
        },
      ],
    }),
  );
  queryHooks.activity.mockReturnValue({
    ...queryResult({
      activity:
        calls === 0
          ? []
          : [
              {
                lastToolCallAt: new Date("2026-08-19T12:00:00Z"),
                recentToolCalls: calls,
                targetId: "linear-governed",
                targetLabel: "Linear",
                targetType: "hosted_mcp_server",
                totalToolCalls: calls,
              },
            ],
    }),
    refetch: refetchActivity,
  });
}

describe("useMcpGuideOperations", () => {
  it("curates automatic read-only HTTP servers without installing a selection", () => {
    const { result } = renderHook(() => useMcpGuideOperations());

    expect(
      result.current.catalogServers?.map((server) => server.title),
    ).toEqual(["Linear", "Other"]);

    act(() => result.current.selectServer(SERVER));

    expect(startInstall).not.toHaveBeenCalled();
    expect(workflowHook).toHaveBeenLastCalledWith({
      servers: [SERVER],
      projectSlug: "request-project",
      autoSelectRemotes: true,
    });
  });

  it("starts the existing project-scoped install workflow only after start", async () => {
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result } = renderHook(() => useMcpGuideOperations());

    act(() => result.current.selectServer(SERVER));
    expect(startInstall).not.toHaveBeenCalled();

    act(() =>
      result.current.handleSignal(
        { type: "start", scope: SERVER_SCOPE },
        report,
      ),
    );

    await waitFor(() => expect(startInstall).toHaveBeenCalledOnce());
    expect(report).toHaveBeenCalledWith({
      type: "progress",
      scope: SERVER_SCOPE,
      message: "Installing Linear into this project",
      progress: 0.2,
    });
    expect(queryHooks.servers).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { throwOnError: false },
    );
    expect(queryHooks.endpoints).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { throwOnError: false },
    );
    expect(queryHooks.plugins).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { throwOnError: false },
    );
    expect(queryHooks.remoteServers).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { throwOnError: false },
    );
  });

  it("resumes an existing catalog server and requires Default plugin plus endpoint readiness", async () => {
    setExistingServer();
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result } = renderHook(() => useMcpGuideOperations());

    expect(result.current.mcpServer?.slug).toBe("linear-governed");
    expect(result.current.endpointUrl).toBe(
      "https://api.example/mcp/linear-endpoint",
    );
    expect(result.current.deploymentReady).toBe(true);
    expect(workflowHook).toHaveBeenLastCalledWith({
      servers: [],
      projectSlug: "request-project",
      autoSelectRemotes: true,
    });

    const scope = { ...SERVER_SCOPE, step: 1, runId: 2 };
    act(() => result.current.handleSignal({ type: "start", scope }, report));

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "success",
        scope,
        result: "Linear is ready on its governed endpoint",
      }),
    );

    queryHooks.plugins.mockReturnValue(queryResult({ plugins: [] }));
    const incomplete = renderHook(() => useMcpGuideOperations());
    expect(incomplete.result.current.deploymentReady).toBe(false);
  });

  it("returns client snippets, links, and a list-plus-read-only-call prompt", () => {
    setExistingServer();
    const { result } = renderHook(() => useMcpGuideOperations());

    expect(result.current.snippets?.claude.code).toContain(
      '"url": "https://api.example/mcp/linear-endpoint"',
    );
    expect(result.current.snippets?.cursor.language).toBe("json");
    expect(result.current.snippets?.codex).toEqual({
      code: '[mcp_servers.linear-governed]\nurl = "https://api.example/mcp/linear-endpoint"',
      language: "toml",
    });
    expect(result.current.prompt).toMatch(/first list the available tools/i);
    expect(result.current.prompt).toMatch(/marked read-only/i);
    expect(result.current.prompt).toMatch(/do not create, update, or delete/i);
    expect(result.current.mcpServerHref).toBe(
      "/projects/request-project/mcp/linear-governed",
    );
    expect(result.current.toolLogsHref).toBe("/projects/request-project/logs");

    expect(result.current.client).toBe("claude");
    expect(result.current.configCopied).toBe(false);
    expect(result.current.promptCopied).toBe(false);
    act(() => {
      result.current.setClient("codex");
      result.current.markConfigCopied();
      result.current.markPromptCopied();
    });
    expect(result.current.client).toBe("codex");
    expect(result.current.configCopied).toBe(true);
    expect(result.current.promptCopied).toBe(true);
  });

  it("captures the selected hosted server baseline before the prompt and ignores historical activity", async () => {
    setExistingServer({ calls: 4 });
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result, rerender } = renderHook(() => useMcpGuideOperations());
    const promptScope = { ...SERVER_SCOPE, step: 2, runId: 2 };
    const listenScope = { ...SERVER_SCOPE, step: 4, runId: 3 };

    act(() => {
      result.current.handleSignal(
        { type: "checkpoint", scope: promptScope },
        report,
      );
      result.current.handleSignal(
        { type: "start", scope: listenScope },
        report,
      );
    });
    rerender();

    expect(result.current.activityBaselineReady).toBe(true);
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "event" }),
    );

    setExistingServer({ calls: 5 });
    rerender();

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "event",
        scope: listenScope,
        event: {
          kind: "Governed call",
          tone: "allow",
          title: "Linear",
          rows: [
            { key: "server", value: "Linear" },
            { key: "calls", value: "5 recorded" },
          ],
          note: "The new call is recorded in Tool Logs.",
        },
      }),
    );
  });

  it("reports unreadable activity as an error and retries without resetting the prompt baseline", async () => {
    setExistingServer({ calls: 2 });
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result, rerender } = renderHook(() => useMcpGuideOperations());
    const promptScope = { ...SERVER_SCOPE, step: 2, runId: 2 };
    const listenScope = { ...SERVER_SCOPE, step: 4, runId: 3 };

    act(() => {
      result.current.handleSignal(
        { type: "checkpoint", scope: promptScope },
        report,
      );
      result.current.handleSignal(
        { type: "start", scope: listenScope },
        report,
      );
    });

    queryHooks.activity.mockReturnValue({
      data: undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: refetchActivity,
    });
    rerender();

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "error",
        scope: listenScope,
        message:
          "Could not check for the new governed call. Retry after checking the client connection.",
      }),
    );

    const retryScope = { ...listenScope, attempt: 1 };
    act(() =>
      result.current.handleSignal({ type: "retry", scope: retryScope }, report),
    );

    expect(refetchActivity).toHaveBeenCalled();
    expect(result.current.activityBaselineReady).toBe(true);
  });

  it("does not treat undefined catalog or project data as empty or ready", () => {
    queryHooks.catalog.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queryHooks.servers.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    const { result } = renderHook(() => useMcpGuideOperations());

    expect(result.current.catalogServers).toBeUndefined();
    expect(result.current.catalogError).toBe(true);
    expect(result.current.projectStateError).toBe(true);
    expect(result.current.deploymentReady).toBe(false);
  });
});
