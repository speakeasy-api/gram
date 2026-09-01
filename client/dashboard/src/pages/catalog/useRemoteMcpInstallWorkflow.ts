import { useFetcher } from "@/contexts/Fetcher";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import {
  createDefaultMcpEndpoint,
  DEFAULT_ENDPOINT_FAILED_MESSAGE,
} from "@/lib/mcpEndpoints";
import { mcpServerRouteParam } from "@/lib/sources";
import { getServerURL } from "@/lib/utils";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import {
  collectibleHeaders,
  getRemoteDisplayInfo,
  isFigmaCatalogServer,
  normalizeRemoteUrl,
} from "@/pages/catalog/remotes";
import { autoConfigureRemoteMcpAuth } from "@/pages/sources/remote-mcp/autoConfigureAuth";
import type { Gram } from "@gram/client";
import type { RequestOptions } from "@gram/client/lib/sdks.js";
import type { ExternalMCPRemote } from "@gram/client/models/components/externalmcpremote.js";
import type { ExternalMCPRemoteHeader } from "@gram/client/models/components/externalmcpremoteheader.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { invalidateAllGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllRemoteMcpServerHeaders } from "@gram/client/react-query/remoteMcpServerHeaders.js";
import {
  invalidateAllRemoteMcpServers,
  useRemoteMcpServers,
} from "@gram/client/react-query/remoteMcpServers.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import {
  invalidateAllUnproxiedMcpServers,
  useUnproxiedMcpServers,
} from "@gram/client/react-query/unproxiedMcpServers.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

type InstallPhaseName =
  | "selectRemotes"
  | "configure"
  | "installing"
  | "complete";

export interface ServerConfig {
  server: PulseMCPServer;
  name: string;
  /** Remote endpoints to install — one remote MCP server is created per URL. */
  remotes: ExternalMCPRemote[];
  /** User-entered header values, keyed by [headerValueKey]. */
  headerValues: Record<string, string>;
}

/** Configuration for a server with multiple remotes during the selectRemotes phase */
interface MultiRemoteServerConfig {
  server: PulseMCPServer;
  name: string;
  remotes: ExternalMCPRemote[];
  selectedRemoteUrls: Set<string>;
}

export interface ServerInstallStatus {
  key: string;
  name: string;
  status: "pending" | "creating" | "completed" | "failed";
  /** mcp_servers row id — used by callers that bundle installs (e.g. plugins). */
  mcpServerId?: string;
  /** Route param (slug or id) for the MCP server details page. */
  mcpServerParam?: string;
  /** Public URL of the pre-staged default MCP endpoint, when one was created. */
  mcpEndpointUrl?: string;
  error?: string;
}

interface WorkflowBase {
  projectSlug?: string;
  /**
   * Whether a remote MCP server already exists in the project for one of this
   * catalog server's endpoint URLs. Informational only — installing again just
   * creates another server.
   */
  isServerAlreadyInstalled: (server: PulseMCPServer) => boolean;
  reset: () => void;
}

export interface SelectRemotesPhase extends WorkflowBase {
  phase: "selectRemotes";
  /** Servers with multiple remotes that need configuration */
  multiRemoteConfigs: MultiRemoteServerConfig[];
  /** Index of the server currently being configured */
  currentServerIndex: number;
  /** Update the current server's name or selected remotes */
  updateCurrentConfig: (updates: {
    name?: string;
    selectedRemoteUrls?: Set<string>;
  }) => void;
  /** Move to the next multi-remote server, or to configure phase if done */
  nextServer: () => void;
  /** Whether the current server can proceed (has at least one remote selected) */
  canProceed: boolean;
}

export interface ConfigurePhase extends WorkflowBase {
  phase: "configure";
  serverConfigs: ServerConfig[];
  updateServerConfig: (
    index: number,
    updates: Partial<Pick<ServerConfig, "name">>,
  ) => void;
  setHeaderValue: (
    index: number,
    remoteUrl: string,
    headerName: string,
    value: string,
  ) => void;
  canInstall: boolean;
  startInstall: () => Promise<void>;
  /** Go back to selectRemotes phase (only available if there were multi-remote servers) */
  goBack?: () => void;
}

interface InstallingPhase extends WorkflowBase {
  phase: "installing";
  statuses: ServerInstallStatus[];
}

export interface CompletePhase extends WorkflowBase {
  phase: "complete";
  statuses: ServerInstallStatus[];
}

export type RemoteMcpInstallWorkflow =
  | SelectRemotesPhase
  | ConfigurePhase
  | InstallingPhase
  | CompletePhase;

interface UseRemoteMcpInstallWorkflowOptions {
  servers: PulseMCPServer[];
  projectSlug?: string;
  serverNameSuffix?: string;
  /**
   * Install every endpoint of multi-remote servers instead of pausing on the
   * interactive selectRemotes phase. Required by headless/auto-start callers
   * (onboarding), which have no UI to select from.
   */
  autoSelectRemotes?: boolean;
}

/** Key into [ServerConfig.headerValues] for one header of one remote. */
export function headerValueKey(remoteUrl: string, headerName: string): string {
  return `${remoteUrl} ${headerName.toLowerCase()}`;
}

function buildServerConfig(
  server: PulseMCPServer,
  serverNameSuffix = "",
): ServerConfig {
  return {
    server,
    name: `${server.title ?? server.registrySpecifier}${serverNameSuffix}`,
    remotes: server.remotes ?? [],
    headerValues: {},
  };
}

/**
 * One remote MCP server creation, resolved from a config at install start.
 * Multi-endpoint installs get per-endpoint names so the resulting servers are
 * tellable apart.
 */
interface InstallTarget {
  server: PulseMCPServer;
  remote: ExternalMCPRemote;
  name: string;
  headers: Array<{ header: ExternalMCPRemoteHeader; value: string }>;
}

function buildInstallTargets(config: ServerConfig): InstallTarget[] {
  return config.remotes.map((remote) => ({
    server: config.server,
    remote,
    name:
      config.remotes.length > 1
        ? `${config.name.replace(/_Governed$/, "")} ${getRemoteDisplayInfo(remote.url).name}_Governed`
        : config.name,
    headers: collectibleHeaders(remote).flatMap((header) => {
      const value =
        config.headerValues[headerValueKey(remote.url, header.name)]?.trim();
      return value ? [{ header, value }] : [];
    }),
  }));
}

/**
 * Best-effort: a logo failure never fails the install, and the returned
 * promise never rejects. Resolves true only when the logo actually landed on
 * mcp_metadata, so callers know whether a metadata refetch is warranted.
 */
async function persistServerIconBestEffort(
  client: Gram,
  target: InstallTarget,
  mcpServerId: string,
  reqOpts: RequestOptions | undefined,
): Promise<boolean> {
  const iconUrl = target.server.iconUrl;
  if (!iconUrl) return false;

  try {
    const uploaded = await client.assets.fetchImageFromURL(
      { fetchImageFromURLForm2: { url: iconUrl } },
      undefined,
      reqOpts,
    );
    await client.mcpMetadata.set(
      {
        setMcpMetadataRequestBody: {
          mcpServerId,
          logoAssetId: uploaded.asset.id,
        },
      },
      undefined,
      reqOpts,
    );
    return true;
  } catch (iconError) {
    console.warn("Failed to persist server icon during install.", {
      mcpServerId,
      iconUrl,
      iconError,
    });
    return false;
  }
}

/**
 * Installs one target as an unproxied MCP server instead of a Gram-proxied
 * remote one: creates the unproxied_mcp_servers row, links an mcp_servers
 * wrapper (rolling back the former on failure, mirroring installTarget's own
 * remote-server path), and returns the same shape installTarget does. There
 * is no OAuth to auto-configure and no Gram endpoint to pre-stage — the
 * customer connects straight to the vendor.
 */
async function installUnproxiedTarget(
  client: Gram,
  target: InstallTarget,
  reqOpts: RequestOptions | undefined,
): Promise<{
  mcpServer: McpServer;
  mcpEndpointUrl?: string;
  authConfigured: boolean;
  iconPersistence: Promise<boolean>;
}> {
  const unproxiedMcpServer = await client.unproxiedMcp.createServer(
    {
      createUnproxiedMcpServerForm: {
        name: target.name,
        url: target.remote.url,
        description: target.server.description || undefined,
      },
    },
    undefined,
    reqOpts,
  );

  let mcpServer: McpServer;
  try {
    mcpServer = await client.mcpServers.create(
      {
        createMcpServerForm: {
          name: target.name,
          unproxiedMcpServerId: unproxiedMcpServer.id,
          // Unproxied servers have no Gram-hosted endpoint, so
          // disabled/private/public gates nothing Gram actually serves.
          visibility: "public",
        },
      },
      undefined,
      reqOpts,
    );
  } catch (linkError) {
    try {
      await client.unproxiedMcp.deleteServer(
        { id: unproxiedMcpServer.id },
        undefined,
        reqOpts,
      );
    } catch (rollbackError) {
      const linkMsg =
        linkError instanceof Error ? linkError.message : String(linkError);
      const rollbackMsg =
        rollbackError instanceof Error
          ? rollbackError.message
          : String(rollbackError);
      throw new Error(
        `Created unproxied MCP server ${unproxiedMcpServer.id} but failed to link an MCP server, and the rollback also failed. Delete it manually before retrying. Cause: ${linkMsg}. Rollback: ${rollbackMsg}.`,
      );
    }
    throw linkError instanceof Error ? linkError : new Error(String(linkError));
  }

  // Runs detached: a slow icon host must not hold up install completion. The
  // caller chains a metadata refetch onto the returned promise instead.
  const iconPersistence = persistServerIconBestEffort(
    client,
    target,
    mcpServer.id,
    reqOpts,
  );

  return {
    mcpServer,
    mcpEndpointUrl: undefined,
    authConfigured: false,
    iconPersistence,
  };
}

/**
 * Installs catalog servers as Remote MCP servers. For every selected remote
 * endpoint: create a remote_mcp_servers row, link an mcp_servers row (which
 * mints the server's user session issuer), persist any user-provided upstream
 * headers, auto-configure OAuth when the upstream advertises it, and pre-stage
 * a default MCP endpoint. Everything is a synchronous management API call —
 * there is no deployment to poll.
 */
export function useRemoteMcpInstallWorkflow({
  servers,
  projectSlug,
  autoSelectRemotes = false,
  serverNameSuffix = "",
}: UseRemoteMcpInstallWorkflowOptions): RemoteMcpInstallWorkflow {
  const client = useSdkClient();
  const { fetch: authedFetch } = useFetcher();
  const queryClient = useQueryClient();
  const { orgSlug } = useSlugs();

  // Informational "already installed" signal: a remote MCP server with a
  // matching URL already exists in the target project. Unproxied servers
  // (e.g. Figma, installed via installUnproxiedTarget) live in a separate
  // resource entirely, so their URLs are checked alongside the remote ones.
  const { data: remoteServersData } = useRemoteMcpServers(
    projectSlug ? { gramProject: projectSlug } : undefined,
  );
  const { data: unproxiedServersData } = useUnproxiedMcpServers(
    projectSlug ? { gramProject: projectSlug } : undefined,
  );
  const installedUrls = useMemo(
    () =>
      new Set(
        [
          ...(remoteServersData?.remoteMcpServers ?? []),
          ...(unproxiedServersData?.unproxiedMcpServers ?? []),
        ].map((server) => normalizeRemoteUrl(server.url)),
      ),
    [
      remoteServersData?.remoteMcpServers,
      unproxiedServersData?.unproxiedMcpServers,
    ],
  );
  const isServerAlreadyInstalled = useCallback(
    (server: PulseMCPServer) =>
      (server.remotes ?? []).some((remote) =>
        installedUrls.has(normalizeRemoteUrl(remote.url)),
      ),
    [installedUrls],
  );

  const [phase, setPhase] = useState<InstallPhaseName>("configure");
  const [serverConfigs, setServerConfigs] = useState<ServerConfig[]>([]);
  const [statuses, setStatuses] = useState<ServerInstallStatus[]>([]);

  // State for multi-remote server selection
  const [multiRemoteConfigs, setMultiRemoteConfigs] = useState<
    MultiRemoteServerConfig[]
  >([]);
  const [currentServerIndex, setCurrentServerIndex] = useState(0);

  // Track last processed server index to prevent double-click duplicates
  const lastProcessedIndexRef = useRef(-1);

  // Keep a live handle on the current phase so the partition effect can bail
  // out once installation has started — a props refresh mid-install must not
  // snap the dialog back to a fresh configure step.
  const phaseRef = useRef<InstallPhaseName>("configure");
  useEffect(() => {
    phaseRef.current = phase;
  }, [phase]);

  const partitionServers = useCallback(() => {
    const multiRemote: MultiRemoteServerConfig[] = [];
    const singleRemote: ServerConfig[] = [];

    for (const server of servers) {
      const remotes = server.remotes ?? [];
      if (remotes.length > 1 && !autoSelectRemotes) {
        multiRemote.push({
          server,
          name: `${server.title ?? server.registrySpecifier}${serverNameSuffix}`,
          remotes,
          selectedRemoteUrls: new Set(),
        });
      } else {
        singleRemote.push(buildServerConfig(server, serverNameSuffix));
      }
    }

    setMultiRemoteConfigs(multiRemote);
    setServerConfigs(singleRemote);
    setCurrentServerIndex(0);
    lastProcessedIndexRef.current = -1;
    setPhase(multiRemote.length > 0 ? "selectRemotes" : "configure");
  }, [autoSelectRemotes, serverNameSuffix, servers]);

  // Initialize server configs when servers change - partition into multi/single remote.
  useEffect(() => {
    if (
      phaseRef.current !== "selectRemotes" &&
      phaseRef.current !== "configure"
    ) {
      return;
    }
    partitionServers();
  }, [partitionServers]);

  const updateServerConfig = useCallback(
    (index: number, updates: Partial<Pick<ServerConfig, "name">>) => {
      setServerConfigs((prev) =>
        prev.map((config, i) => {
          if (i !== index) return config;
          return { ...config, ...updates };
        }),
      );
    },
    [],
  );

  const setHeaderValue = useCallback(
    (index: number, remoteUrl: string, headerName: string, value: string) => {
      setServerConfigs((prev) =>
        prev.map((config, i) => {
          if (i !== index) return config;
          return {
            ...config,
            headerValues: {
              ...config.headerValues,
              [headerValueKey(remoteUrl, headerName)]: value,
            },
          };
        }),
      );
    },
    [],
  );

  // Callbacks for multi-remote selection phase
  const updateCurrentConfig = useCallback(
    (updates: { name?: string; selectedRemoteUrls?: Set<string> }) => {
      setMultiRemoteConfigs((prev) =>
        prev.map((config, i) => {
          if (i !== currentServerIndex) return config;
          return {
            ...config,
            ...(updates.name !== undefined && {
              name: updates.name,
            }),
            ...(updates.selectedRemoteUrls !== undefined && {
              selectedRemoteUrls: updates.selectedRemoteUrls,
            }),
          };
        }),
      );
    },
    [currentServerIndex],
  );

  const canProceed = useMemo(() => {
    const currentConfig = multiRemoteConfigs[currentServerIndex];
    if (!currentConfig) return false;
    return (
      currentConfig.name.trim() !== "" &&
      currentConfig.selectedRemoteUrls.size > 0
    );
  }, [multiRemoteConfigs, currentServerIndex]);

  const nextServer = useCallback(() => {
    if (!canProceed) return;

    // Prevent double-click duplicates by checking if this index was already processed
    if (lastProcessedIndexRef.current === currentServerIndex) return;
    lastProcessedIndexRef.current = currentServerIndex;

    const currentConfig = multiRemoteConfigs[currentServerIndex];
    if (currentConfig) {
      setServerConfigs((prev) => [
        ...prev,
        {
          server: currentConfig.server,
          name: currentConfig.name,
          remotes: currentConfig.remotes.filter((r) =>
            currentConfig.selectedRemoteUrls.has(r.url),
          ),
          headerValues: {},
        },
      ]);
    }

    if (currentServerIndex < multiRemoteConfigs.length - 1) {
      setCurrentServerIndex((prev) => prev + 1);
    } else {
      // Done with all multi-remote servers, move to configure phase
      setPhase("configure");
    }
  }, [canProceed, currentServerIndex, multiRemoteConfigs]);

  // A config with no HTTP remote cannot be installed as a remote MCP server —
  // startInstall reports it as failed. At least one config must be
  // installable for the install to be worth starting at all.
  const canInstall = useMemo(() => {
    return (
      serverConfigs.length > 0 &&
      serverConfigs.every((c) => c.name.trim() !== "") &&
      serverConfigs.some((c) => c.remotes.length > 0)
    );
  }, [serverConfigs]);

  // goBack returns to selectRemotes phase - only available if there were multi-remote servers
  const goBack = useCallback(() => {
    // Remove multi-remote servers from serverConfigs; they re-enter via
    // nextServer. Single-remote configs are the ones whose server has 0 or 1
    // remotes overall.
    setServerConfigs((prev) =>
      prev.filter((config) => (config.server.remotes ?? []).length <= 1),
    );
    setCurrentServerIndex(0);
    lastProcessedIndexRef.current = -1; // Reset to allow re-processing
    setPhase("selectRemotes");
  }, []);

  const hasMultiRemoteServers = multiRemoteConfigs.length > 0;

  const installTarget = useCallback(
    async (
      target: InstallTarget,
      reqOpts: RequestOptions | undefined,
    ): Promise<{
      mcpServer: McpServer;
      mcpEndpointUrl?: string;
      authConfigured: boolean;
      iconPersistence: Promise<boolean>;
    }> => {
      if (isFigmaCatalogServer(target.server)) {
        return installUnproxiedTarget(client, target, reqOpts);
      }

      const remoteMcpServer = await client.remoteMcp.createServer(
        {
          createServerForm: {
            name: target.name,
            url: target.remote.url,
            transportType: "streamable-http",
          },
        },
        undefined,
        reqOpts,
      );

      let mcpServer: McpServer;
      try {
        mcpServer = await client.mcpServers.create(
          {
            createMcpServerForm: {
              name: target.name,
              remoteMcpServerId: remoteMcpServer.id,
              // Private (user-session gated) rather than the sources flow's
              // "disabled": catalog installs promise a usable server, and the
              // pre-staged endpoint must actually serve. Public would expose
              // any stored upstream API-key headers to anyone with the URL.
              visibility: "private",
            },
          },
          undefined,
          reqOpts,
        );
      } catch (linkError) {
        try {
          await client.remoteMcp.deleteServer(
            { id: remoteMcpServer.id },
            undefined,
            reqOpts,
          );
        } catch (rollbackError) {
          const linkMsg =
            linkError instanceof Error ? linkError.message : String(linkError);
          const rollbackMsg =
            rollbackError instanceof Error
              ? rollbackError.message
              : String(rollbackError);
          throw new Error(
            `Created remote MCP server ${remoteMcpServer.id} but failed to link an MCP server, and the rollback also failed. Delete it manually before retrying. Cause: ${linkMsg}. Rollback: ${rollbackMsg}.`,
          );
        }
        throw linkError instanceof Error
          ? linkError
          : new Error(String(linkError));
      }

      // Persist user-provided upstream headers. Best-effort per header: the
      // server is already linked and headers can always be (re)configured from
      // its Settings tab, so a failure warns instead of failing the install.
      for (const { header, value } of target.headers) {
        try {
          await client.remoteMcp.createServerHeader(
            {
              createServerHeaderForm: {
                remoteMcpServerId: remoteMcpServer.id,
                name: header.name,
                description: header.description,
                isSecret: header.isSecret ?? false,
                isRequired: header.isRequired ?? false,
                value,
              },
            },
            undefined,
            reqOpts,
          );
        } catch (headerError) {
          console.warn("Failed to save upstream header during install.", {
            remoteMcpServerId: remoteMcpServer.id,
            header: header.name,
            headerError,
          });
          toast.warning(
            `Couldn't save the "${header.name}" header for ${target.name}. Add it from the server's Settings tab.`,
          );
        }
      }

      // Runs detached: a slow icon host must not hold up install completion.
      // startInstall chains a metadata refetch onto the returned promise.
      const iconPersistence = persistServerIconBestEffort(
        client,
        target,
        mcpServer.id,
        reqOpts,
      );

      const authAutoConfig = await autoConfigureRemoteMcpAuth({
        client,
        authedFetch,
        remoteMcpServer,
        mcpServer,
        options: reqOpts,
      });
      const configuredMcpServer =
        authAutoConfig.status === "configured"
          ? authAutoConfig.mcpServer
          : mcpServer;

      // Pre-stage a default endpoint so the user doesn't have to create one
      // before the server can serve. Best-effort: never rolls back the source.
      if (!orgSlug) {
        toast.warning(DEFAULT_ENDPOINT_FAILED_MESSAGE);
      }
      const endpoint = orgSlug
        ? await createDefaultMcpEndpoint(
            client,
            configuredMcpServer,
            orgSlug,
            reqOpts,
          )
        : undefined;

      return {
        mcpServer: configuredMcpServer,
        mcpEndpointUrl: endpoint
          ? `${getServerURL()}/mcp/${endpoint.slug}`
          : undefined,
        authConfigured: authAutoConfig.status === "configured",
        iconPersistence,
      };
    },
    [authedFetch, client, orgSlug],
  );

  const startInstall = useCallback(async () => {
    if (!canInstall || phaseRef.current !== "configure") return;

    // Configs without a compatible endpoint can't be installed; report them as
    // failed instead of blocking the rest of the batch (or, for headless
    // callers, stalling forever with nothing to install).
    const targets = serverConfigs
      .filter((config) => config.remotes.length > 0)
      .flatMap(buildInstallTargets);
    const uninstallable = serverConfigs.filter(
      (config) => config.remotes.length === 0,
    );

    setPhase("installing");
    setStatuses([
      ...targets.map((target, index) => ({
        key: `${index}-${target.remote.url}`,
        name: target.name,
        status: "pending" as const,
      })),
      ...uninstallable.map((config, index) => ({
        key: `uninstallable-${index}`,
        name: config.name,
        status: "failed" as const,
        error:
          "This server does not expose a compatible remote endpoint and cannot be added.",
      })),
    ]);

    const reqOpts = projectSlug
      ? { headers: { "gram-project": projectSlug } }
      : undefined;

    const setStatusAt = (
      index: number,
      updates: Partial<ServerInstallStatus>,
    ) => {
      setStatuses((prev) =>
        prev.map((status, i) =>
          i === index ? { ...status, ...updates } : status,
        ),
      );
    };

    let anyAuthConfigured = false;
    let anyUnproxiedInstalled = false;
    const iconPersistences: Promise<boolean>[] = [];
    for (const [index, target] of targets.entries()) {
      setStatusAt(index, { status: "creating" });
      try {
        const result = await installTarget(target, reqOpts);
        iconPersistences.push(result.iconPersistence);
        anyAuthConfigured ||= result.authConfigured;
        anyUnproxiedInstalled ||= isFigmaCatalogServer(target.server);
        setStatusAt(index, {
          status: "completed",
          mcpServerId: result.mcpServer.id,
          mcpServerParam: mcpServerRouteParam(result.mcpServer),
          mcpEndpointUrl: result.mcpEndpointUrl,
        });
      } catch (err) {
        setStatusAt(index, {
          status: "failed",
          error: err instanceof Error ? err.message : String(err),
        });
      }
    }

    // refetchType "all" forces the refetch even when there are no active
    // observers, so list pages pick up the new servers on next mount.
    const invalidations = [
      invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
      invalidateAllRemoteMcpServerHeaders(queryClient, { refetchType: "all" }),
      invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
      // Installs may have persisted a catalog icon into MCP metadata.
      invalidateAllGetMcpMetadata(queryClient, { refetchType: "all" }),
      // Every create links a fresh user_session_issuer.
      invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
    ];
    // The issuer/client caches only change when auto-configuration actually
    // ran to completion on at least one server.
    if (anyAuthConfigured) {
      invalidations.push(
        invalidateAllRemoteSessionIssuers(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionClients(queryClient, { refetchType: "all" }),
      );
    }
    // Unproxied installs (e.g. Figma) don't touch any of the remote-server
    // caches above, so the Sources page's unproxied listing needs its own
    // invalidation.
    if (anyUnproxiedInstalled) {
      invalidations.push(
        invalidateAllUnproxiedMcpServers(queryClient, { refetchType: "all" }),
      );
    }
    await Promise.all(invalidations);

    // Icon persistence runs detached from the install loop, so the metadata
    // invalidation above usually fires before the logos land. Refetch again
    // once they settle — without gating the "complete" transition on it. The
    // persistence promises never reject, so Promise.all is safe here.
    void Promise.all(iconPersistences).then((persisted) => {
      if (persisted.some(Boolean)) {
        return invalidateAllGetMcpMetadata(queryClient, {
          refetchType: "all",
        });
      }
      return undefined;
    });

    setPhase("complete");
  }, [canInstall, installTarget, projectSlug, queryClient, serverConfigs]);

  const reset = useCallback(() => {
    setStatuses([]);
    partitionServers();
  }, [partitionServers]);

  const base: WorkflowBase = {
    projectSlug,
    isServerAlreadyInstalled,
    reset,
  };

  switch (phase) {
    case "selectRemotes":
      return {
        phase,
        multiRemoteConfigs,
        currentServerIndex,
        updateCurrentConfig,
        nextServer,
        canProceed,
        ...base,
      };
    case "configure":
      return {
        phase,
        serverConfigs,
        updateServerConfig,
        setHeaderValue,
        canInstall,
        startInstall,
        goBack: hasMultiRemoteServers ? goBack : undefined,
        ...base,
      };
    case "installing":
      return { phase, statuses, ...base };
    case "complete":
      return { phase, statuses, ...base };
  }
}
