import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import type { McpServerActivity } from "@gram/client/models/components/mcpserveractivity.js";
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
const refetchActivity = vi.hoisted(() =>
  vi.fn<
    () => Promise<{
      data: { activity: McpServerActivity[] } | undefined;
      isError: boolean;
    }>
  >(() => Promise.resolve({ data: { activity: [] }, isError: false })),
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
  refetchActivity.mockResolvedValue({
    data: { activity: [] },
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
          supportsDcr: false,
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
  it("exposes six curated catalog choices without installing a selection", () => {
    const { result } = renderHook(() => useMcpGuideOperations());

    expect(result.current.catalogServers).toHaveLength(6);

    act(() => result.current.selectServer(SERVER));

    expect(startInstall).not.toHaveBeenCalled();
    expect(workflowHook).toHaveBeenLastCalledWith({
      servers: [SERVER],
      projectSlug: "request-project",
      autoSelectRemotes: true,
    });
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
        progress: 0.5,
      }),
    );
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

  it("exposes the endpoint and Default plugin readiness for client setup", () => {
    setExistingServer();
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

    queryHooks.plugins.mockReturnValue(queryResult({ plugins: [] }));
    const incomplete = renderHook(() => useMcpGuideOperations());
    expect(incomplete.result.current.deploymentReady).toBe(false);
  });

  it("returns client connection prompts and a list-plus-read-only-call prompt", () => {
    setExistingServer();
    const { result } = renderHook(() => useMcpGuideOperations());

    expect(result.current.connectionPrompts?.claude).toContain(
      "Configure the remote Linear MCP server in my local Claude Code setup only.",
    );
    expect(result.current.connectionPrompts?.cursor).toContain(
      "in my local Cursor setup only.",
    );
    expect(result.current.connectionPrompts?.codex).toContain(
      "in my local Codex setup only.",
    );
    expect(result.current.prompt).toMatch(/first list the available tools/i);
    expect(result.current.prompt).toContain(
      "https://api.example/mcp/linear-endpoint",
    );
    expect(result.current.prompt).toMatch(
      /same name.*use only the one at this URL/i,
    );
    expect(result.current.prompt).toMatch(/marked read-only/i);
    expect(result.current.prompt).toMatch(/do not create, update, or delete/i);
    expect(result.current.mcpServerHref).toBe(
      "/projects/request-project/mcp/linear-governed",
    );
    expect(result.current.toolLogsHref).toBe("/projects/request-project/logs");

    expect(result.current.client).toBe("claude");
    expect(result.current.connectionPromptCopied).toBe(false);
    expect(result.current.promptCopied).toBe(false);
    act(() => {
      result.current.setClient("codex");
      result.current.markConnectionPromptCopied();
      result.current.markPromptCopied();
    });
    expect(result.current.client).toBe("codex");
    expect(result.current.connectionPromptCopied).toBe(true);
    expect(result.current.promptCopied).toBe(true);
  });

  it("awaits a fresh selected-server baseline before exposing prompt readiness", async () => {
    setExistingServer({ calls: 4 });
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result, rerender } = renderHook(() => useMcpGuideOperations());
    const promptScope = { ...SERVER_SCOPE, step: 2, runId: 2 };
    const listenScope = { ...SERVER_SCOPE, step: 3, runId: 3 };
    let resolveFreshRead!: (value: {
      data: { activity: McpServerActivity[] };
      isError: boolean;
    }) => void;
    refetchActivity.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveFreshRead = resolve;
      }),
    );

    act(() => {
      result.current.handleSignal(
        { type: "checkpoint", scope: promptScope },
        report,
      );
    });

    expect(refetchActivity).toHaveBeenCalledOnce();
    expect(result.current.activityBaselineReady).toBe(false);

    await act(async () => {
      resolveFreshRead({
        data: {
          activity: [
            {
              lastToolCallAt: new Date("2026-08-19T12:01:00Z"),
              recentToolCalls: 5,
              targetId: "linear-governed",
              targetLabel: "Linear",
              targetType: "hosted_mcp_server",
              totalToolCalls: 5,
            },
          ],
        },
        isError: false,
      });
    });

    await waitFor(() =>
      expect(result.current.activityBaselineReady).toBe(true),
    );

    setExistingServer({ calls: 5 });
    rerender();
    act(() => {
      result.current.handleSignal(
        { type: "start", scope: listenScope },
        report,
      );
    });
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "event" }),
    );

    setExistingServer({ calls: 6 });
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
            { key: "calls", value: "6 recorded" },
          ],
          note: "The new call is recorded in Tool Logs.",
        },
      }),
    );
  });

  it("keeps listening while the activity baseline is unresolved", async () => {
    setExistingServer({ calls: 4 });
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result } = renderHook(() => useMcpGuideOperations());
    const promptScope = { ...SERVER_SCOPE, step: 2, runId: 2 };
    const listenScope = { ...SERVER_SCOPE, step: 3, runId: 3 };
    refetchActivity.mockReturnValueOnce(new Promise(() => undefined));

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
    setExistingServer({ calls: 4 });
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result, rerender } = renderHook(() => useMcpGuideOperations());
    const promptScope = { ...SERVER_SCOPE, step: 2, runId: 2 };
    const listenScope = { ...SERVER_SCOPE, step: 3, runId: 3 };
    refetchActivity.mockResolvedValueOnce({
      data: {
        activity: [
          {
            lastToolCallAt: new Date("2026-08-19T12:01:00Z"),
            recentToolCalls: 4,
            targetId: "linear-governed",
            targetLabel: "Linear",
            targetType: "hosted_mcp_server",
            totalToolCalls: 4,
          },
        ],
      },
      isError: false,
    });

    act(() => {
      result.current.handleSignal(
        { type: "checkpoint", scope: promptScope },
        report,
      );
    });
    await waitFor(() =>
      expect(result.current.activityBaselineReady).toBe(true),
    );

    act(() => {
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
  });

  it("captures a fresh baseline after a failed baseline read is retried", async () => {
    setExistingServer({ calls: 2 });
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result } = renderHook(() => useMcpGuideOperations());
    const promptScope = { ...SERVER_SCOPE, step: 2, runId: 2 };
    refetchActivity.mockResolvedValueOnce({
      data: undefined,
      isError: true,
    });

    act(() => {
      result.current.handleSignal(
        { type: "checkpoint", scope: promptScope },
        report,
      );
    });

    await waitFor(() => {
      expect(result.current.activityError).toBe(true);
      expect(result.current.activityBaselineReady).toBe(false);
    });

    refetchActivity.mockResolvedValueOnce({
      data: {
        activity: [
          {
            lastToolCallAt: new Date("2026-08-19T12:02:00Z"),
            recentToolCalls: 3,
            targetId: "linear-governed",
            targetLabel: "Linear",
            targetType: "hosted_mcp_server",
            totalToolCalls: 3,
          },
        ],
      },
      isError: false,
    });

    act(() => result.current.retryActivity());

    await waitFor(() => {
      expect(refetchActivity).toHaveBeenCalledTimes(2);
      expect(result.current.activityError).toBe(false);
      expect(result.current.activityBaselineReady).toBe(true);
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
    expect(result.current.deploymentReady).toBe(false);
  });
});
