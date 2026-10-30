import {
  claudeCodeAddCommand,
  codexAddCommand,
} from "@/lib/mcp-client-install";
import {
  catalogBackedMcpServers,
  findMcpServerActivity,
  hasDefaultPluginServer,
} from "@/components/project-guide/journeyStatus";
import { AUTOMATIC_CATALOG_SERVER_SPECIFIERS } from "@/components/project-guide/journeys";
import type {
  ProjectGuideEventCard,
  ProjectGuideOperationReport,
  ProjectGuideOperationScope,
  ProjectGuideOperationSignal,
} from "@/components/project-guide/projectGuideMachine";
import { projectGuideOperationKey } from "@/components/project-guide/projectGuideMachine";
import {
  markProjectGuideMcpServerSelected,
  useProjectGuideMcpServerSelection,
} from "@/components/project-guide/projectGuideStores";
import { useProjectSlugForRequests, useSlugs } from "@/contexts/Sdk";
import { mcpServerRouteParam } from "@/lib/sources";
import { getServerURL } from "@/lib/utils";
import { type CatalogServer, useListMCPCatalog } from "@/pages/catalog/hooks";
import {
  extractAuthType,
  isCatalogServer,
  supportsAutomaticOAuth,
} from "@/pages/catalog/hooks/serverMetadata";
import {
  filterToHttpRemotes,
  normalizeRemoteUrl,
} from "@/pages/catalog/remotes";
import { useRemoteMcpInstallWorkflow } from "@/pages/catalog/useRemoteMcpInstallWorkflow";
import { useCanViewToolLogs } from "@/components/observe/observeDeepLink";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { McpServerActivity } from "@gram/client/models/components/mcpserveractivity.js";
import type { ExternalMCPServer } from "@gram/client/models/components/externalmcpserver.js";
import {
  invalidateAllGetMcpServerActivity,
  useGetMcpServerActivity,
} from "@gram/client/react-query/getMcpServerActivity.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export type McpGuideClient = "claude" | "cursor" | "codex";

type ActiveOperation = {
  scope: ProjectGuideOperationScope;
  report: (report: ProjectGuideOperationReport) => void;
  paused: boolean;
};

// The server's call count and latest call time when the guide started
// listening. Detection reads the project:read activity aggregate rather than
// individual calls, which need log:read and so are closed to most guide users.
type ActivityBaseline = {
  capturedAtMs: number;
  totalToolCalls: number;
  lastToolCallAtMs: number;
};

// The activity aggregate reports the last call to the second, so a call made
// in the second listening started can read as up to a second earlier.
const LAST_CALL_PRECISION_MS = 1_000;

const CLIENTS: McpGuideClient[] = ["claude", "codex", "cursor"];

function serverName(server: CatalogServer): string {
  return server.title ?? server.registrySpecifier;
}

function compareCatalogServers(
  left: CatalogServer,
  right: CatalogServer,
): number {
  const rank = (server: CatalogServer): number => {
    const index = AUTOMATIC_CATALOG_SERVER_SPECIFIERS.indexOf(
      server.registrySpecifier as (typeof AUTOMATIC_CATALOG_SERVER_SPECIFIERS)[number],
    );
    return index === -1 ? Number.POSITIVE_INFINITY : index;
  };
  return (
    rank(left) - rank(right) ||
    serverName(left).localeCompare(serverName(right))
  );
}

function canConfigureWithoutUserCredentials(server: CatalogServer): boolean {
  const authType = extractAuthType(server);
  return (
    authType === "none" ||
    (authType === "oauth" && supportsAutomaticOAuth(server))
  );
}

function curateCatalogServers(
  servers: ExternalMCPServer[] | undefined,
): CatalogServer[] | undefined {
  if (!servers) return undefined;
  return servers
    .filter(isCatalogServer)
    .filter(canConfigureWithoutUserCredentials)
    .filter((server) =>
      AUTOMATIC_CATALOG_SERVER_SPECIFIERS.includes(
        server.registrySpecifier as (typeof AUTOMATIC_CATALOG_SERVER_SPECIFIERS)[number],
      ),
    )
    .sort(compareCatalogServers);
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
      return claudeCodeAddCommand(serverName, endpointUrl);
    case "codex":
      return codexAddCommand(serverName, endpointUrl);
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

function lastToolCallAtMs(entry: McpServerActivity | undefined): number {
  const ms = entry?.lastToolCallAt?.getTime();
  return ms === undefined || Number.isNaN(ms) ? 0 : ms;
}

function activityBaselineFor(
  entry: McpServerActivity | undefined,
  capturedAtMs: number,
): ActivityBaseline {
  return {
    capturedAtMs,
    totalToolCalls: entry?.totalToolCalls ?? 0,
    lastToolCallAtMs: lastToolCallAtMs(entry),
  };
}

// A later call shows up as a higher count or a later last-call time. Either is
// enough: the count is over a sliding lookback window, so an old call aging out
// can cancel a new one, while the last-call time only has second precision.
// The last call must also fall after listening started, so a call made before
// then but ingested late, or history on a server the baseline could not yet
// see, does not count as the new call.
function hasNewActivity(
  baseline: ActivityBaseline,
  entry: McpServerActivity | undefined,
): entry is McpServerActivity {
  if (!entry) return false;
  const lastCallMs = lastToolCallAtMs(entry);
  if (lastCallMs < baseline.capturedAtMs - LAST_CALL_PRECISION_MS) return false;
  return (
    entry.totalToolCalls > baseline.totalToolCalls ||
    lastCallMs > baseline.lastToolCallAtMs
  );
}

function selectedServerReadiness(
  servers: McpServer[],
  remoteServers: RemoteMcpServer[],
  endpoints: McpEndpoint[],
  plugins: Plugin[],
  selectedServer: CatalogServer,
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
  catalogServers: CatalogServer[] | undefined,
): CatalogServer | undefined {
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
  catalogServers: CatalogServer[] | undefined;
  client: McpGuideClient;
  connectionPromptCopied: boolean;
  promptCopied: boolean;
  endpointUrl: string | undefined;
  handleSignal: (
    signal: ProjectGuideOperationSignal,
    report: (report: ProjectGuideOperationReport) => void,
  ) => void;
  markConnectionPromptCopied: () => void;
  markPromptCopied: () => void;
  mcpServer: McpServer | undefined;
  projectStateError: boolean;
  projectStatePending: boolean;
  prompt: string | undefined;
  prepareActivityBaseline: () => Promise<boolean>;
  retryCatalog: () => void;
  selectServer: (server: CatalogServer) => void;
  selectedServer: CatalogServer | undefined;
  serverName: string | undefined;
  setClient: (client: McpGuideClient) => void;
  connectionPrompts: Record<McpGuideClient, string> | undefined;
  /** Undefined when the caller cannot open Tool Logs (it needs log:read). */
  toolLogsHref: string | undefined;
  /** The governed server's overview, where its call activity is shown. */
  mcpServerHref: string;
} {
  const gramProject = useProjectSlugForRequests();
  const { orgSlug, projectSlug } = useSlugs();
  const routes = useRoutes();
  const canViewToolLogs = useCanViewToolLogs();
  const queryClient = useQueryClient();
  const [selectedServerOverride, setSelectedServerOverride] = useState<
    CatalogServer | undefined
  >(undefined);
  const [client, setClient] = useState<McpGuideClient>("claude");
  const [connectionPromptCopied, setConnectionPromptCopied] = useState(false);
  const [promptCopied, setPromptCopied] = useState(false);
  const [activeOperation, setActiveOperation] = useState<
    ActiveOperation | undefined
  >(undefined);
  const activeOperationRef = useRef<ActiveOperation | undefined>(undefined);
  const [, setActivityBaseline] = useState<ActivityBaseline>();
  const activityBaselineRef = useRef<ActivityBaseline | undefined>(undefined);
  const activityBaselineGenerationRef = useRef(0);
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
      catalog.data?.servers?.filter(isCatalogServer).map(filterToHttpRemotes),
    [catalog.data?.servers],
  );
  const catalogServers = useMemo(
    () =>
      catalog.isError ? undefined : curateCatalogServers(catalog.data?.servers),
    [catalog.data?.servers, catalog.isError],
  );
  const catalogError =
    catalog.isError || (!catalog.isPending && catalog.data === undefined);
  const selectedMcpServerSpecifier = useProjectGuideMcpServerSelection(
    orgSlug,
    projectSlug,
  );
  const selectedServer =
    selectedServerOverride ??
    catalogServers?.find(
      (server) => server.registrySpecifier === selectedMcpServerSpecifier,
    );

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

  const activityQuery = useGetMcpServerActivity(
    { gramProject, getMcpServerActivityPayload: {} },
    undefined,
    {
      enabled: Boolean(endpointUrl && mcpServer?.slug),
      refetchInterval: () =>
        activeOperation?.scope.step === 3 && !activeOperation.paused
          ? 5_000
          : false,
      throwOnError: false,
    },
  );
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
    const generation = activityBaselineGenerationRef.current + 1;
    activityBaselineGenerationRef.current = generation;
    activityBaselineRef.current = undefined;
    setActivityBaseline(undefined);
    setBaselineCaptureError(false);
    setBaselineCapturePending(true);
    setSuppressActivityError(true);
    try {
      const result = await activityQuery.refetch();
      if (activityBaselineGenerationRef.current !== generation) return false;
      if (result.isError || !result.data) {
        setBaselineCaptureError(true);
        return false;
      }
      const baseline = activityBaselineFor(
        findMcpServerActivity(result.data.activity, mcpServer),
        Date.now(),
      );
      activityBaselineRef.current = baseline;
      setActivityBaseline(baseline);
      return true;
    } catch {
      if (activityBaselineGenerationRef.current !== generation) return false;
      setBaselineCaptureError(true);
      return false;
    } finally {
      if (activityBaselineGenerationRef.current === generation) {
        setBaselineCapturePending(false);
        setSuppressActivityError(false);
      }
    }
  }, [activityQuery, mcpServer]);
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

  const reportBaselineFailure = useCallback(
    (
      report: (report: ProjectGuideOperationReport) => void,
      scope: ProjectGuideOperationScope,
    ) => {
      const baselineCapture = captureActivityBaselineRef.current();
      const generation = activityBaselineGenerationRef.current;
      void baselineCapture.then((ready) => {
        if (activityBaselineGenerationRef.current !== generation || ready) {
          return;
        }
        updateActiveOperation(undefined);
        report({
          type: "error",
          scope,
          message: "We couldn't prepare the connection yet. Try again.",
        });
      });
    },
    [updateActiveOperation],
  );

  const retryActivity = useCallback(
    (
      report: (report: ProjectGuideOperationReport) => void,
      scope: ProjectGuideOperationScope,
    ) => {
      if (!activityBaselineRef.current) {
        reportBaselineFailure(report, scope);
        return;
      }
      setSuppressActivityError(true);
      void activityQuery
        .refetch()
        .finally(() => setSuppressActivityError(false));
    },
    [activityQuery, reportBaselineFailure],
  );

  const handleSignal = useCallback(
    (
      signal: ProjectGuideOperationSignal,
      report: (report: ProjectGuideOperationReport) => void,
    ): void => {
      if (signal.scope.path !== "third-party-mcp") return;

      if (signal.type === "abort") {
        updateActiveOperation(undefined);
        activityBaselineGenerationRef.current += 1;
        activityBaselineRef.current = undefined;
        setActivityBaseline(undefined);
        setBaselineCapturePending(false);
        setSuppressActivityError(false);
        return;
      }
      if (signal.type === "prepare") {
        const baselineCapture = captureActivityBaselineRef.current();
        const generation = activityBaselineGenerationRef.current;
        void baselineCapture.then((ready) => {
          if (activityBaselineGenerationRef.current !== generation) return;
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
        if (!activityBaselineRef.current) {
          reportBaselineFailure(report, signal.scope);
        }
      }
      if (signal.type === "retry") {
        if (signal.scope.step === 0) workflow.reset();
        if (signal.scope.step === 3) retryActivity(report, signal.scope);
      }
    },
    [
      reportBaselineFailure,
      resolvedName,
      retryActivity,
      updateActiveOperation,
      workflow,
    ],
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
          result: `${resolvedName ?? "Catalog server"} MCP setup ready`,
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
          result: `${resolvedName ?? serverName(selectedServer)} MCP setup ready`,
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
        message: `Adding ${name} MCP server to this project`,
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
    if (workflow.phase === "configure" && workflow.installBlockedReason) {
      updateActiveOperation(undefined);
      operation.report({
        type: "error",
        scope: operation.scope,
        message: workflow.installBlockedReason,
      });
      return;
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
    const entry = findMcpServerActivity(
      activityQuery.data?.activity,
      mcpServer,
    );
    if (!hasNewActivity(baseline, entry) || !endpointUrl) return;

    const event: ProjectGuideEventCard = {
      kind: "Governed call",
      tone: "allow",
      title: "tool call recorded",
      rows: [
        { key: "server", value: entry.targetLabel },
        { key: "endpoint", value: endpointUrl },
      ],
      note: canViewToolLogs
        ? "The first new call is recorded in Tool Logs."
        : "The first new call is counted in the server's activity.",
    };
    updateActiveOperation(undefined);
    void invalidateAllGetMcpServerActivity(queryClient);
    operation.report({ type: "event", scope: operation.scope, event });
  }, [
    activeOperation,
    activityError,
    activityQuery.data?.activity,
    activityQuery.isPending,
    canViewToolLogs,
    endpointUrl,
    mcpServer,
    suppressActivityError,
    updateActiveOperation,
    queryClient,
  ]);

  return {
    activityBaselineError: baselineCaptureError,
    activityBaselinePending: baselineCapturePending,
    catalogError,
    catalogPending: catalog.isPending,
    catalogServers,
    client,
    connectionPromptCopied,
    promptCopied,
    endpointUrl,
    handleSignal,
    markConnectionPromptCopied: () => setConnectionPromptCopied(true),
    markPromptCopied: () => setPromptCopied(true),
    mcpServer,
    projectStateError,
    projectStatePending,
    prompt,
    prepareActivityBaseline: captureActivityBaseline,
    retryCatalog: () => {
      void catalog.refetch();
    },
    selectServer: (server) => {
      setSelectedServerOverride(server);
      workflow.reset();
      if (orgSlug && projectSlug) {
        markProjectGuideMcpServerSelected(
          orgSlug,
          projectSlug,
          server.registrySpecifier,
        );
      }
      setConnectionPromptCopied(false);
      setPromptCopied(false);
      activityBaselineRef.current = undefined;
      activityBaselineGenerationRef.current += 1;
      setActivityBaseline(undefined);
      setBaselineCaptureError(false);
    },
    selectedServer,
    serverName: resolvedName,
    setClient: (nextClient) => {
      setClient(nextClient);
      setConnectionPromptCopied(false);
      setPromptCopied(false);
    },
    connectionPrompts,
    toolLogsHref: canViewToolLogs ? routes.logs.href() : undefined,
    mcpServerHref: mcpServer
      ? routes.mcp.details.overview.href(mcpServerRouteParam(mcpServer))
      : routes.home.href(),
  };
}

export { CLIENTS as MCP_GUIDE_CLIENTS };
