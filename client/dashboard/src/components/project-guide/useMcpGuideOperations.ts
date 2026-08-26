import {
  catalogBackedMcpServers,
  hasDefaultPluginServer,
} from "@/components/project-guide/journeyStatus";
import { AUTOMATIC_CATALOG_SERVER_NAMES } from "@/components/project-guide/journeys";
import type {
  ProjectGuideEventCard,
  ProjectGuideOperationReport,
  ProjectGuideOperationScope,
  ProjectGuideOperationSignal,
} from "@/components/project-guide/projectGuideMachine";
import { projectGuideOperationKey } from "@/components/project-guide/projectGuideMachine";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { type PulseMCPServer, useListMCPCatalog } from "@/pages/catalog/hooks";
import {
  extractAuthType,
  isPulseMcpServer,
  requiresManualSetup,
} from "@/pages/catalog/hooks/serverMetadata";
import {
  filterToHttpRemotes,
  normalizeRemoteUrl,
} from "@/pages/catalog/remotes";
import { useRemoteMcpInstallWorkflow } from "@/pages/catalog/useRemoteMcpInstallWorkflow";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { ToolUsageTraceSummary } from "@gram/client/models/components/toolusagetracesummary.js";
import type { ExternalMCPServer } from "@gram/client/models/components/externalmcpserver.js";
import { useListToolUsageTraces } from "@gram/client/react-query/listToolUsageTraces.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export type McpGuideClient = "claude" | "cursor" | "codex";

type ActiveOperation = {
  scope: ProjectGuideOperationScope;
  report: (report: ProjectGuideOperationReport) => void;
  paused: boolean;
};

type ActivityBaseline = {
  capturedAtMs: number;
  traceIds: Set<string>;
};

const CLIENTS: McpGuideClient[] = ["claude", "codex", "cursor"];

function serverName(server: PulseMCPServer): string {
  return server.title ?? server.registrySpecifier;
}

function compareCatalogServers(
  left: PulseMCPServer,
  right: PulseMCPServer,
): number {
  const rank = (server: PulseMCPServer): number => {
    const index = AUTOMATIC_CATALOG_SERVER_NAMES.indexOf(
      serverName(server) as (typeof AUTOMATIC_CATALOG_SERVER_NAMES)[number],
    );
    return index === -1 ? Number.POSITIVE_INFINITY : index;
  };
  return (
    rank(left) - rank(right) ||
    serverName(left).localeCompare(serverName(right))
  );
}

function canConfigureWithoutUserCredentials(server: PulseMCPServer): boolean {
  const authType = extractAuthType(server);
  return (
    authType === "none" ||
    (authType === "oauth" && !requiresManualSetup(server))
  );
}

function curateCatalogServers(
  servers: ExternalMCPServer[] | undefined,
): PulseMCPServer[] | undefined {
  if (!servers) return undefined;
  return servers
    .filter(isPulseMcpServer)
    .filter(canConfigureWithoutUserCredentials)
    .filter((server) =>
      AUTOMATIC_CATALOG_SERVER_NAMES.includes(
        serverName(server) as (typeof AUTOMATIC_CATALOG_SERVER_NAMES)[number],
      ),
    )
    .sort(compareCatalogServers);
}

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

function governedClientName(name: string): string {
  return name.endsWith("_Governed") ? name : `${name}_Governed`;
}

function connectionCommandFor(
  client: McpGuideClient,
  serverName: string,
  endpointUrl: string,
): string {
  switch (client) {
    case "claude":
      return `claude mcp add --transport http --scope user ${shellQuote(serverName)} ${shellQuote(endpointUrl)}`;
    case "codex":
      return `codex mcp add ${shellQuote(serverName)} --url ${shellQuote(endpointUrl)}`;
    case "cursor":
      return JSON.stringify(
        {
          mcpServers: {
            [serverName]: { type: "http", url: endpointUrl },
          },
        },
        null,
        2,
      );
  }
}

function connectionCommandsFor(
  serverName: string,
  endpointUrl: string,
): Record<McpGuideClient, string> {
  return {
    claude: connectionCommandFor("claude", serverName, endpointUrl),
    cursor: connectionCommandFor("cursor", serverName, endpointUrl),
    codex: connectionCommandFor("codex", serverName, endpointUrl),
  };
}

function promptFor(name: string, endpointUrl: string): string {
  return `Using the ${name} MCP server at this exact URL, ${endpointUrl}, first list the available tools. If multiple servers have the same name, use only the one at this URL. Then choose one tool marked read-only and call it with a harmless request. Do not create, update, or delete anything. Summarize the result, and do not call any tool unless it is marked read-only.`;
}

function traceTimeMs(trace: ToolUsageTraceSummary): number | undefined {
  try {
    return Number(BigInt(trace.startTimeUnixNano) / 1_000_000n);
  } catch {
    return undefined;
  }
}

function firstNewTrace(
  baseline: ActivityBaseline,
  traces: ToolUsageTraceSummary[] | undefined,
  server: McpServer | undefined,
): ToolUsageTraceSummary | undefined {
  if (!server?.slug) return undefined;
  return traces
    ?.filter((trace) => {
      const observedAtMs = traceTimeMs(trace);
      return (
        trace.targetType === "hosted_mcp_server" &&
        trace.targetId === server.slug &&
        !baseline.traceIds.has(trace.id) &&
        observedAtMs !== undefined &&
        observedAtMs >= baseline.capturedAtMs
      );
    })
    .sort(
      (left, right) =>
        (traceTimeMs(left) ?? Number.POSITIVE_INFINITY) -
        (traceTimeMs(right) ?? Number.POSITIVE_INFINITY),
    )[0];
}

function selectedServerReadiness(
  servers: McpServer[],
  remoteServers: RemoteMcpServer[],
  endpoints: McpEndpoint[],
  plugins: Plugin[],
  selectedServer: PulseMCPServer,
): boolean {
  const server = catalogBackedMcpServers(servers, remoteServers, [
    filterToHttpRemotes(selectedServer),
  ])[0];
  return Boolean(
    server &&
    endpoints.some(
      (endpoint) => endpoint.mcpServerId === server.id && endpoint.slug,
    ) &&
    hasDefaultPluginServer(plugins, server.id),
  );
}

function catalogServerForMcp(
  mcpServer: McpServer | undefined,
  remoteServers: Array<{ id: string; url: string }> | undefined,
  catalogServers: PulseMCPServer[] | undefined,
): PulseMCPServer | undefined {
  const remote = remoteServers?.find(
    (candidate) => candidate.id === mcpServer?.remoteMcpServerId,
  );
  if (!remote) return undefined;
  const url = normalizeRemoteUrl(remote.url);
  return catalogServers?.find((server) =>
    server.remotes?.some(
      (candidate) => normalizeRemoteUrl(candidate.url) === url,
    ),
  );
}

export function useMcpGuideOperations(): {
  activityBaselineError: boolean;
  activityBaselinePending: boolean;
  catalogError: boolean;
  catalogPending: boolean;
  catalogServers: PulseMCPServer[] | undefined;
  client: McpGuideClient;
  connectionPromptCopied: boolean;
  endpointUrl: string | undefined;
  handleSignal: (
    signal: ProjectGuideOperationSignal,
    report: (report: ProjectGuideOperationReport) => void,
  ) => void;
  markConnectionPromptCopied: () => void;
  mcpServer: McpServer | undefined;
  projectStateError: boolean;
  projectStatePending: boolean;
  prompt: string | undefined;
  prepareActivityBaseline: () => Promise<boolean>;
  retryCatalog: () => void;
  selectServer: (server: PulseMCPServer) => void;
  selectedServer: PulseMCPServer | undefined;
  serverName: string | undefined;
  setClient: (client: McpGuideClient) => void;
  connectionPrompts: Record<McpGuideClient, string> | undefined;
  toolLogsHref: string;
} {
  const gramProject = useProjectSlugForRequests();
  const routes = useRoutes();
  const [selectedServer, setSelectedServer] = useState<
    PulseMCPServer | undefined
  >(undefined);
  const [client, setClient] = useState<McpGuideClient>("claude");
  const [connectionPromptCopied, setConnectionPromptCopied] = useState(false);
  const [activeOperation, setActiveOperation] = useState<
    ActiveOperation | undefined
  >(undefined);
  const activeOperationRef = useRef<ActiveOperation | undefined>(undefined);
  const [, setActivityBaseline] = useState<ActivityBaseline>();
  const activityBaselineRef = useRef<ActivityBaseline | undefined>(undefined);
  const captureActivityBaselineRef = useRef<() => Promise<boolean>>(() =>
    Promise.resolve(false),
  );
  const installStartedFor = useRef<string | undefined>(undefined);
  const progressReportedFor = useRef(new Set<string>());
  const readinessCheckedFor = useRef(new Set<string>());
  const [suppressActivityError, setSuppressActivityError] = useState(false);
  const [baselineCaptureError, setBaselineCaptureError] = useState(false);
  const [baselineCapturePending, setBaselineCapturePending] = useState(false);

  const catalog = useListMCPCatalog();
  const serversQuery = useMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });
  const endpointsQuery = useMcpEndpoints({ gramProject }, undefined, {
    throwOnError: false,
  });
  const pluginsQuery = usePlugins({ gramProject }, undefined, {
    throwOnError: false,
  });
  const remoteServersQuery = useRemoteMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });

  const allCatalogServers = useMemo(
    () =>
      catalog.data?.servers?.filter(isPulseMcpServer).map(filterToHttpRemotes),
    [catalog.data?.servers],
  );
  const catalogServers = useMemo(
    () =>
      catalog.isError ? undefined : curateCatalogServers(catalog.data?.servers),
    [catalog.data?.servers, catalog.isError],
  );
  const catalogError =
    catalog.isError || (!catalog.isPending && catalog.data === undefined);

  const projectStatePending =
    serversQuery.isPending ||
    endpointsQuery.isPending ||
    pluginsQuery.isPending ||
    remoteServersQuery.isPending;
  const projectStateError =
    serversQuery.isError ||
    endpointsQuery.isError ||
    pluginsQuery.isError ||
    remoteServersQuery.isError ||
    (!projectStatePending &&
      (serversQuery.data === undefined ||
        endpointsQuery.data === undefined ||
        pluginsQuery.data === undefined ||
        remoteServersQuery.data === undefined));
  const projectDataDefined =
    !projectStateError &&
    serversQuery.data !== undefined &&
    endpointsQuery.data !== undefined &&
    pluginsQuery.data !== undefined &&
    remoteServersQuery.data !== undefined;
  const mcpServer = projectDataDefined
    ? catalogBackedMcpServers(
        serversQuery.data.mcpServers,
        remoteServersQuery.data.remoteMcpServers,
        selectedServer
          ? [filterToHttpRemotes(selectedServer)]
          : allCatalogServers,
      )[0]
    : undefined;
  const endpoint = projectDataDefined
    ? endpointsQuery.data.mcpEndpoints.find(
        (candidate) => candidate.mcpServerId === mcpServer?.id,
      )
    : undefined;
  const endpointUrl = endpoint?.slug
    ? `${getServerURL()}/mcp/${endpoint.slug}`
    : undefined;
  const matchedCatalogServer = catalogServerForMcp(
    mcpServer,
    remoteServersQuery.data?.remoteMcpServers,
    allCatalogServers,
  );
  const resolvedName =
    mcpServer?.name ??
    (selectedServer ? serverName(selectedServer) : undefined) ??
    (matchedCatalogServer ? serverName(matchedCatalogServer) : undefined);
  const clientName = resolvedName
    ? governedClientName(resolvedName)
    : undefined;

  const workflowServers = useMemo(
    () =>
      selectedServer && projectDataDefined && !mcpServer
        ? [filterToHttpRemotes(selectedServer)]
        : [],
    [mcpServer, projectDataDefined, selectedServer],
  );
  const workflow = useRemoteMcpInstallWorkflow({
    servers: workflowServers,
    projectSlug: gramProject,
    autoSelectRemotes: true,
    serverNameSuffix: "_Governed",
  });

  const tracesRequest = useMemo(
    () => ({
      gramProject,
      listToolUsageTracesPayload: {
        from: new Date(0),
        to: new Date(),
        hostedToolsetSlugs: mcpServer?.slug ? [mcpServer.slug] : undefined,
        targetTypes: ["hosted_mcp_server"] as Array<"hosted_mcp_server">,
        limit: 20,
        sort: "asc" as const,
      },
    }),
    [gramProject, mcpServer?.slug],
  );
  const activityQuery = useListToolUsageTraces(tracesRequest, undefined, {
    enabled: Boolean(endpointUrl && mcpServer?.slug),
    refetchInterval: () => {
      tracesRequest.listToolUsageTracesPayload.to = new Date();
      return activeOperation?.scope.step === 3 && !activeOperation.paused
        ? 5_000
        : false;
    },
    throwOnError: false,
  });
  const queryActivityError =
    activityQuery.isError ||
    (Boolean(endpointUrl && mcpServer?.slug) &&
      !activityQuery.isPending &&
      activityQuery.data === undefined);
  const activityError = baselineCaptureError || queryActivityError;

  const connectionPrompts =
    endpointUrl && clientName
      ? connectionCommandsFor(clientName, endpointUrl)
      : undefined;
  const prompt =
    clientName && endpointUrl ? promptFor(clientName, endpointUrl) : undefined;

  const updateActiveOperation = useCallback(
    (operation: ActiveOperation | undefined) => {
      activeOperationRef.current = operation;
      setActiveOperation(operation);
    },
    [],
  );

  const captureActivityBaseline = useCallback(async (): Promise<boolean> => {
    activityBaselineRef.current = undefined;
    setActivityBaseline(undefined);
    setBaselineCaptureError(false);
    setBaselineCapturePending(true);
    setSuppressActivityError(true);
    tracesRequest.listToolUsageTracesPayload.to = new Date();
    try {
      const result = await activityQuery.refetch();
      if (result.isError || !result.data) {
        setBaselineCaptureError(true);
        return false;
      }
      const latestTraceMs = result.data.traces.reduce(
        (latest, trace) => Math.max(latest, traceTimeMs(trace) ?? 0),
        0,
      );
      const baseline = {
        capturedAtMs: Math.max(Date.now(), latestTraceMs),
        traceIds: new Set(result.data.traces.map((trace) => trace.id)),
      };
      activityBaselineRef.current = baseline;
      setActivityBaseline(baseline);
      tracesRequest.listToolUsageTracesPayload.from = new Date(
        baseline.capturedAtMs,
      );
      tracesRequest.listToolUsageTracesPayload.to = new Date();
      return true;
    } catch {
      setBaselineCaptureError(true);
      return false;
    } finally {
      setBaselineCapturePending(false);
      setSuppressActivityError(false);
    }
  }, [activityQuery, tracesRequest]);
  captureActivityBaselineRef.current = captureActivityBaseline;

  const refetchSelectedServerReadiness = useCallback(async (): Promise<
    "ready" | "not-ready" | "unreadable"
  > => {
    if (!selectedServer) return "not-ready";
    try {
      const [servers, remoteServers, endpoints, plugins] = await Promise.all([
        serversQuery.refetch(),
        remoteServersQuery.refetch(),
        endpointsQuery.refetch(),
        pluginsQuery.refetch(),
      ]);
      if (
        servers.isError ||
        remoteServers.isError ||
        endpoints.isError ||
        plugins.isError ||
        !servers.data ||
        !remoteServers.data ||
        !endpoints.data ||
        !plugins.data
      ) {
        return "unreadable";
      }
      return selectedServerReadiness(
        servers.data.mcpServers,
        remoteServers.data.remoteMcpServers,
        endpoints.data.mcpEndpoints,
        plugins.data.plugins,
        selectedServer,
      )
        ? "ready"
        : "not-ready";
    } catch {
      return "unreadable";
    }
  }, [
    endpointsQuery,
    pluginsQuery,
    remoteServersQuery,
    selectedServer,
    serversQuery,
  ]);

  const retryActivity = useCallback(() => {
    if (!activityBaselineRef.current) {
      void captureActivityBaseline();
      return;
    }
    setSuppressActivityError(true);
    tracesRequest.listToolUsageTracesPayload.to = new Date();
    void activityQuery.refetch().finally(() => setSuppressActivityError(false));
  }, [activityQuery, captureActivityBaseline, tracesRequest]);

  const handleSignal = useCallback(
    (
      signal: ProjectGuideOperationSignal,
      report: (report: ProjectGuideOperationReport) => void,
    ): void => {
      if (signal.scope.path !== "third-party-mcp") return;

      if (signal.type === "abort") {
        updateActiveOperation(undefined);
        activityBaselineRef.current = undefined;
        setActivityBaseline(undefined);
        return;
      }
      if (signal.type === "prepare") {
        void captureActivityBaselineRef.current().then((ready) => {
          if (ready) {
            report({
              type: "success",
              scope: signal.scope,
              result: `${resolvedName ?? "Selected"} mcp server is now setup`,
            });
          } else {
            report({
              type: "error",
              scope: signal.scope,
              message: "We couldn't prepare the connection yet. Try again.",
            });
          }
        });
        return;
      }
      if (signal.type === "pause") {
        const current = activeOperationRef.current;
        if (
          current &&
          projectGuideOperationKey(current.scope) ===
            projectGuideOperationKey(signal.scope)
        ) {
          updateActiveOperation({ ...current, paused: true });
        }
        return;
      }
      if (signal.type === "checkpoint") return;

      updateActiveOperation({ scope: signal.scope, report, paused: false });
      if (signal.type === "start" && signal.scope.step === 3) {
        report({
          type: "progress",
          scope: signal.scope,
          message: "Listening for a new call on the selected governed endpoint",
        });
      }
      if (signal.type === "retry") {
        if (signal.scope.step === 0) workflow.reset();
        if (signal.scope.step === 3) retryActivity();
      }
    },
    [resolvedName, retryActivity, updateActiveOperation, workflow],
  );

  useEffect(() => {
    const operation = activeOperation;
    if (!operation || operation.paused || operation.scope.step !== 0) return;
    const key = projectGuideOperationKey(operation.scope);
    const finishReadinessCheck = (
      readiness: "ready" | "not-ready" | "unreadable",
    ): void => {
      const current = activeOperationRef.current;
      if (
        !current ||
        projectGuideOperationKey(current.scope) !==
          projectGuideOperationKey(operation.scope)
      ) {
        return;
      }
      updateActiveOperation(undefined);
      if (readiness === "ready") {
        operation.report({
          type: "success",
          scope: operation.scope,
          result: `${resolvedName ?? "Catalog server"} governed endpoint and Default plugin verified`,
        });
        return;
      }
      operation.report({
        type: "error",
        scope: operation.scope,
        message:
          readiness === "unreadable"
            ? "Could not verify the governed endpoint and Default plugin. Retry the readiness check."
            : "The governed endpoint or Default plugin is not ready yet. Retry the readiness check.",
      });
    };
    if (!selectedServer) {
      updateActiveOperation(undefined);
      operation.report({
        type: "error",
        scope: operation.scope,
        message: catalogError
          ? "Could not load the automatic catalog servers. Retry the catalog check."
          : "Choose a catalog server before starting the journey.",
      });
      return;
    }
    if (projectStatePending) return;
    if (projectStateError || !projectDataDefined) {
      updateActiveOperation(undefined);
      operation.report({
        type: "error",
        scope: operation.scope,
        message:
          "Could not read this project's existing MCP servers. Retry the project check before installing.",
      });
      return;
    }
    if (mcpServer) {
      if (
        selectedServerReadiness(
          serversQuery.data?.mcpServers ?? [],
          remoteServersQuery.data?.remoteMcpServers ?? [],
          endpointsQuery.data?.mcpEndpoints ?? [],
          pluginsQuery.data?.plugins ?? [],
          selectedServer,
        )
      ) {
        updateActiveOperation(undefined);
        operation.report({
          type: "success",
          scope: operation.scope,
          result: `${resolvedName ?? serverName(selectedServer)} governed endpoint and Default plugin verified`,
        });
      } else {
        if (readinessCheckedFor.current.has(key)) return;
        readinessCheckedFor.current.add(key);
        void refetchSelectedServerReadiness().then(finishReadinessCheck);
      }
      return;
    }
    if (!progressReportedFor.current.has(key)) {
      progressReportedFor.current.add(key);
      const name = serverName(selectedServer);
      operation.report({
        type: "progress",
        scope: operation.scope,
        message: "Reading the server's tool list",
        progress: 0.2,
      });
      operation.report({
        type: "progress",
        scope: operation.scope,
        message: `Installing ${name} into this project`,
        progress: 0.5,
      });
      if (extractAuthType(selectedServer) === "oauth") {
        operation.report({
          type: "progress",
          scope: operation.scope,
          message: `Configuring OAuth for ${name}`,
          progress: 0.75,
        });
      }
    }
    if (
      workflow.phase === "configure" &&
      workflow.canInstall &&
      installStartedFor.current !== key
    ) {
      installStartedFor.current = key;
      void workflow.startInstall();
    }
    if (workflow.phase !== "complete") return;

    const failure = workflow.statuses.find(
      (status) => status.status === "failed",
    );
    if (failure) {
      updateActiveOperation(undefined);
      operation.report({
        type: "error",
        scope: operation.scope,
        message:
          failure.error ??
          "The server installation failed. Retry to run the existing install workflow again.",
      });
      return;
    }
    if (readinessCheckedFor.current.has(key)) return;
    readinessCheckedFor.current.add(key);
    void refetchSelectedServerReadiness().then(finishReadinessCheck);
  }, [
    activeOperation,
    catalogError,
    mcpServer,
    projectDataDefined,
    projectStateError,
    projectStatePending,
    refetchSelectedServerReadiness,
    resolvedName,
    selectedServer,
    endpointsQuery.data?.mcpEndpoints,
    pluginsQuery.data?.plugins,
    remoteServersQuery.data?.remoteMcpServers,
    serversQuery.data?.mcpServers,
    updateActiveOperation,
    workflow,
  ]);

  useEffect(() => {
    const operation = activeOperation;
    if (!operation || operation.paused || operation.scope.step !== 3) return;
    const baseline = activityBaselineRef.current;
    if (!baseline) return;
    if (suppressActivityError || activityQuery.isPending) return;
    if (activityError) {
      updateActiveOperation(undefined);
      operation.report({
        type: "error",
        scope: operation.scope,
        message:
          "Could not check for the new governed call. Retry after checking the client connection.",
      });
      return;
    }
    const trace = firstNewTrace(
      baseline,
      activityQuery.data?.traces,
      mcpServer,
    );
    if (!trace || !endpointUrl) return;

    const event: ProjectGuideEventCard = {
      kind: "Governed call",
      tone: "allow",
      title: trace.toolName,
      rows: [
        { key: "server", value: trace.targetLabel },
        { key: "endpoint", value: endpointUrl },
        {
          key: "result",
          value:
            trace.httpStatusCode === undefined
              ? trace.eventSource
              : `HTTP ${trace.httpStatusCode}`,
        },
      ],
      note: "The first new call is recorded in Tool Logs.",
    };
    updateActiveOperation(undefined);
    operation.report({ type: "event", scope: operation.scope, event });
  }, [
    activeOperation,
    activityError,
    activityQuery.data?.traces,
    activityQuery.isPending,
    endpointUrl,
    mcpServer,
    suppressActivityError,
    updateActiveOperation,
  ]);

  return {
    activityBaselineError: baselineCaptureError,
    activityBaselinePending: baselineCapturePending,
    catalogError,
    catalogPending: catalog.isPending,
    catalogServers,
    client,
    connectionPromptCopied,
    endpointUrl,
    handleSignal,
    markConnectionPromptCopied: () => setConnectionPromptCopied(true),
    mcpServer,
    projectStateError,
    projectStatePending,
    prompt,
    prepareActivityBaseline: captureActivityBaseline,
    retryCatalog: () => {
      void catalog.refetch();
    },
    selectServer: (server) => {
      setSelectedServer(server);
      setConnectionPromptCopied(false);
      activityBaselineRef.current = undefined;
      setActivityBaseline(undefined);
      setBaselineCaptureError(false);
    },
    selectedServer,
    serverName: resolvedName,
    setClient: (nextClient) => {
      setClient(nextClient);
      setConnectionPromptCopied(false);
    },
    connectionPrompts,
    toolLogsHref: routes.logs.href(),
  };
}

export { CLIENTS as MCP_GUIDE_CLIENTS };
