import { Toolset } from "@/lib/toolTypes";
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
import { ExportSection } from "./sections/ExportSection";
import { HeadersSection } from "./sections/HeadersSection";
import { InstructionsSection } from "./sections/InstructionsSection";
import { PublishingSection } from "./sections/PublishingSection";
import {
  MCP_SERVER_URL_SECTION_ID,
  ServerUrlSection,
} from "./sections/ServerUrlSection";
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
  backingToolset,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
  /**
   * The backing toolset of a toolset-backed server. Toolset-owned settings
   * (instructions generation, tool filtering, export, delete cascade) key off
   * it; the user-session Authentication section is skipped because toolset
   * OAuth lives on the Authentication tab.
   */
  backingToolset?: Toolset;
}): JSX.Element {
  useScrollToSettingsHash();

  const isToolsetBacked = mcpServer.backendKind === "toolset";

  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-10 px-8 py-8">
      <BrandingSection mcpServer={mcpServer} />
      {isToolsetBacked && (
        <InstructionsSection mcpServer={mcpServer} toolset={backingToolset} />
      )}
      <ServerUrlSection
        mcpServer={mcpServer}
        endpoints={endpoints}
        isLoadingEndpoints={isLoadingEndpoints}
      />
      {!isToolsetBacked && <AuthenticationSection mcpServer={mcpServer} />}
      {mcpServer.remoteMcpServerId ? (
        <HeadersSection
          remoteMcpServerId={mcpServer.remoteMcpServerId}
          context={{ kind: "mcp-server" }}
        />
      ) : null}
      <ToolFilteringSection
        mcpServer={mcpServer}
        backingToolset={backingToolset}
      />
      <PublishingSection mcpServer={mcpServer} endpoints={endpoints} />
      {isToolsetBacked && (
        <ExportSection mcpServer={mcpServer} endpoints={endpoints} />
      )}
      <DangerZoneSection
        mcpServer={mcpServer}
        endpoints={endpoints}
        backingToolset={backingToolset}
      />
    </div>
  );
}
