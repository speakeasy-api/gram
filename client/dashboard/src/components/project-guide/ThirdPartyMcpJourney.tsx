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
import { motion } from "motion/react";
import { type ReactNode, useMemo, useState } from "react";

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
  const workflow = useRemoteMcpInstallWorkflow({
    servers: selectedServer ? [filterToHttpRemotes(selectedServer)] : [],
    projectSlug: gramProject,
    autoSelectRemotes: true,
  });

  if (!expanded) return null;

  const chooseServer = (server: PulseMCPServer) => {
    setSelectedServer(server);
    setPhase("deployment");
  };

  if (phase === "deployment") {
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
        <span className="text-muted-foreground font-mono text-[10px] tracking-[0.05em] uppercase">
          {workflow.phase === "complete"
            ? "Ready to verify"
            : "Preparing deployment"}
        </span>
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
          This governed endpoint already has recorded activity. Review the
          connection to continue.
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
