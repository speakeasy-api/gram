import type { JourneyStatus } from "@/components/project-guide/journeys";
import { AUTOMATIC_CATALOG_SERVER_NAMES } from "@/components/project-guide/journeys";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { mcpServerRouteParam } from "@/lib/sources";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import { useListMCPCatalog } from "@/pages/catalog/hooks";
import {
  isPulseMcpServer,
  requiresManualSetup,
} from "@/pages/catalog/hooks/serverMetadata";
import {
  filterToHttpRemotes,
  normalizeRemoteUrl,
} from "@/pages/catalog/remotes";
import { useRemoteMcpInstallWorkflow } from "@/pages/catalog/useRemoteMcpInstallWorkflow";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { McpServerActivity } from "@gram/client/models/components/mcpserveractivity.js";
import { useGetMcpServerActivity } from "@gram/client/react-query/getMcpServerActivity.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { motion, useReducedMotion } from "motion/react";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";

type JourneyPhase = "selection" | "deployment" | "verification";
type Client = "claude" | "cursor" | "codex";

function initialPhase(status: JourneyStatus): JourneyPhase {
  if (status === "not-started") return "selection";
  return status === "in-progress" ? "deployment" : "verification";
}

function catalogServerName(server: PulseMCPServer): string {
  return server.title ?? server.registrySpecifier;
}

function compareCatalogServers(a: PulseMCPServer, b: PulseMCPServer): number {
  const aIndex = AUTOMATIC_CATALOG_SERVER_NAMES.indexOf(
    catalogServerName(a) as (typeof AUTOMATIC_CATALOG_SERVER_NAMES)[number],
  );
  const bIndex = AUTOMATIC_CATALOG_SERVER_NAMES.indexOf(
    catalogServerName(b) as (typeof AUTOMATIC_CATALOG_SERVER_NAMES)[number],
  );
  const aRank = aIndex === -1 ? Infinity : aIndex;
  const bRank = bIndex === -1 ? Infinity : bIndex;
  return (
    aRank - bRank || catalogServerName(a).localeCompare(catalogServerName(b))
  );
}

function clientConfig(
  client: Client,
  serverSlug: string,
  endpointUrl: string,
): { code: string; language: string } {
  if (client === "codex") {
    return {
      code: `[mcp_servers.${serverSlug}]\nurl = "${endpointUrl}"`,
      language: "toml",
    };
  }

  return {
    code: JSON.stringify(
      { mcpServers: { [serverSlug]: { url: endpointUrl } } },
      null,
      2,
    ),
    language: "json",
  };
}

function clientLabel(client: Client): string {
  switch (client) {
    case "claude":
      return "Claude";
    case "cursor":
      return "Cursor";
    case "codex":
      return "Codex";
  }
}

export function ThirdPartyMcpJourney({
  status,
  onComplete,
  onSwitchJourney,
  expanded = true,
}: {
  status: JourneyStatus;
  onComplete: () => void;
  onSwitchJourney: () => void;
  expanded?: boolean;
}): JSX.Element | null {
  const gramProject = useProjectSlugForRequests();
  const routes = useRoutes();
  const [phase, setPhase] = useState(() => initialPhase(status));
  const [selectedServer, setSelectedServer] = useState<PulseMCPServer>();
  const [showMore, setShowMore] = useState(false);
  const [client, setClient] = useState<Client>("claude");
  const [isPromptPhase, setIsPromptPhase] = useState(false);
  const [isListening, setIsListening] = useState(false);
  const [hasCopiedConfig, setHasCopiedConfig] = useState(false);
  const [hasCopiedPrompt, setHasCopiedPrompt] = useState(false);
  const [promptStartedAt, setPromptStartedAt] = useState<Date>();
  const [activityBaseline, setActivityBaseline] = useState<Date>();
  const completionNotified = useRef(false);
  const shouldReduceMotion = useReducedMotion();
  const catalog = useListMCPCatalog(
    undefined,
    undefined,
    expanded &&
      (phase === "selection" ||
        phase === "deployment" ||
        phase === "verification"),
  );
  const deployableServers = useMemo(
    () =>
      ((catalog.data?.servers ?? []) as PulseMCPServer[])
        .filter(isPulseMcpServer)
        .filter((server) => !requiresManualSetup(server))
        .map(filterToHttpRemotes)
        .filter((server) => (server.remotes?.length ?? 0) > 0)
        .sort(compareCatalogServers),
    [catalog.data?.servers],
  );
  const {
    data: mcpServersData,
    isError: mcpServersError,
    refetch: refetchMcpServers,
  } = useMcpServers({ gramProject }, undefined, { throwOnError: false });
  const {
    data: endpointsData,
    isError: endpointsError,
    refetch: refetchEndpoints,
  } = useMcpEndpoints({ gramProject }, undefined, { throwOnError: false });
  const {
    data: pluginsData,
    isError: pluginsError,
    refetch: refetchPlugins,
  } = usePlugins({ gramProject }, undefined, { throwOnError: false });
  const {
    data: remoteMcpServersData,
    isError: remoteMcpServersError,
    refetch: refetchRemoteMcpServers,
  } = useRemoteMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });
  const workflow = useRemoteMcpInstallWorkflow({
    servers: selectedServer ? [filterToHttpRemotes(selectedServer)] : [],
    projectSlug: gramProject,
    autoSelectRemotes: true,
  });
  const catalogIdentityServers = selectedServer
    ? [filterToHttpRemotes(selectedServer)]
    : deployableServers;
  const catalogRemoteUrls = new Set(
    catalogIdentityServers.flatMap((server) =>
      (server.remotes ?? []).map((remote) => normalizeRemoteUrl(remote.url)),
    ),
  );
  const catalogRemoteMcpServerIds = new Set(
    (remoteMcpServersData?.remoteMcpServers ?? [])
      .filter((server) => catalogRemoteUrls.has(normalizeRemoteUrl(server.url)))
      .map((server) => server.id),
  );
  const projectQueryError =
    catalog.isError ||
    mcpServersError ||
    endpointsError ||
    pluginsError ||
    remoteMcpServersError;
  const mcpServer = projectQueryError
    ? undefined
    : mcpServersData?.mcpServers.find(
        (server) =>
          server.remoteMcpServerId !== undefined &&
          catalogRemoteMcpServerIds.has(server.remoteMcpServerId),
      );
  const defaultPluginComplete = Boolean(
    mcpServer &&
    pluginsData?.plugins.some(
      (plugin) =>
        plugin.isDefault === true &&
        plugin.servers?.some((server) => server.mcpServerId === mcpServer.id),
    ),
  );
  const endpoint = endpointsData?.mcpEndpoints.find(
    (candidate) => candidate.mcpServerId === mcpServer?.id,
  );
  const endpointUrl = endpoint
    ? `${getServerURL()}/mcp/${endpoint.slug}`
    : workflow.phase === "complete"
      ? workflow.statuses.find((status) => status.mcpServerId === mcpServer?.id)
          ?.mcpEndpointUrl
      : undefined;
  const {
    data: activityData,
    isError: activityError,
    refetch: refetchActivity,
  } = useGetMcpServerActivity(
    { gramProject, getMcpServerActivityPayload: {} },
    undefined,
    {
      enabled: Boolean(endpointUrl),
      refetchInterval: endpointUrl
        ? (query) => {
            const activity = query.state.data?.activity.find(
              (entry) =>
                entry.targetType === "hosted_mcp_server" &&
                entry.targetId === mcpServer?.slug &&
                entry.totalToolCalls > 0,
            );
            if (activity && !isPromptPhase) return false;
            if (
              isPromptPhase &&
              promptStartedAt &&
              activity?.lastToolCallAt &&
              activity.lastToolCallAt >= promptStartedAt &&
              (!activityBaseline || activity.lastToolCallAt > activityBaseline)
            ) {
              return false;
            }
            return 5_000;
          }
        : false,
      throwOnError: false,
    },
  );
  const serverActivity = activityError
    ? undefined
    : activityData?.activity.find(
        (activity) =>
          activity.targetType === "hosted_mcp_server" &&
          activity.targetId === mcpServer?.slug &&
          activity.totalToolCalls > 0,
      );
  const activityAfterPrompt = Boolean(
    isListening &&
    promptStartedAt &&
    serverActivity?.lastToolCallAt &&
    serverActivity.lastToolCallAt >= promptStartedAt &&
    (!activityBaseline || serverActivity.lastToolCallAt > activityBaseline),
  );
  const activityCompletesJourney = Boolean(
    serverActivity && (!isPromptPhase || activityAfterPrompt),
  );

  useEffect(() => {
    if (!activityCompletesJourney || completionNotified.current) return;
    completionNotified.current = true;
    onComplete();
  }, [activityCompletesJourney, onComplete]);

  useEffect(() => {
    if (workflow.phase !== "complete") return;
    void refetchMcpServers();
    void refetchEndpoints();
    void refetchPlugins();
    void refetchRemoteMcpServers();
    if (!workflow.statuses.some((status) => status.status === "failed")) {
      setPhase("verification");
    }
  }, [
    workflow.phase,
    refetchEndpoints,
    refetchMcpServers,
    refetchPlugins,
    refetchRemoteMcpServers,
  ]);

  if (!expanded) return null;

  const chooseServer = (server: PulseMCPServer) => {
    setSelectedServer(server);
    setPhase("deployment");
  };

  if (phase === "deployment") {
    const hasFailedInstall =
      workflow.phase === "installing" || workflow.phase === "complete"
        ? workflow.statuses.some((status) => status.status === "failed")
        : false;
    const showWorkflowStatuses =
      !mcpServer || workflow.phase === "installing" || hasFailedInstall;
    const resumedStatuses = mcpServer
      ? [
          {
            key: "remote-server",
            label: "Installed as a remote MCP server",
            name: mcpServer.name ?? "Catalog server",
            status: "completed" as const,
          },
          {
            key: "default-plugin",
            label: defaultPluginComplete
              ? "Attached to the Default plugin"
              : "Waiting for the Default plugin",
            name: "Default plugin",
            status: defaultPluginComplete
              ? ("completed" as const)
              : ("pending" as const),
          },
          {
            key: "endpoint",
            label: endpointUrl
              ? "Created a governed endpoint"
              : "Creating a governed endpoint",
            name: "Governed endpoint",
            status: endpointUrl
              ? ("completed" as const)
              : ("creating" as const),
          },
        ]
      : [];

    return (
      <JourneyPanel
        title="Deploy your server"
        onSwitchJourney={onSwitchJourney}
      >
        <p className="text-muted-foreground text-[13px] leading-[1.6]">
          {selectedServer
            ? `${catalogServerName(selectedServer)} is ready to deploy as a governed endpoint.`
            : "Your catalog server is already in this project. Continue deployment from its current state."}
        </p>
        {!showWorkflowStatuses ? (
          <DeploymentStatuses statuses={resumedStatuses} />
        ) : workflow.phase === "configure" ? (
          <>
            <DeploymentStatuses
              statuses={[
                {
                  key: "catalog-read",
                  label: "Read the server's tool list",
                  name: selectedServer
                    ? catalogServerName(selectedServer)
                    : "Catalog server",
                  status: "completed",
                },
                {
                  key: "catalog-install",
                  label: "Install it into this project",
                  name: selectedServer
                    ? catalogServerName(selectedServer)
                    : "Catalog server",
                  status: "pending",
                },
              ]}
            />
            <button
              type="button"
              onClick={() => void workflow.startInstall()}
              disabled={!workflow.canInstall}
              className="border-foreground bg-foreground text-background disabled:border-border disabled:bg-muted disabled:text-muted-foreground px-3 py-2 font-mono text-[11px] tracking-[0.05em] uppercase"
            >
              Install server
            </button>
          </>
        ) : workflow.phase === "installing" || workflow.phase === "complete" ? (
          <>
            <DeploymentStatuses
              statuses={workflow.statuses.map((status) => ({
                error: status.error,
                key: status.key,
                label: installStatusLabel(status.status),
                name: status.name,
                status: status.status,
              }))}
            />
            {hasFailedInstall && (
              <button
                type="button"
                onClick={workflow.reset}
                className="border-border border px-3 py-2 font-mono text-[11px] tracking-[0.05em] uppercase"
              >
                Retry installation
              </button>
            )}
          </>
        ) : null}
      </JourneyPanel>
    );
  }

  if (phase === "verification") {
    const serverName = mcpServer?.name ?? "this server";
    const serverSlug = mcpServer?.slug;
    const config =
      endpointUrl && serverSlug
        ? clientConfig(client, serverSlug, endpointUrl)
        : undefined;
    const prompt = `Using the ${serverName} MCP server, list the read-only tools you can call and summarise what each one reads.`;
    const startPrompt = () => {
      setActivityBaseline(serverActivity?.lastToolCallAt);
      setPromptStartedAt(new Date());
      setIsPromptPhase(true);
    };

    return (
      <JourneyPanel
        title="Verify your connection"
        onSwitchJourney={onSwitchJourney}
      >
        <p className="text-muted-foreground text-[13px] leading-[1.6]">
          {serverName} has a governed endpoint. Connect your client, then send
          one read-only tools/list request to watch the first call arrive.
        </p>
        {endpointUrl && (
          <a
            href={endpointUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-information-default w-fit font-mono text-[11px] underline underline-offset-2"
          >
            {endpointUrl}
          </a>
        )}
        {mcpServer && (
          <Link
            to={routes.mcp.x.overview.href(mcpServerRouteParam(mcpServer))}
            className="text-information-default w-fit font-mono text-[11px] underline underline-offset-2"
          >
            View {serverName} MCP server
          </Link>
        )}
        {activityCompletesJourney && serverActivity ? (
          <JourneyCompletion
            activity={serverActivity}
            serverName={serverName}
            shouldReduceMotion={shouldReduceMotion}
          />
        ) : config && !isPromptPhase ? (
          <>
            <div className="border-border flex gap-3 border-b">
              {(["claude", "cursor", "codex"] as const).map((name) => (
                <button
                  key={name}
                  type="button"
                  onClick={() => setClient(name)}
                  className={`border-b-2 px-1 pb-2 font-mono text-[10px] tracking-[0.05em] uppercase ${
                    client === name
                      ? "border-foreground"
                      : "border-transparent text-muted-foreground"
                  }`}
                >
                  {clientLabel(name)}
                </button>
              ))}
            </div>
            <CodeSnippet
              code={config.code}
              language={config.language}
              copyable
              onSelectOrCopy={() => setHasCopiedConfig(true)}
            />
            <button
              type="button"
              onClick={startPrompt}
              disabled={!hasCopiedConfig}
              className="border-foreground bg-foreground text-background disabled:border-border disabled:bg-muted disabled:text-muted-foreground px-3 py-2 font-mono text-[11px] tracking-[0.05em] uppercase"
            >
              I've connected it
            </button>
          </>
        ) : isPromptPhase ? (
          <>
            <CodeSnippet
              code={prompt}
              language="text"
              copyable
              onSelectOrCopy={() => setHasCopiedPrompt(true)}
            />
            {!isListening && (
              <button
                type="button"
                onClick={() => setIsListening(true)}
                disabled={!hasCopiedPrompt}
                className="border-foreground bg-foreground text-background disabled:border-border disabled:bg-muted disabled:text-muted-foreground px-3 py-2 font-mono text-[11px] tracking-[0.05em] uppercase"
              >
                Sent it
              </button>
            )}
            {isListening && activityError ? (
              <>
                <p className="text-destructive text-[13px]">
                  Could not check for the first governed call.
                </p>
                <button
                  type="button"
                  onClick={() => void refetchActivity()}
                  className="border-border border px-3 py-2 font-mono text-[11px] tracking-[0.05em] uppercase"
                >
                  Retry activity check
                </button>
              </>
            ) : isListening ? (
              <motion.p
                initial={shouldReduceMotion ? false : { opacity: 0, y: 4 }}
                animate={{ opacity: 1, y: 0 }}
                transition={
                  shouldReduceMotion ? { duration: 0 } : { duration: 0.22 }
                }
                className="text-muted-foreground flex items-center gap-2 font-mono text-[11px]"
              >
                <motion.span
                  aria-hidden="true"
                  animate={
                    shouldReduceMotion
                      ? { opacity: 1 }
                      : { opacity: [0.2, 1, 0.2] }
                  }
                  transition={
                    shouldReduceMotion
                      ? { duration: 0 }
                      : { duration: 1.1, repeat: Infinity }
                  }
                  className="bg-foreground size-1.5"
                />
                Listening for the first call on your endpoint
              </motion.p>
            ) : null}
          </>
        ) : null}
        {activityError && !isPromptPhase && (
          <>
            <p className="text-destructive text-[13px]">
              Could not check for the first governed call.
            </p>
            <button
              type="button"
              onClick={() => void refetchActivity()}
              className="border-border border px-3 py-2 font-mono text-[11px] tracking-[0.05em] uppercase"
            >
              Retry activity check
            </button>
          </>
        )}
      </JourneyPanel>
    );
  }

  if (
    catalog.isError ||
    (!catalog.isPending && deployableServers.length === 0)
  ) {
    return (
      <JourneyPanel
        title="Pick a server from the catalog"
        onSwitchJourney={onSwitchJourney}
      >
        <p className="text-muted-foreground text-[13px] leading-[1.6]">
          No automatic servers are available right now.
        </p>
        <button
          type="button"
          onClick={() => void catalog.refetch()}
          className="border-border border px-3 py-2 font-mono text-[11px] tracking-[0.05em] uppercase"
        >
          Retry catalog
        </button>
      </JourneyPanel>
    );
  }

  const primaryServers = deployableServers.slice(0, 5);
  const moreServers = deployableServers.slice(5);
  return (
    <JourneyPanel
      title="Pick a server from the catalog"
      onSwitchJourney={onSwitchJourney}
    >
      <p className="text-muted-foreground text-[13px] leading-[1.6]">
        The catalog lists servers from the official MCP Registry. Installing one
        creates a governed endpoint in front of the vendor's server — the
        vendor's URL is already known, and nothing upstream changes.
      </p>
      {catalog.isPending ? (
        <span className="text-muted-foreground font-mono text-[10px] tracking-[0.05em] uppercase">
          Loading catalog
        </span>
      ) : (
        <div className="grid gap-2 sm:grid-cols-2">
          {primaryServers.map((server) => (
            <ServerButton
              key={server.registrySpecifier}
              server={server}
              onClick={chooseServer}
            />
          ))}
          {showMore &&
            moreServers.map((server) => (
              <ServerButton
                key={server.registrySpecifier}
                server={server}
                onClick={chooseServer}
              />
            ))}
          {!showMore && moreServers.length > 0 && (
            <button
              type="button"
              onClick={() => setShowMore(true)}
              className="border-border text-muted-foreground border px-3 py-2 text-left font-mono text-[10px] tracking-[0.05em] uppercase"
            >
              More automatic servers
            </button>
          )}
        </div>
      )}
    </JourneyPanel>
  );
}

function installStatusLabel(
  status: "pending" | "creating" | "completed" | "failed",
): string {
  switch (status) {
    case "pending":
      return "Waiting to install";
    case "creating":
      return "Creating remote MCP server";
    case "completed":
      return "Installed as a remote MCP server";
    case "failed":
      return "Install failed";
  }
}

function JourneyCompletion({
  activity,
  serverName,
  shouldReduceMotion,
}: {
  activity: McpServerActivity;
  serverName: string;
  shouldReduceMotion: boolean | null;
}): JSX.Element {
  return (
    <motion.div
      initial={shouldReduceMotion ? false : { opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={shouldReduceMotion ? { duration: 0 } : { duration: 0.22 }}
      className="border-l-2 border-l-success-default grid gap-3 pl-3"
    >
      <div className="grid gap-1">
        <span className="text-success-default font-mono text-[10px] tracking-[0.05em] uppercase">
          Journey A complete
        </span>
        <h5 className="text-[24px] leading-[1.1]">The path is governed.</h5>
        <p className="text-muted-foreground text-[13px] leading-[1.6]">
          {serverName} is connected through your governed endpoint, and its
          calls are recorded in Tool Logs.
        </p>
      </div>
      <GovernedCallEvent
        activity={activity}
        serverName={serverName}
        shouldReduceMotion={shouldReduceMotion}
      />
    </motion.div>
  );
}

function GovernedCallEvent({
  activity,
  serverName,
  shouldReduceMotion,
}: {
  activity: McpServerActivity;
  serverName: string;
  shouldReduceMotion: boolean | null;
}): JSX.Element {
  return (
    <motion.div
      initial={shouldReduceMotion ? false : { opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={shouldReduceMotion ? { duration: 0 } : { duration: 0.22 }}
      className="border-border grid gap-3 border px-3 py-3"
    >
      <span className="font-mono text-[10px] tracking-[0.05em] uppercase">
        Governed call
      </span>
      <p className="text-[13px]">{activity.targetLabel}</p>
      <dl className="text-muted-foreground grid gap-1 font-mono text-[10px]">
        <div className="flex justify-between gap-3">
          <dt>Server</dt>
          <dd>{serverName}</dd>
        </div>
        <div className="flex justify-between gap-3">
          <dt>Recent calls</dt>
          <dd>{activity.recentToolCalls}</dd>
        </div>
        <div className="flex justify-between gap-3">
          <dt>Recorded calls</dt>
          <dd>{activity.totalToolCalls}</dd>
        </div>
      </dl>
    </motion.div>
  );
}

function DeploymentStatuses({
  statuses,
}: {
  statuses: Array<{
    error?: string;
    key: string;
    label: string;
    name?: string;
    status: "pending" | "creating" | "completed" | "failed";
  }>;
}): JSX.Element {
  const shouldReduceMotion = useReducedMotion();

  return (
    <ol className="grid gap-2">
      {statuses.map((item, index) => (
        <motion.li
          key={item.key}
          initial={shouldReduceMotion ? false : { opacity: 0, y: 4 }}
          animate={{ opacity: 1, y: 0 }}
          transition={
            shouldReduceMotion
              ? { duration: 0 }
              : {
                  delay: index * 0.04,
                  duration: 0.2,
                  ease: [0.2, 0.7, 0.3, 1],
                }
          }
          className="border-border flex items-center gap-2 border px-3 py-2"
        >
          <span
            aria-hidden="true"
            className={`size-1.5 ${deploymentStatusClass(item.status)}`}
          />
          {item.name && <span className="text-[12px]">{item.name}</span>}
          <span className="text-muted-foreground text-[11px]">
            {item.label}
          </span>
          {item.error && (
            <span className="text-destructive ml-auto text-[11px]">
              {item.error}
            </span>
          )}
        </motion.li>
      ))}
    </ol>
  );
}

function deploymentStatusClass(
  status: "pending" | "creating" | "completed" | "failed",
): string {
  switch (status) {
    case "completed":
      return "bg-success-default";
    case "failed":
      return "bg-destructive";
    case "pending":
    case "creating":
      return "bg-muted-foreground";
  }
}

function JourneyPanel({
  title,
  children,
  onSwitchJourney,
}: {
  title: string;
  children: ReactNode;
  onSwitchJourney: () => void;
}): JSX.Element {
  return (
    <motion.section
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.34, ease: [0.2, 0.7, 0.3, 1] }}
      className="border-border grid gap-4 border-l-2 border-l-information-default py-4 pl-4"
    >
      <div className="flex items-center justify-between gap-4">
        <h4 className="text-[19px] leading-[1.2]">{title}</h4>
        <button
          type="button"
          onClick={onSwitchJourney}
          className="text-muted-foreground font-mono text-[10px] tracking-[0.05em] uppercase"
        >
          Switch journey
        </button>
      </div>
      {children}
    </motion.section>
  );
}

function ServerButton({
  server,
  onClick,
}: {
  server: PulseMCPServer;
  onClick: (server: PulseMCPServer) => void;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={() => onClick(server)}
      className="border-border hover:border-foreground flex items-center gap-2 border px-3 py-2 text-left transition-colors"
    >
      <span aria-hidden="true" className="bg-foreground size-1.5" />
      <span className="text-[12px]">{catalogServerName(server)}</span>
      <span className="text-muted-foreground ml-auto font-mono text-[10px]">
        {server.toolCount} tools
      </span>
    </button>
  );
}
