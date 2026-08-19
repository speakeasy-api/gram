import type { JourneyStatus } from "@/components/project-guide/journeys";
import { AUTOMATIC_CATALOG_SERVER_NAMES } from "@/components/project-guide/journeys";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import { useListMCPCatalog } from "@/pages/catalog/hooks";
import {
  isPulseMcpServer,
  requiresManualSetup,
} from "@/pages/catalog/hooks/serverMetadata";
import { filterToHttpRemotes } from "@/pages/catalog/remotes";
import { useRemoteMcpInstallWorkflow } from "@/pages/catalog/useRemoteMcpInstallWorkflow";
import { getServerURL } from "@/lib/utils";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { motion } from "motion/react";
import { type ReactNode, useEffect, useMemo, useState } from "react";

type JourneyPhase = "selection" | "deployment" | "verification";

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
  const [phase, setPhase] = useState(() => initialPhase(status));
  const [selectedServer, setSelectedServer] = useState<PulseMCPServer>();
  const [showMore, setShowMore] = useState(false);
  const catalog = useListMCPCatalog(
    undefined,
    undefined,
    expanded && phase === "selection",
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
  const { data: mcpServersData, refetch: refetchMcpServers } = useMcpServers(
    { gramProject },
    undefined,
    { throwOnError: false },
  );
  const { data: endpointsData, refetch: refetchEndpoints } = useMcpEndpoints(
    { gramProject },
    undefined,
    { throwOnError: false },
  );
  const { data: pluginsData, refetch: refetchPlugins } = usePlugins(
    { gramProject },
    undefined,
    { throwOnError: false },
  );
  const workflow = useRemoteMcpInstallWorkflow({
    servers: selectedServer ? [filterToHttpRemotes(selectedServer)] : [],
    projectSlug: gramProject,
    autoSelectRemotes: true,
  });
  const mcpServer = mcpServersData?.mcpServers.find((server) =>
    Boolean(server.remoteMcpServerId),
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

  useEffect(() => {
    if (workflow.phase !== "complete") return;
    void refetchMcpServers();
    void refetchEndpoints();
    void refetchPlugins();
  }, [workflow.phase, refetchEndpoints, refetchMcpServers, refetchPlugins]);

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
            label: "Installed as a remote MCP server",
            status: "completed" as const,
          },
          {
            label: defaultPluginComplete
              ? "Attached to the Default plugin"
              : "Waiting for the Default plugin",
            status: defaultPluginComplete
              ? ("completed" as const)
              : ("pending" as const),
          },
          {
            label: endpointUrl
              ? "Created a governed endpoint"
              : "Creating a governed endpoint",
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
                { label: "Read the server's tool list", status: "completed" },
                { label: "Install it into this project", status: "pending" },
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
                label: installStatusLabel(status.status),
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
    return (
      <JourneyPanel
        title="Verify your connection"
        onSwitchJourney={onSwitchJourney}
      >
        <p className="text-muted-foreground text-[13px] leading-[1.6]">
          {mcpServer?.name ?? "This server"} has a governed endpoint
          {endpointUrl ? ` at ${endpointUrl}` : ""}. Review the connection to
          continue.
        </p>
        <button
          type="button"
          onClick={onComplete}
          className="border-foreground bg-foreground text-background px-3 py-2 font-mono text-[11px] tracking-[0.05em] uppercase"
        >
          Complete journey
        </button>
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

function DeploymentStatuses({
  statuses,
}: {
  statuses: Array<{
    error?: string;
    label: string;
    status: "pending" | "creating" | "completed" | "failed";
  }>;
}): JSX.Element {
  return (
    <ol className="grid gap-2">
      {statuses.map((item, index) => (
        <motion.li
          key={`${item.label}-${item.status}`}
          initial={{ opacity: 0, y: 4 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{
            delay: index * 0.04,
            duration: 0.2,
            ease: [0.2, 0.7, 0.3, 1],
          }}
          className="border-border flex items-center gap-2 border px-3 py-2"
        >
          <span
            aria-hidden="true"
            className={`size-1.5 ${deploymentStatusClass(item.status)}`}
          />
          <span className="text-[12px]">{item.label}</span>
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
      return "bg-[#5A8250]";
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
      className="border-border grid gap-4 border-l-2 border-l-[#2879D8] py-4 pl-4"
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
