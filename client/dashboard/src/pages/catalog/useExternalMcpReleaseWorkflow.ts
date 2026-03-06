import { useSdkClient } from "@/contexts/Sdk";
import type { Server } from "@/pages/catalog/hooks";
import {
  useDeployment,
  useDeploymentLogs,
  useLatestDeployment,
} from "@gram/client/react-query";
import type {
  DeploymentLogEvent,
  ExternalMCPRemote,
} from "@gram/client/models/components";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export function generateSlug(name: string): string {
  const lastPart = name.split("/").pop() || name;
  return lastPart
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

export type ReleasePhase =
  | "selectRemotes"
  | "configure"
  | "deploying"
  | "complete"
  | "error";

export interface ServerConfig {
  server: Server;
  name: string;
  /** For multi-remote servers, track which remotes are selected */
  selectedRemotes?: ExternalMCPRemote[];
}

/** Configuration for a server with multiple remotes during the selectRemotes phase */
export interface MultiRemoteServerConfig {
  server: Server;
  name: string;
  remotes: ExternalMCPRemote[];
  selectedRemoteUrls: Set<string>;
}

export interface ServerToolsetStatus {
  name: string;
  slug: string;
  status: "pending" | "creating" | "completed" | "failed";
  toolsetSlug?: string;
  mcpSlug?: string;
  error?: string;
}

interface WorkflowBase {
  projectSlug?: string;
  existingSpecifiers: Set<string>;
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
  canDeploy: boolean;
  startDeployment: () => Promise<void>;
  /** Go back to selectRemotes phase (only available if there were multi-remote servers) */
  goBack?: () => void;
}

export interface DeployingPhase extends WorkflowBase {
  phase: "deploying";
  deploymentId: string;
  deploymentStatus?: string;
  deploymentLogs: DeploymentLogEvent[];
}

export interface CompletePhase extends WorkflowBase {
  phase: "complete";
  toolsetStatuses: ServerToolsetStatus[];
}

export interface ErrorPhase extends WorkflowBase {
  phase: "error";
  error: string;
  deploymentId?: string;
  deploymentLogs: DeploymentLogEvent[];
}

export type ExternalMcpReleaseWorkflow =
  | SelectRemotesPhase
  | ConfigurePhase
  | DeployingPhase
  | CompletePhase
  | ErrorPhase;

interface UseExternalMcpReleaseWorkflowOptions {
  servers: Server[];
  projectSlug?: string;
}

export function useExternalMcpReleaseWorkflow({
  servers,
  projectSlug,
}: UseExternalMcpReleaseWorkflowOptions): ExternalMcpReleaseWorkflow {
  const client = useSdkClient();
  const { data: latestDeploymentResult } = useLatestDeployment(
    projectSlug ? { gramProject: projectSlug } : undefined,
  );
  const latestDeployment = latestDeploymentResult?.deployment;

  const existingSpecifiers = useMemo(
    () =>
      new Set(
        (latestDeployment?.externalMcps ?? []).map(
          (mcp) => mcp.registryServerSpecifier,
        ),
      ),
    [latestDeployment?.externalMcps],
  );

  const [phase, setPhase] = useState<ReleasePhase>("configure");
  const [serverConfigs, setServerConfigs] = useState<ServerConfig[]>([]);
  const [deploymentId, setDeploymentId] = useState<string | undefined>();
  const [toolsetStatuses, setToolsetStatuses] = useState<ServerToolsetStatus[]>(
    [],
  );
  const [error, setError] = useState<string | undefined>();

  // State for multi-remote server selection
  const [multiRemoteConfigs, setMultiRemoteConfigs] = useState<
    MultiRemoteServerConfig[]
  >([]);
  const [currentServerIndex, setCurrentServerIndex] = useState(0);

  // Track whether we've already transitioned from deploying to complete
  const hasTransitionedRef = useRef(false);

  // Track last processed server index to prevent double-click duplicates
  const lastProcessedIndexRef = useRef(-1);

  // Initialize server configs when servers change - partition into multi/single remote
  useEffect(() => {
    const multiRemote: MultiRemoteServerConfig[] = [];
    const singleRemote: ServerConfig[] = [];

    for (const server of servers) {
      const remotes = server.remotes ?? [];
      if (remotes.length > 1) {
        multiRemote.push({
          server,
          name: server.title ?? server.registrySpecifier,
          remotes,
          selectedRemoteUrls: new Set(),
        });
      } else {
        singleRemote.push({
          server,
          name: server.title ?? server.registrySpecifier,
        });
      }
    }

    setMultiRemoteConfigs(multiRemote);
    setServerConfigs(singleRemote);
    setCurrentServerIndex(0);

    // Start in selectRemotes phase if there are multi-remote servers
    if (multiRemote.length > 0) {
      setPhase("selectRemotes");
    } else {
      setPhase("configure");
    }
  }, [servers]);

  // Poll deployment status — pass gramProject for cross-project batch flow
  const { data: deploymentData } = useDeployment(
    { id: deploymentId!, gramProject: projectSlug },
    undefined,
    {
      enabled: !!deploymentId && phase === "deploying",
      refetchInterval: (query) => {
        const status = query.state.data?.status;
        if (status === "completed" || status === "failed") return false;
        return 2000;
      },
    },
  );

  // Poll deployment logs — keep polling in deploying phase OR briefly in error phase to capture final logs
  const { data: logsData } = useDeploymentLogs(
    { deploymentId: deploymentId!, gramProject: projectSlug },
    undefined,
    {
      enabled: !!deploymentId && (phase === "deploying" || phase === "error"),
      refetchInterval: phase === "deploying" ? 2000 : false,
    },
  );

  const deploymentLogs = logsData?.events ?? [];
  // Use status from both deployment and logs endpoints for faster detection
  const deploymentStatus = deploymentData?.status ?? logsData?.status;

  // Transition from deploying → complete (then toolset creation starts)
  // or deploying → error
  useEffect(() => {
    if (phase !== "deploying" || !deploymentStatus) return;
    if (hasTransitionedRef.current) return;

    if (deploymentStatus === "completed") {
      hasTransitionedRef.current = true;
      // Initialize toolset statuses as pending, then start creating them
      const statuses: ServerToolsetStatus[] = serverConfigs.map((config) => ({
        name: config.name,
        slug: generateSlug(config.name),
        status: "pending" as const,
      }));
      setToolsetStatuses(statuses);
      setPhase("complete");
    } else if (deploymentStatus === "failed") {
      hasTransitionedRef.current = true;
      setError("Deployment failed. Check the logs for details.");
      setPhase("error");
    }
  }, [phase, deploymentStatus, serverConfigs]);

  // Create toolsets when entering complete phase
  useEffect(() => {
    if (phase !== "complete") return;
    // Only run if there are pending toolsets
    if (
      toolsetStatuses.length === 0 ||
      !toolsetStatuses.some((s) => s.status === "pending")
    )
      return;

    const reqOpts = projectSlug
      ? { headers: { "gram-project": projectSlug } }
      : undefined;

    async function createToolsets() {
      for (let i = 0; i < serverConfigs.length; i++) {
        const config = serverConfigs[i];
        const slug = generateSlug(config.server.registrySpecifier);

        setToolsetStatuses((prev) =>
          prev.map((s, idx) =>
            idx === i ? { ...s, status: "creating" as const } : s,
          ),
        );

        try {
          let toolUrns = [`tools:externalmcp:${slug}:proxy`];
          if (config.server.tools) {
            toolUrns = config.server.tools.map(
              (t) => `tools:externalmcp:${slug}:${t.name}`,
            );
          }

          const toolset = await client.toolsets.create(
            {
              createToolsetRequestBody: {
                name: config.name,
                description:
                  config.server.description ??
                  `MCP server: ${config.server.registrySpecifier}`,
                toolUrns,
              },
            },
            undefined,
            reqOpts,
          );

          await client.toolsets.updateBySlug(
            {
              slug: toolset.slug,
              updateToolsetRequestBody: {
                mcpEnabled: true,
                mcpIsPublic: true,
              },
            },
            undefined,
            reqOpts,
          );

          const updatedToolset = await client.toolsets.getBySlug(
            { slug: toolset.slug },
            undefined,
            reqOpts,
          );

          setToolsetStatuses((prev) =>
            prev.map((s, idx) =>
              idx === i
                ? {
                    ...s,
                    status: "completed" as const,
                    toolsetSlug: toolset.slug,
                    mcpSlug: updatedToolset.mcpSlug,
                  }
                : s,
            ),
          );
        } catch (err) {
          setToolsetStatuses((prev) =>
            prev.map((s, idx) =>
              idx === i
                ? {
                    ...s,
                    status: "failed" as const,
                    error: err instanceof Error ? err.message : String(err),
                  }
                : s,
            ),
          );
        }
      }
    }

    createToolsets();
  }, [phase]);

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

  // Callbacks for multi-remote selection phase
  const updateCurrentConfig = useCallback(
    (updates: { name?: string; selectedRemoteUrls?: Set<string> }) => {
      setMultiRemoteConfigs((prev) =>
        prev.map((config, i) => {
          if (i !== currentServerIndex) return config;
          return {
            ...config,
            ...(updates.name !== undefined && { name: updates.name }),
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
      // Add the configured multi-remote server to serverConfigs
      const selectedRemotes = currentConfig.remotes.filter((r) =>
        currentConfig.selectedRemoteUrls.has(r.url),
      );
      setServerConfigs((prev) => [
        ...prev,
        {
          server: currentConfig.server,
          name: currentConfig.name,
          selectedRemotes,
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

  const canDeploy = useMemo(() => {
    return (
      serverConfigs.length > 0 &&
      serverConfigs.every((c) => c.name.trim() !== "")
    );
  }, [serverConfigs]);

  // goBack returns to selectRemotes phase - only available if there were multi-remote servers
  const goBack = useCallback(() => {
    // Remove multi-remote servers from serverConfigs (they came from multiRemoteConfigs)
    setServerConfigs((prev) =>
      prev.filter((config) => !config.selectedRemotes),
    );
    setCurrentServerIndex(0);
    lastProcessedIndexRef.current = -1; // Reset to allow re-processing
    setPhase("selectRemotes");
  }, []);

  // Only provide goBack if we originally had multi-remote servers
  const hasMultiRemoteServers = useMemo(() => {
    return servers.some((s) => (s.remotes ?? []).length > 1);
  }, [servers]);

  const startDeployment = useCallback(async () => {
    if (!canDeploy) return;

    setPhase("deploying");
    setError(undefined);
    hasTransitionedRef.current = false;

    const reqOpts = projectSlug
      ? { headers: { "gram-project": projectSlug } }
      : undefined;

    try {
      const result = await client.deployments.evolveDeployment(
        {
          evolveForm: {
            deploymentId: latestDeployment?.id,
            nonBlocking: true,
            upsertExternalMcps: serverConfigs.map((config) => {
              const slug = generateSlug(config.server.registrySpecifier);
              return {
                registryId: config.server.registryId,
                name: config.name,
                slug,
                registryServerSpecifier: config.server.registrySpecifier,
                selectedRemotes: config.selectedRemotes?.map((r) => r.url),
              };
            }),
          },
        },
        undefined,
        reqOpts,
      );

      if (result.deployment?.id) {
        setDeploymentId(result.deployment.id);
      } else {
        // No deployment ID — may have completed synchronously
        const statuses: ServerToolsetStatus[] = serverConfigs.map((config) => ({
          name: config.name,
          slug: generateSlug(config.name),
          status: "pending" as const,
        }));
        setToolsetStatuses(statuses);
        setPhase("complete");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setPhase("error");
    }
  }, [canDeploy, client, latestDeployment?.id, projectSlug, serverConfigs]);

  const reset = useCallback(() => {
    setDeploymentId(undefined);
    setToolsetStatuses([]);
    setError(undefined);
    hasTransitionedRef.current = false;
    setCurrentServerIndex(0);

    // Re-partition servers into multi/single remote
    const multiRemote: MultiRemoteServerConfig[] = [];
    const singleRemote: ServerConfig[] = [];

    for (const server of servers) {
      const remotes = server.remotes ?? [];
      if (remotes.length > 1) {
        multiRemote.push({
          server,
          name: server.title ?? server.registrySpecifier,
          remotes,
          selectedRemoteUrls: new Set(),
        });
      } else {
        singleRemote.push({
          server,
          name: server.title ?? server.registrySpecifier,
        });
      }
    }

    setMultiRemoteConfigs(multiRemote);
    setServerConfigs(singleRemote);

    if (multiRemote.length > 0) {
      setPhase("selectRemotes");
    } else {
      setPhase("configure");
    }
  }, [servers]);

  const base: WorkflowBase = {
    projectSlug,
    existingSpecifiers,
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
        canDeploy,
        startDeployment,
        goBack: hasMultiRemoteServers ? goBack : undefined,
        ...base,
      };
    case "deploying":
      return {
        phase,
        deploymentId: deploymentId!,
        deploymentStatus,
        deploymentLogs,
        ...base,
      };
    case "complete":
      return { phase, toolsetStatuses, ...base };
    case "error":
      return { phase, error: error!, deploymentId, deploymentLogs, ...base };
  }
}
