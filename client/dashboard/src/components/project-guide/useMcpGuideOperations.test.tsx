import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import type { ToolUsageTraceSummary } from "@gram/client/models/components/toolusagetracesummary.js";
import type { ProjectGuideOperationReport } from "./projectGuideMachine";

const queryHooks = vi.hoisted(() => ({
  catalog: vi.fn(),
  endpoints: vi.fn(),
  plugins: vi.fn(),
  remoteServers: vi.fn(),
  servers: vi.fn(),
  toolTraces: vi.fn(),
}));
const workflowHook = vi.hoisted(() => vi.fn());
const requestProject = vi.hoisted(() => ({ slug: "request-project" }));
const refetchTraces = vi.hoisted(() =>
  vi.fn<
    () => Promise<{
      data: { traces: ToolUsageTraceSummary[] } | undefined;
      isError: boolean;
    }>
  >(() => Promise.resolve({ data: { traces: [] }, isError: false })),
);
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
vi.mock("@gram/client/react-query/listToolUsageTraces.js", () => ({
  useListToolUsageTraces: queryHooks.toolTraces,
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
  refetchTraces.mockResolvedValue({
    data: { traces: [] },
    isError: false,
  });
  requestProject.slug = "request-project";
  queryHooks.catalog.mockReturnValue(
    queryResult({
      servers: [
        catalogServer({ title: "Other", registrySpecifier: "other/server" }),
        catalogServer({ title: "Linear" }),
        catalogServer({
          title: "Vercel",
          isReadOnly: false,
          supportsDcr: true,
        }),
        catalogServer({ title: "GitHub" }),
        catalogServer({ title: "Notion" }),
        catalogServer({ title: "Granola" }),
        catalogServer({ title: "Ramp" }),
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
  queryHooks.toolTraces.mockReturnValue({
    ...queryResult({ traces: [] }),
    refetch: refetchTraces,
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

function setExistingServer({
  endpoint = true,
  defaultPlugin = true,
}: {
  endpoint?: boolean;
  defaultPlugin?: boolean;
} = {}): void {
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
      mcpEndpoints: endpoint
        ? [
            {
              id: "endpoint-id",
              slug: "linear-endpoint",
              mcpServerId: "mcp-server-id",
            },
          ]
        : [],
    }),
  );
  queryHooks.plugins.mockReturnValue(
    queryResult({
      plugins: [
        {
          id: "default-plugin",
          isDefault: true,
          servers: defaultPlugin ? [{ mcpServerId: "mcp-server-id" }] : [],
        },
      ],
    }),
  );
}

function toolTrace(
  overrides: Partial<ToolUsageTraceSummary> = {},
): ToolUsageTraceSummary {
  return {
    eventSource: "mcp-proxy",
    gramUrn: "tools:linear-governed:list_issues",
    httpStatusCode: 200,
    id: "trace-row-id",
    logCount: 2,
    logGroup: { kind: "trace_id", value: "trace-id" },
    startTimeUnixNano: "1787140800000000000",
    targetId: "linear-governed",
    targetKind: "server",
    targetLabel: "Linear",
    targetType: "hosted_mcp_server",
    toolName: "linear.list_issues",
    traceId: "trace-id",
    userKey: "user-id",
    userKind: "user_id",
    userLabel: "Agent user",
    ...overrides,
  };
}

describe("useMcpGuideOperations", () => {
  it("exposes six curated catalog choices without installing a selection", () => {
    const { result } = renderHook(() => useMcpGuideOperations());

    expect(result.current.catalogServers).toHaveLength(6);

    act(() => result.current.selectServer(SERVER));

    expect(startInstall).not.toHaveBeenCalled();
    expect(workflowHook).toHaveBeenLastCalledWith({
      servers: [SERVER],
      projectSlug: "request-project",
      autoSelectRemotes: true,
      serverNameSuffix: "_Governed",
    });
  });

  it("excludes curated servers that require user-supplied credentials", () => {
    const apiKeyServer = catalogServer({
      title: "Notion",
      meta: {
        "com.pulsemcp/server-version": {
          "remotes[0]": { authOptions: [{ type: "api_key" }] },
        },
      },
    });
    queryHooks.catalog.mockReturnValue(
      queryResult({ servers: [catalogServer(), apiKeyServer] }),
    );

    const { result } = renderHook(() => useMcpGuideOperations());

    expect(
      result.current.catalogServers?.map((server) => server.title),
    ).toEqual(["Linear"]);
  });

  it("reports OAuth setup in the activity output", async () => {
    const oauthServer = catalogServer({
      title: "Notion",
      meta: {
        "com.pulsemcp/server-version": {
          "remotes[0]": { authOptions: [{ type: "oauth" }] },
        },
      },
    });
    queryHooks.catalog.mockReturnValue(queryResult({ servers: [oauthServer] }));

    const { result } = renderHook(() => useMcpGuideOperations());
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();

    act(() => result.current.selectServer(oauthServer));
    act(() =>
      result.current.handleSignal(
        { type: "start", scope: SERVER_SCOPE },
        report,
      ),
    );

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "progress",
        scope: SERVER_SCOPE,
        message: "Configuring OAuth for Notion",
        progress: 0.75,
      }),
    );
    expect(report).toHaveBeenNthCalledWith(1, {
      type: "progress",
      scope: SERVER_SCOPE,
      message: "Reading the server's tool list",
      progress: 0.2,
    });
    expect(report).toHaveBeenNthCalledWith(2, {
      type: "progress",
      scope: SERVER_SCOPE,
      message: "Installing Notion into this project",
      progress: 0.5,
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
      progress: 0.5,
    });
    expect(report).toHaveBeenCalledWith({
      type: "progress",
      scope: SERVER_SCOPE,
      message: "Reading the server's tool list",
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

  it("does not configure or start installation while project state is pending or unreadable", async () => {
    queryHooks.servers.mockReturnValue({
      data: undefined,
      isError: false,
      isFetching: true,
      isPending: true,
      refetch: vi.fn(),
    });
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result, rerender } = renderHook(() => useMcpGuideOperations());

    act(() => result.current.selectServer(SERVER));
    expect(workflowHook).toHaveBeenLastCalledWith({
      servers: [],
      projectSlug: "request-project",
      autoSelectRemotes: true,
      serverNameSuffix: "_Governed",
    });

    act(() =>
      result.current.handleSignal(
        { type: "start", scope: SERVER_SCOPE },
        report,
      ),
    );
    expect(startInstall).not.toHaveBeenCalled();

    queryHooks.servers.mockReturnValue({
      data: undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(),
    });
    rerender();

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "error",
        scope: SERVER_SCOPE,
        message:
          "Could not read this project's existing MCP servers. Retry the project check before installing.",
      }),
    );
    expect(startInstall).not.toHaveBeenCalled();
  });

  it("exposes the endpoint for client setup", () => {
    setExistingServer();
    const { result } = renderHook(() => useMcpGuideOperations());

    expect(result.current.mcpServer?.slug).toBe("linear-governed");
    expect(result.current.endpointUrl).toBe(
      "https://api.example/mcp/linear-endpoint",
    );
    expect(workflowHook).toHaveBeenLastCalledWith({
      servers: [],
      projectSlug: "request-project",
      autoSelectRemotes: true,
      serverNameSuffix: "_Governed",
    });
    expect(queryHooks.toolTraces).toHaveBeenCalledWith(
      {
        gramProject: "request-project",
        listToolUsageTracesPayload: expect.objectContaining({
          hostedToolsetSlugs: ["linear-governed"],
          targetTypes: ["hosted_mcp_server"],
        }),
      },
      undefined,
      expect.objectContaining({ enabled: true, throwOnError: false }),
    );
  });

  it.each([
    { endpoint: false, defaultPlugin: true },
    { endpoint: true, defaultPlugin: false },
  ])(
    "does not advance an existing server until endpoint and Default membership are readable: %o",
    async (readiness) => {
      setExistingServer(readiness);
      const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
      const { result } = renderHook(() => useMcpGuideOperations());

      act(() => result.current.selectServer(SERVER));
      act(() =>
        result.current.handleSignal(
          { type: "start", scope: SERVER_SCOPE },
          report,
        ),
      );

      await waitFor(() =>
        expect(report).toHaveBeenCalledWith({
          type: "error",
          scope: SERVER_SCOPE,
          message:
            "The governed endpoint or Default plugin is not ready yet. Retry the readiness check.",
        }),
      );
      expect(report).not.toHaveBeenCalledWith(
        expect.objectContaining({ type: "success" }),
      );
    },
  );

  it("refetches project readiness and does not advance a completed install until it is ready", async () => {
    const servers = queryResult({ mcpServers: [] });
    const remoteServers = queryResult({ remoteMcpServers: [] });
    const endpoints = queryResult({ mcpEndpoints: [] });
    const plugins = queryResult({ plugins: [] });
    queryHooks.servers.mockReturnValue(servers);
    queryHooks.remoteServers.mockReturnValue(remoteServers);
    queryHooks.endpoints.mockReturnValue(endpoints);
    queryHooks.plugins.mockReturnValue(plugins);
    workflowHook.mockReturnValue({
      phase: "complete",
      statuses: [
        {
          key: "linear",
          name: "Linear",
          status: "completed",
          mcpServerId: "mcp-server-id",
        },
      ],
      reset: resetInstall,
      isServerAlreadyInstalled: () => false,
    });
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result } = renderHook(() => useMcpGuideOperations());

    act(() => result.current.selectServer(SERVER));
    act(() =>
      result.current.handleSignal(
        { type: "start", scope: SERVER_SCOPE },
        report,
      ),
    );

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "error",
        scope: SERVER_SCOPE,
        message:
          "The governed endpoint or Default plugin is not ready yet. Retry the readiness check.",
      }),
    );
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" }),
    );
    expect(servers.refetch).toHaveBeenCalledOnce();
    expect(remoteServers.refetch).toHaveBeenCalledOnce();
    expect(endpoints.refetch).toHaveBeenCalledOnce();
    expect(plugins.refetch).toHaveBeenCalledOnce();
  });

  it("returns namespaced client commands and a list-plus-read-only-call prompt", () => {
    setExistingServer();
    const { result } = renderHook(() => useMcpGuideOperations());

    expect(result.current.connectionPrompts?.claude).toContain(
      "claude mcp add --transport http --scope user 'Linear_Governed'",
    );
    expect(result.current.connectionPrompts?.cursor).toContain(
      '"Linear_Governed"',
    );
    expect(result.current.connectionPrompts?.codex).toContain(
      "codex mcp add 'Linear_Governed'",
    );
    expect(result.current.prompt).toContain("Linear_Governed MCP server");
    expect(result.current.prompt).toMatch(/first list the available tools/i);
    expect(result.current.prompt).toContain(
      "https://api.example/mcp/linear-endpoint",
    );
    expect(result.current.prompt).toMatch(
      /same name.*use only the one at this URL/i,
    );
    expect(result.current.prompt).toMatch(/marked read-only/i);
    expect(result.current.prompt).toMatch(/do not create, update, or delete/i);
    expect(result.current.toolLogsHref).toBe("/projects/request-project/logs");

    expect(result.current.client).toBe("claude");
    expect(result.current.connectionPromptCopied).toBe(false);
    act(() => {
      result.current.setClient("codex");
      result.current.markConnectionPromptCopied();
    });
    expect(result.current.client).toBe("codex");
    expect(result.current.connectionPromptCopied).toBe(true);
  });

  it("captures the selected-server baseline once before prompt interaction", async () => {
    const now = Date.now();
    setExistingServer();
    refetchTraces.mockResolvedValueOnce({
      data: {
        traces: [
          toolTrace({
            id: "baseline-trace",
            startTimeUnixNano: String(BigInt(now - 1_000) * 1_000_000n),
          }),
        ],
      },
      isError: false,
    });
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result } = renderHook(() => useMcpGuideOperations());
    const promptScope = { ...SERVER_SCOPE, step: 2, runId: 2 };

    await act(async () => {
      expect(await result.current.prepareActivityBaseline()).toBe(true);
    });
    act(() => {
      result.current.handleSignal(
        { type: "checkpoint", scope: promptScope },
        report,
      );
    });

    expect(refetchTraces).toHaveBeenCalledOnce();
    expect(result.current.activityBaselineError).toBe(false);
  });

  it("completes from the first new selected-server trace and renders its real details", async () => {
    const now = Date.now();
    const traceData = {
      current: {
        traces: [
          toolTrace({
            id: "old-trace",
            startTimeUnixNano: String(BigInt(now - 1_000) * 1_000_000n),
          }),
        ],
      },
    };
    const refetchTraces = vi.fn(() =>
      Promise.resolve({ data: traceData.current, isError: false }),
    );
    queryHooks.toolTraces.mockImplementation(() => ({
      ...queryResult(traceData.current),
      refetch: refetchTraces,
    }));
    setExistingServer();
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const view = renderHook(() => useMcpGuideOperations());
    const listenScope = { ...SERVER_SCOPE, step: 3, runId: 3 };

    await act(async () => {
      expect(await view.result.current.prepareActivityBaseline()).toBe(true);
    });
    await waitFor(() => expect(refetchTraces).toHaveBeenCalledOnce());
    act(() =>
      view.result.current.handleSignal(
        { type: "start", scope: listenScope },
        report,
      ),
    );

    traceData.current = {
      traces: [
        toolTrace({
          id: "unrelated-trace",
          startTimeUnixNano: String(BigInt(now + 1_000) * 1_000_000n),
          targetId: "another-server",
          targetLabel: "Another server",
          toolName: "other.unrelated_call",
        }),
        toolTrace({
          id: "later-trace",
          startTimeUnixNano: String(BigInt(now + 3_000) * 1_000_000n),
          toolName: "linear.get_issue",
        }),
        toolTrace({
          id: "first-trace",
          startTimeUnixNano: String(BigInt(now + 2_000) * 1_000_000n),
        }),
      ],
    };
    view.rerender();

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "event",
        scope: listenScope,
        event: {
          kind: "Governed call",
          tone: "allow",
          title: "linear.list_issues",
          rows: [
            { key: "server", value: "Linear" },
            {
              key: "endpoint",
              value: "https://api.example/mcp/linear-endpoint",
            },
            { key: "result", value: "HTTP 200" },
          ],
          note: "The first new call is recorded in Tool Logs.",
        },
      }),
    );
  });

  it("keeps listening while the activity baseline is unresolved", async () => {
    setExistingServer();
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result } = renderHook(() => useMcpGuideOperations());
    const listenScope = { ...SERVER_SCOPE, step: 3, runId: 3 };
    refetchTraces.mockReturnValueOnce(new Promise(() => undefined));

    act(() => {
      void result.current.prepareActivityBaseline();
      result.current.handleSignal(
        { type: "start", scope: listenScope },
        report,
      );
    });

    expect(result.current.prompt).toMatch(/first list the available tools/i);
    expect(report).toHaveBeenCalledWith({
      type: "progress",
      scope: listenScope,
      message: "Listening for a new call on the selected governed endpoint",
    });
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "error" }),
    );
  });

  it("reports a real activity query failure after the baseline is ready", async () => {
    setExistingServer();
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result, rerender } = renderHook(() => useMcpGuideOperations());
    const listenScope = { ...SERVER_SCOPE, step: 3, runId: 3 };
    refetchTraces.mockResolvedValueOnce({
      data: { traces: [] },
      isError: false,
    });

    await act(async () => {
      expect(await result.current.prepareActivityBaseline()).toBe(true);
    });

    act(() => {
      result.current.handleSignal(
        { type: "start", scope: listenScope },
        report,
      );
    });
    queryHooks.toolTraces.mockReturnValue({
      data: undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: refetchTraces,
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
  });

  it("captures a fresh baseline after a failed baseline read is retried", async () => {
    setExistingServer();
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result } = renderHook(() => useMcpGuideOperations());
    refetchTraces.mockResolvedValueOnce({
      data: undefined,
      isError: true,
    });

    await act(async () => {
      expect(await result.current.prepareActivityBaseline()).toBe(false);
    });

    await waitFor(() => expect(refetchTraces).toHaveBeenCalledOnce());

    refetchTraces.mockResolvedValueOnce({
      data: {
        traces: [toolTrace({ id: "retry-baseline-trace" })],
      },
      isError: false,
    });

    act(() =>
      result.current.handleSignal(
        { type: "retry", scope: { ...SERVER_SCOPE, step: 3, runId: 3 } },
        report,
      ),
    );

    await waitFor(() => {
      expect(refetchTraces).toHaveBeenCalledTimes(2);
    });
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
  });
});
