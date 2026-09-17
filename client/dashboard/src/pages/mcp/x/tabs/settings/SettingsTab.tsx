import {
  AuthenticationSection,
  MCP_AUTHENTICATION_SECTION_ID,
} from "./sections/authentication/AuthenticationSection";
import {
  MCP_SERVER_URL_SECTION_ID,
  ServerUrlSection,
} from "./sections/ServerUrlSection";

import { getTunneledMcpServerArgs } from "@/lib/sources";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { useGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { useGetUnproxiedMcpServer } from "@gram/client/react-query/getUnproxiedMcpServer.js";
import { Fragment, useEffect } from "react";
import { useLocation } from "react-router";
import {
  AgentSetupSection,
  MCP_AGENT_SETUP_SECTION_ID,
} from "./sections/AgentSetupSection";
import { BrandingSection } from "./sections/BrandingSection";
import { DangerZoneSection } from "./sections/DangerZoneSection";
import { HeadersSection } from "./sections/HeadersSection";
import { NetworkAccessSection } from "./sections/NetworkAccessSection";
import {
  MCP_PUBLIC_ACCESS_SECTION_ID,
  PublicAccessSection,
} from "./sections/PublicAccessSection";
import { PublicRateLimitsSection } from "./sections/PublicRateLimitsSection";
import {
  MCP_RESOURCE_IDENTIFIER_SECTION_ID,
  ResourceIdentifierSection,
} from "./sections/ResourceIdentifierSection";
import type { SourceBackedDeleteTarget } from "./sections/sourceDelete";
import {
  MCP_SOURCE_NAME_SECTION_ID,
  RemoteSourceNameSection,
  TunneledSourceNameSection,
} from "./sections/SourceNameSection";
import { ToolFilteringSection } from "./sections/ToolFilteringSection";
import {
  MCP_TUNNEL_KEY_SECTION_ID,
  TunnelKeySection,
} from "./sections/TunnelKeySection";
import {
  MCP_UPSTREAM_URL_SECTION_ID,
  UpstreamUrlSection,
} from "./sections/UpstreamUrlSection";

// Every section that can be deep-linked from elsewhere in the dashboard
// (readiness bar, visibility picker, rate-limit hint, connections panel).
const SCROLLABLE_SECTION_IDS: readonly string[] = [
  MCP_SERVER_URL_SECTION_ID,
  MCP_AUTHENTICATION_SECTION_ID,
  MCP_SOURCE_NAME_SECTION_ID,
  MCP_UPSTREAM_URL_SECTION_ID,
  MCP_RESOURCE_IDENTIFIER_SECTION_ID,
  MCP_PUBLIC_ACCESS_SECTION_ID,
  MCP_TUNNEL_KEY_SECTION_ID,
  MCP_AGENT_SETUP_SECTION_ID,
];

// `ready` flips once the source-backed sections can be on the page: they
// mount after the source row loads, so a cold-load deep link would otherwise
// look for an element that isn't there yet.
function useScrollToSettingsHash(ready: boolean) {
  const location = useLocation();

  useEffect(() => {
    const targetId = location.hash.replace("#", "");
    if (!ready || !SCROLLABLE_SECTION_IDS.includes(targetId)) {
      return;
    }

    const animationFrame = window.requestAnimationFrame(() => {
      document
        .getElementById(targetId)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    });

    return () => window.cancelAnimationFrame(animationFrame);
  }, [location.hash, ready]);
}

export function SettingsTab({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
}): JSX.Element {
  const isUnproxied = !!mcpServer.unproxiedMcpServerId;
  // Only remote and tunneled sources add sections to the page; the unproxied
  // row feeds the danger zone alone, so a deep link never waits on it.
  const hasSourceSections =
    !!mcpServer.remoteMcpServerId || !!mcpServer.tunneledMcpServerId;

  // The source rows behind this server. Each section edits the source
  // directly, so they are fetched once here rather than per section.
  const remoteQuery = useGetRemoteMcpServer(
    { id: mcpServer.remoteMcpServerId ?? "" },
    undefined,
    { enabled: !!mcpServer.remoteMcpServerId },
  );
  const tunneledQuery = useGetTunneledMcpServer(
    getTunneledMcpServerArgs(mcpServer.tunneledMcpServerId ?? ""),
    undefined,
    { enabled: !!mcpServer.tunneledMcpServerId },
  );
  const unproxiedQuery = useGetUnproxiedMcpServer(
    { id: mcpServer.unproxiedMcpServerId ?? "" },
    undefined,
    { enabled: isUnproxied },
  );
  const remoteMcpServer = remoteQuery.data;
  const tunneledMcpServer = tunneledQuery.data;
  const unproxiedMcpServer = unproxiedQuery.data;
  const sourceUnavailable =
    remoteQuery.isError || tunneledQuery.isError || unproxiedQuery.isError;
  const sourceSettled =
    !hasSourceSections ||
    remoteQuery.isError ||
    tunneledQuery.isError ||
    !!remoteMcpServer ||
    !!tunneledMcpServer;

  useScrollToSettingsHash(sourceSettled);

  let deleteTarget: SourceBackedDeleteTarget | undefined;
  if (remoteMcpServer) {
    deleteTarget = { kind: "remote", source: remoteMcpServer };
  } else if (tunneledMcpServer) {
    deleteTarget = { kind: "tunneled", source: tunneledMcpServer };
  } else if (unproxiedMcpServer) {
    deleteTarget = { kind: "unproxied", source: unproxiedMcpServer };
  }

  // The source-backed sections are keyed by the source row so their drafts
  // remount when the route moves to a server backed by a different source;
  // this tree stays mounted across that navigation, and a draft or late save
  // result from the previous source must not land on the next one.
  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-10 px-8 py-8">
      <BrandingSection mcpServer={mcpServer} />
      {remoteMcpServer ? (
        <Fragment key={remoteMcpServer.id}>
          <RemoteSourceNameSection remoteMcpServer={remoteMcpServer} />
          <UpstreamUrlSection remoteMcpServer={remoteMcpServer} />
        </Fragment>
      ) : null}
      {tunneledMcpServer ? (
        <TunneledSourceNameSection
          key={tunneledMcpServer.id}
          tunneledMcpServer={tunneledMcpServer}
        />
      ) : null}
      {isUnproxied ? null : (
        <>
          <ServerUrlSection
            backend={{ mcpServerId: mcpServer.id }}
            endpoints={endpoints}
            isLoadingEndpoints={isLoadingEndpoints}
          />
          <NetworkAccessSection mcpServer={mcpServer} endpoints={endpoints} />
        </>
      )}
      <AuthenticationSection mcpServer={mcpServer} />
      {mcpServer.remoteMcpServerId ? (
        <HeadersSection
          remoteMcpServerId={mcpServer.remoteMcpServerId}
          mcpServerId={mcpServer.id}
          projectId={mcpServer.projectId}
        />
      ) : null}
      {tunneledMcpServer ? (
        <Fragment key={tunneledMcpServer.id}>
          <ResourceIdentifierSection tunneledMcpServer={tunneledMcpServer} />
          <PublicAccessSection tunneledMcpServer={tunneledMcpServer} />
          <PublicRateLimitsSection
            tunneledMcpServerId={tunneledMcpServer.id}
            projectId={mcpServer.projectId}
          />
          <TunnelKeySection tunneledMcpServer={tunneledMcpServer} />
          <AgentSetupSection tunneledMcpServer={tunneledMcpServer} />
        </Fragment>
      ) : null}
      {isUnproxied ? null : <ToolFilteringSection mcpServer={mcpServer} />}
      <DangerZoneSection
        mcpServer={mcpServer}
        endpoints={endpoints}
        deleteTarget={deleteTarget}
        sourceUnavailable={sourceUnavailable}
      />
    </div>
  );
}
