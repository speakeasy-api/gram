import {
  AuthenticationSection,
  MCP_AUTHENTICATION_SECTION_ID,
} from "./sections/authentication/AuthenticationSection";
import {
  MCP_SERVER_URL_SECTION_ID,
  ServerUrlSection,
} from "./sections/ServerUrlSection";

import { RequireScope } from "@/components/require-scope";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import type { Toolset } from "@/lib/toolTypes";
import {
  MCPAuthenticationTab,
  ToolsetOAuthSection,
} from "@/pages/mcp/MCPEnvironmentSettings";
import { MCPPerformanceTab } from "@/pages/mcp/MCPPerformanceTab";
import { PageSection } from "@/pages/mcp/PageSection";
import { ExportJsonSection } from "@/pages/mcp/x/hosted/ExportJsonSection";
import { HostedAddressSections } from "@/pages/mcp/x/hosted/HostedAddressSections";
import { ServerInstructionsSection } from "@/pages/mcp/x/hosted/ServerInstructionsSection";
import { isCanonicalHostedWrapper } from "@/pages/mcp/x/MCPServerDetailsRouting";
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
import { CallerIdentitySection } from "./sections/CallerIdentitySection";
import {
  IDENTITY_CHAINING_SECTION_ID,
  IdentityChainingSection,
} from "./sections/authentication/IdentityChainingSection";
import { GeneralSection } from "./sections/GeneralSection";
import { DangerZoneSection } from "./sections/DangerZoneSection";
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
  MCP_SESSIONS_SECTION_ID,
  RemoteMcpSessionsSection,
} from "./sections/RemoteMcpSessionsSection";
import { ToolFilteringSection } from "./sections/ToolFilteringSection";
import {
  MCP_TUNNEL_KEY_SECTION_ID,
  TunnelKeySection,
} from "./sections/TunnelKeySection";

// Every section that can be deep-linked from elsewhere in the dashboard
// (readiness bar, visibility picker, rate-limit hint, connections panel).
const MCP_PERFORMANCE_SECTION_ID = "performance";
const MCP_ENVIRONMENT_SECTION_ID = "environment";

const SCROLLABLE_SECTION_IDS: readonly string[] = [
  MCP_SERVER_URL_SECTION_ID,
  MCP_PERFORMANCE_SECTION_ID,
  MCP_ENVIRONMENT_SECTION_ID,
  MCP_AUTHENTICATION_SECTION_ID,
  MCP_RESOURCE_IDENTIFIER_SECTION_ID,
  MCP_PUBLIC_ACCESS_SECTION_ID,
  MCP_TUNNEL_KEY_SECTION_ID,
  MCP_AGENT_SETUP_SECTION_ID,
  IDENTITY_CHAINING_SECTION_ID,
  MCP_SESSIONS_SECTION_ID,
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
  hostedToolset,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
  /** The toolset a hosted server serves; it adds the hosted-only sections. */
  hostedToolset?: Toolset;
}): JSX.Element {
  const isUnproxied = !!mcpServer.unproxiedMcpServerId;
  // Only remote and tunneled sources add sections to the page; the unproxied
  // row feeds the danger zone alone, so a deep link never waits on it.
  const hasSourceSections =
    !!mcpServer.remoteMcpServerId || !!mcpServer.tunneledMcpServerId;
  const remoteMcpServerId = mcpServer.remoteMcpServerId;
  const { mcpUrl } = useResolvedMcpServerUrl(endpoints, isLoadingEndpoints);

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

  if (hostedToolset) {
    // Toolset columns sync onto the canonical server only.
    const canonical = isCanonicalHostedWrapper(mcpServer);
    return (
      <div className="mx-auto w-full max-w-[1270px] space-y-10 px-8 py-8">
        <GeneralSection mcpServer={mcpServer} />
        {canonical ? (
          <RequireScope
            scope="mcp:write"
            resourceId={hostedToolset.id}
            level="section"
          >
            <HostedAddressSections toolset={hostedToolset} />
            <NetworkAccessSection toolset={hostedToolset} />
            <div id={MCP_AUTHENTICATION_SECTION_ID}>
              <ToolsetOAuthSection toolset={hostedToolset} />
            </div>
            <div id={MCP_ENVIRONMENT_SECTION_ID}>
              <MCPAuthenticationTab toolset={hostedToolset} showOAuth={false} />
            </div>
          </RequireScope>
        ) : (
          <>
            <ServerUrlSection
              backend={{ mcpServerId: mcpServer.id }}
              endpoints={endpoints}
              isLoadingEndpoints={isLoadingEndpoints}
              mcpServer={mcpServer}
            />
            <NetworkAccessSection mcpServer={mcpServer} endpoints={endpoints} />
            <AuthenticationSection mcpServer={mcpServer} mcpUrl={mcpUrl} />
          </>
        )}
        <RequireScope
          scope="mcp:write"
          resourceId={hostedToolset.id}
          level="section"
        >
          <div id={MCP_PERFORMANCE_SECTION_ID}>
            <MCPPerformanceTab toolset={hostedToolset} />
          </div>
        </RequireScope>
        <PageSection
          heading="Server Instructions"
          description="Instructions returned to LLMs when they connect to your MCP server. Describe how your tools work together, required workflows, and any constraints."
        >
          <ServerInstructionsSection
            mcpServerId={mcpServer.id}
            writeResourceId={hostedToolset.id}
            name={mcpServer.name || hostedToolset.name}
            tools={hostedToolset.tools}
          />
        </PageSection>
        <ToolFilteringSection mcpServer={mcpServer} />
        {/* Export reads the toolset's canonical server only. */}
        {canonical && <ExportJsonSection mcpServer={mcpServer} />}
        <DangerZoneSection
          mcpServer={mcpServer}
          endpoints={endpoints}
          hostedToolset={canonical ? hostedToolset : undefined}
        />
      </div>
    );
  }

  if (remoteMcpServerId) {
    return (
      <div className="mx-auto w-full max-w-[1270px] space-y-10 px-8 py-8">
        {/* Held until the source settles: arriving later would remount the
            form around its Remote URL and drop anything already typed. */}
        {remoteMcpServer || remoteQuery.isError ? (
          <GeneralSection
            mcpServer={mcpServer}
            remoteMcpServer={remoteMcpServer}
          />
        ) : null}
        {/* Identity sits directly under General: it is the first decision a
            remote server needs. Upstream headers live inside its Custom
            Headers disclosure; they are governed by the identity choice, not
            a peer of it. */}
        <AuthenticationSection mcpServer={mcpServer} mcpUrl={mcpUrl} />
        <IdentityChainingSection mcpServer={mcpServer} />
        <ServerUrlSection
          backend={{ mcpServerId: mcpServer.id }}
          endpoints={endpoints}
          isLoadingEndpoints={isLoadingEndpoints}
          mcpServer={mcpServer}
        />
        <NetworkAccessSection mcpServer={mcpServer} endpoints={endpoints} />
        <RemoteMcpSessionsSection mcpServer={mcpServer} mcpUrl={mcpUrl} />
        <ToolFilteringSection mcpServer={mcpServer} />
        <DangerZoneSection
          mcpServer={mcpServer}
          endpoints={endpoints}
          deleteTarget={deleteTarget}
          sourceUnavailable={sourceUnavailable}
        />
      </div>
    );
  }

  // The source-backed sections are keyed by the source row so their drafts
  // remount when the route moves to a server backed by a different source;
  // this tree stays mounted across that navigation, and a draft or late save
  // result from the previous source must not land on the next one.
  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-10 px-8 py-8">
      <GeneralSection mcpServer={mcpServer} />
      {isUnproxied ? null : (
        <>
          <ServerUrlSection
            backend={{ mcpServerId: mcpServer.id }}
            endpoints={endpoints}
            isLoadingEndpoints={isLoadingEndpoints}
            mcpServer={mcpServer}
          />
          <NetworkAccessSection mcpServer={mcpServer} endpoints={endpoints} />
        </>
      )}
      <AuthenticationSection mcpServer={mcpServer} mcpUrl={mcpUrl} />
      {tunneledMcpServer ? (
        <Fragment key={tunneledMcpServer.id}>
          <ResourceIdentifierSection tunneledMcpServer={tunneledMcpServer} />
          <CallerIdentitySection />
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
