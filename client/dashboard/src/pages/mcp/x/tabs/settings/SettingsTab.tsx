import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useEffect } from "react";
import { useLocation } from "react-router";
import {
  MCP_AUTHENTICATION_SECTION_ID,
  AuthenticationSection,
} from "./sections/authentication/AuthenticationSection";
import { BrandingSection } from "./sections/BrandingSection";
import { DangerZoneSection } from "./sections/DangerZoneSection";
import { HeadersSection } from "./sections/HeadersSection";
import {
  MCP_SERVER_URL_SECTION_ID,
  ServerUrlSection,
} from "./sections/ServerUrlSection";
import { PublicRateLimitsSection } from "./sections/PublicRateLimitsSection";
import { RemoteMcpSessionsSection } from "./sections/RemoteMcpSessionsSection";
import { ToolFilteringSection } from "./sections/ToolFilteringSection";

function useScrollToSettingsHash() {
  const location = useLocation();

  useEffect(() => {
    const targetId = location.hash.replace("#", "");
    if (
      targetId !== MCP_SERVER_URL_SECTION_ID &&
      targetId !== MCP_AUTHENTICATION_SECTION_ID
    ) {
      return;
    }

    const animationFrame = window.requestAnimationFrame(() => {
      document
        .getElementById(targetId)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    });

    return () => window.cancelAnimationFrame(animationFrame);
  }, [location.hash]);
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
  useScrollToSettingsHash();

  const isUnproxied = !!mcpServer.unproxiedMcpServerId;
  const remoteMcpServerId = mcpServer.remoteMcpServerId;

  if (remoteMcpServerId) {
    return (
      <div className="mx-auto w-full max-w-[1270px] space-y-10 px-8 py-8">
        <BrandingSection
          mcpServer={mcpServer}
          title="Display"
          description="Customize how this Remote MCP server appears in the dashboard and on its installation page."
        />
        <AuthenticationSection mcpServer={mcpServer} />
        <HeadersSection
          remoteMcpServerId={remoteMcpServerId}
          context={{ kind: "mcp-server" }}
          identityManagement={{
            userSessionIssuerId: mcpServer.userSessionIssuerId,
            resourceId: mcpServer.id,
          }}
        />
        <ServerUrlSection
          backend={{ mcpServerId: mcpServer.id }}
          endpoints={endpoints}
          isLoadingEndpoints={isLoadingEndpoints}
        />
        <RemoteMcpSessionsSection mcpServer={mcpServer} />
        <ToolFilteringSection mcpServer={mcpServer} />
        <DangerZoneSection mcpServer={mcpServer} endpoints={endpoints} />
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-10 px-8 py-8">
      <BrandingSection mcpServer={mcpServer} />
      {isUnproxied ? null : (
        <ServerUrlSection
          backend={{ mcpServerId: mcpServer.id }}
          endpoints={endpoints}
          isLoadingEndpoints={isLoadingEndpoints}
        />
      )}
      <AuthenticationSection mcpServer={mcpServer} />
      {mcpServer.tunneledMcpServerId ? (
        <PublicRateLimitsSection
          tunneledMcpServerId={mcpServer.tunneledMcpServerId}
          projectId={mcpServer.projectId}
        />
      ) : null}
      {isUnproxied ? null : <ToolFilteringSection mcpServer={mcpServer} />}
      <DangerZoneSection mcpServer={mcpServer} endpoints={endpoints} />
    </div>
  );
}
