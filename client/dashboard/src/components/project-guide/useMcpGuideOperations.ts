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
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { mcpServerRouteParam } from "@/lib/sources";
import { getServerURL } from "@/lib/utils";
import { type PulseMCPServer, useListMCPCatalog } from "@/pages/catalog/hooks";
import {
  isPulseMcpServer,
  requiresManualSetup,
} from "@/pages/catalog/hooks/serverMetadata";
import {
  filterToHttpRemotes,
  isFigmaCatalogServer,
  normalizeRemoteUrl,
} from "@/pages/catalog/remotes";
import {
  type ServerInstallStatus,
  useRemoteMcpInstallWorkflow,
} from "@/pages/catalog/useRemoteMcpInstallWorkflow";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { McpServerActivity } from "@gram/client/models/components/mcpserveractivity.js";
import type { ExternalMCPServer } from "@gram/client/models/components/externalmcpserver.js";
import { useGetMcpServerActivity } from "@gram/client/react-query/getMcpServerActivity.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export type McpGuideClient = "claude" | "cursor" | "codex";

export type McpGuideSnippet = {
  code: string;
  language: "json" | "toml";
};

type ActiveOperation = {
  scope: ProjectGuideOperationScope;
  report: (report: ProjectGuideOperationReport) => void;
  paused: boolean;
};

type ActivityBaseline = {
  totalToolCalls: number;
};

const CLIENTS: McpGuideClient[] = ["claude", "cursor", "codex"];

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

function curateCatalogServers(
  servers: ExternalMCPServer[] | undefined,
): PulseMCPServer[] | undefined {
  if (!servers) return undefined;
  return servers
    .filter(isPulseMcpServer)
    .filter((server) => !requiresManualSetup(server))
    .filter((server) => server.isReadOnly === true)
    .filter((server) => !isFigmaCatalogServer(server))
    .map(filterToHttpRemotes)
    .filter((server) => (server.remotes?.length ?? 0) > 0)
    .sort(compareCatalogServers);
}

function snippetsFor(
  serverSlug: string,
  endpointUrl: string,
): Record<McpGuideClient, McpGuideSnippet> {
  const json = JSON.stringify(
    { mcpServers: { [serverSlug]: { url: endpointUrl } } },
    null,
    2,
  );
  return {
    claude: { code: json, language: "json" },
    cursor: { code: json, language: "json" },
    codex: {
      code: `[mcp_servers.${serverSlug}]\nurl = "${endpointUrl}"`,
      language: "toml",
    },
  };
}

function promptFor(name: string): string {
  return `Using the ${name} MCP server, first list the available tools. Then choose one tool marked read-only and call it with a harmless request. Do not create, update, or delete anything. Summarize the result, and do not call any tool unless it is marked read-only.`;
}

function activityFor(
  activity: McpServerActivity[] | undefined,
  server: McpServer | undefined,
): McpServerActivity | undefined {
  if (!server?.slug) return undefined;
  return activity?.find(
    (entry) =>
      entry.targetType === "hosted_mcp_server" &&
      entry.targetId === server.slug,
  );
}

function isNewActivity(
  baseline: ActivityBaseline,
  activity: McpServerActivity | undefined,
): boolean {
  return Boolean(activity && activity.totalToolCalls > baseline.totalToolCalls);
}

function operationKey(scope: ProjectGuideOperationScope): string {
  return `${scope.path}:${scope.step}:${scope.attempt}:${scope.runId}`;
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
  activityBaselineReady: boolean;
  activityError: boolean;
  catalogError: boolean;
  catalogPending: boolean;
  catalogServers: PulseMCPServer[] | undefined;
  client: McpGuideClient;
  configCopied: boolean;
  deploymentReady: boolean;
  endpointUrl: string | undefined;
  handleSignal: (
    signal: ProjectGuideOperationSignal,
    report: (report: ProjectGuideOperationReport) => void,
  ) => void;
  installStatuses: ServerInstallStatus[];
  markConfigCopied: () => void;
  markPromptCopied: () => void;
  mcpServer: McpServer | undefined;
  mcpServerHref: string | undefined;
  projectStateError: boolean;
  projectStatePending: boolean;
  prompt: string | undefined;
  promptCopied: boolean;
  retryActivity: () => void;
  retryCatalog: () => void;
  selectServer: (server: PulseMCPServer) => void;
  selectedServer: PulseMCPServer | undefined;
  serverName: string | undefined;
  setClient: (client: McpGuideClient) => void;
  snippets: Record<McpGuideClient, McpGuideSnippet> | undefined;
  toolLogsHref: string;
} {
  const gramProject = useProjectSlugForRequests();
  const routes = useRoutes();
  const [selectedServer, setSelectedServer] = useState<
    PulseMCPServer | undefined
  >(undefined);
  const [client, setClient] = useState<McpGuideClient>("claude");
  const [configCopied, setConfigCopied] = useState(false);
  const [promptCopied, setPromptCopied] = useState(false);
  const [activeOperation, setActiveOperation] = useState<
    ActiveOperation | undefined
  >(undefined);
  const activeOperationRef = useRef<ActiveOperation | undefined>(undefined);
  const [activityBaseline, setActivityBaseline] = useState<
    ActivityBaseline | undefined
  >(undefined);
  const activityBaselineRef = useRef<ActivityBaseline | undefined>(undefined);
  const installStartedFor = useRef<string | undefined>(undefined);
  const progressReportedFor = useRef(new Set<string>());
  const [suppressActivityError, setSuppressActivityError] = useState(false);
  const [baselineCaptureError, setBaselineCaptureError] = useState(false);

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
  const deploymentReady = Boolean(
    mcpServer &&
    endpoint &&
    hasDefaultPluginServer(pluginsQuery.data?.plugins, mcpServer.id),
  );
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
  });

  const activityQuery = useGetMcpServerActivity(
    { gramProject, getMcpServerActivityPayload: {} },
    undefined,
    {
      enabled: Boolean(endpointUrl),
      refetchInterval:
        activeOperation?.scope.step === 4 && !activeOperation.paused
          ? 5_000
          : false,
      throwOnError: false,
    },
  );
  const queryActivityError =
    activityQuery.isError ||
    (Boolean(endpointUrl) &&
      !activityQuery.isPending &&
      activityQuery.data === undefined);
  const activityError = baselineCaptureError || queryActivityError;
  const serverActivity = activityFor(activityQuery.data?.activity, mcpServer);

  const snippets =
    endpointUrl && mcpServer?.slug
      ? snippetsFor(mcpServer.slug, endpointUrl)
      : undefined;
  const prompt = resolvedName ? promptFor(resolvedName) : undefined;
  const installStatuses =
    workflow.phase === "installing" || workflow.phase === "complete"
      ? workflow.statuses
      : [];

  const updateActiveOperation = useCallback(
    (operation: ActiveOperation | undefined) => {
      activeOperationRef.current = operation;
      setActiveOperation(operation);
    },
    [],
  );

  const captureActivityBaseline = useCallback(async (): Promise<void> => {
    activityBaselineRef.current = undefined;
    setActivityBaseline(undefined);
    setBaselineCaptureError(false);
    setSuppressActivityError(true);
    try {
      const result = await activityQuery.refetch();
      if (result.isError || !result.data) {
        setBaselineCaptureError(true);
        return;
      }
      const current = activityFor(result.data.activity, mcpServer);
      const baseline = { totalToolCalls: current?.totalToolCalls ?? 0 };
      activityBaselineRef.current = baseline;
      setActivityBaseline(baseline);
    } catch {
      setBaselineCaptureError(true);
    } finally {
      setSuppressActivityError(false);
    }
  }, [activityQuery, mcpServer]);

  const retryActivity = useCallback(() => {
    if (!activityBaselineRef.current) {
      void captureActivityBaseline();
      return;
    }
    setSuppressActivityError(true);
    void activityQuery.refetch().finally(() => setSuppressActivityError(false));
  }, [activityQuery, captureActivityBaseline]);

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
      if (signal.type === "pause") {
        const current = activeOperationRef.current;
        if (
          current &&
          operationKey(current.scope) === operationKey(signal.scope)
        ) {
          updateActiveOperation({ ...current, paused: true });
        }
        return;
      }
      if (signal.type === "checkpoint") {
        if (signal.scope.step === 2) void captureActivityBaseline();
        return;
      }

      updateActiveOperation({ scope: signal.scope, report, paused: false });
      if (signal.type === "start" && signal.scope.step === 4) {
        report({
          type: "progress",
          scope: signal.scope,
          message: "Listening for a new call on the selected governed endpoint",
        });
      }
      if (signal.type === "retry") {
        if (signal.scope.step === 0) workflow.reset();
        if (signal.scope.step === 4) retryActivity();
      }
    },
    [captureActivityBaseline, retryActivity, updateActiveOperation, workflow],
  );

  useEffect(() => {
    const operation = activeOperation;
    if (!operation || operation.paused || operation.scope.step !== 0) return;
    const key = operationKey(operation.scope);
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
      updateActiveOperation(undefined);
      operation.report({
        type: "success",
        scope: operation.scope,
        result: `${resolvedName ?? serverName(selectedServer)} is already installed as a governed MCP server`,
      });
      return;
    }
    if (!progressReportedFor.current.has(key)) {
      progressReportedFor.current.add(key);
      operation.report({
        type: "progress",
        scope: operation.scope,
        message: `Installing ${serverName(selectedServer)} into this project`,
        progress: 0.2,
      });
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
    updateActiveOperation(undefined);
    if (failure) {
      operation.report({
        type: "error",
        scope: operation.scope,
        message:
          failure.error ??
          "The server installation failed. Retry to run the existing install workflow again.",
      });
      return;
    }
    operation.report({
      type: "success",
      scope: operation.scope,
      result: `${serverName(selectedServer)} installed as a governed MCP server`,
    });
  }, [
    activeOperation,
    catalogError,
    mcpServer,
    projectDataDefined,
    projectStateError,
    projectStatePending,
    resolvedName,
    selectedServer,
    updateActiveOperation,
    workflow,
  ]);

  useEffect(() => {
    const operation = activeOperation;
    if (!operation || operation.paused || operation.scope.step !== 1) return;
    if (projectStatePending) return;
    updateActiveOperation(undefined);
    if (projectStateError) {
      operation.report({
        type: "error",
        scope: operation.scope,
        message:
          "Could not read this project's MCP server, Default plugin, and endpoint. Retry the readiness check.",
      });
      return;
    }
    if (!deploymentReady || !resolvedName) {
      operation.report({
        type: "error",
        scope: operation.scope,
        message:
          "The server is not ready in the Default plugin with a governed endpoint. Retry after the install finishes.",
      });
      return;
    }
    operation.report({
      type: "success",
      scope: operation.scope,
      result: `${resolvedName} is ready on its governed endpoint`,
    });
  }, [
    activeOperation,
    deploymentReady,
    projectStateError,
    projectStatePending,
    resolvedName,
    updateActiveOperation,
  ]);

  useEffect(() => {
    const operation = activeOperation;
    if (!operation || operation.paused || operation.scope.step !== 4) return;
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
    if (!isNewActivity(baseline, serverActivity)) return;

    const event: ProjectGuideEventCard = {
      kind: "Governed call",
      tone: "allow",
      title: serverActivity?.targetLabel ?? resolvedName ?? "MCP server",
      rows: [
        { key: "server", value: resolvedName ?? "Catalog server" },
        {
          key: "calls",
          value: `${serverActivity?.totalToolCalls ?? 0} recorded`,
        },
      ],
      note: "The new call is recorded in Tool Logs.",
    };
    updateActiveOperation(undefined);
    operation.report({ type: "event", scope: operation.scope, event });
  }, [
    activeOperation,
    activityError,
    activityQuery.isPending,
    resolvedName,
    serverActivity,
    suppressActivityError,
    updateActiveOperation,
  ]);

  return {
    activityBaselineReady: activityBaseline !== undefined,
    activityError,
    catalogError,
    catalogPending: catalog.isPending,
    catalogServers,
    client,
    configCopied,
    deploymentReady,
    endpointUrl,
    handleSignal,
    installStatuses,
    markConfigCopied: () => setConfigCopied(true),
    markPromptCopied: () => setPromptCopied(true),
    mcpServer,
    mcpServerHref: mcpServer
      ? routes.mcp.x.overview.href(mcpServerRouteParam(mcpServer))
      : undefined,
    projectStateError,
    projectStatePending,
    prompt,
    promptCopied,
    retryActivity,
    retryCatalog: () => {
      void catalog.refetch();
    },
    selectServer: (server) => {
      setSelectedServer(server);
      setConfigCopied(false);
      setPromptCopied(false);
      activityBaselineRef.current = undefined;
      setActivityBaseline(undefined);
      setBaselineCaptureError(false);
    },
    selectedServer,
    serverName: resolvedName,
    setClient: (nextClient) => {
      setClient(nextClient);
      setConfigCopied(false);
    },
    snippets,
    toolLogsHref: routes.logs.href(),
  };
}

export { CLIENTS as MCP_GUIDE_CLIENTS };
