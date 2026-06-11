import type { McpEndpoint, McpServer } from "@gram/client/models/components";
import { useEffect } from "react";
import { useLocation } from "react-router";
import {
  AuthenticationSection,
  MCP_AUTHENTICATION_SECTION_ID,
} from "./sections/authentication/AuthenticationSection";
import { BrandingSection } from "./sections/BrandingSection";
import { DangerZoneSection } from "./sections/DangerZoneSection";
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
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
}): JSX.Element {
  useScrollToSettingsHash();

  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-10 px-8 py-8">
      <BrandingSection mcpServer={mcpServer} />
      <ServerUrlSection
        mcpServer={mcpServer}
        endpoints={endpoints}
        isLoadingEndpoints={isLoadingEndpoints}
      />
      <AuthenticationSection mcpServer={mcpServer} />
      <ToolFilteringSection mcpServer={mcpServer} />
      <PublishingSection mcpServer={mcpServer} endpoints={endpoints} />
      <DangerZoneSection mcpServer={mcpServer} endpoints={endpoints} />
    </div>
  );
}
