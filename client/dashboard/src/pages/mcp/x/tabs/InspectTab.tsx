import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { useRoutes } from "@/routes";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { RemoteMcpToolsSection } from "./RemoteMcpToolsSection";
import { MCP_AUTHENTICATION_SECTION_ID } from "./settings/sections/authentication/AuthenticationSection";

type InspectTabProps = {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
};

export function InspectTab({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
}: InspectTabProps): JSX.Element {
  const routes = useRoutes();
  const { mcpUrl, loading } = useResolvedMcpServerUrl(
    endpoints,
    isLoadingEndpoints,
  );

  // Deep-link to the Authentication section under Settings so the tools view
  // can point users at auth setup when a server has none configured yet.
  const authSettingsHref = `${routes.mcp.x.settings.href(
    mcpServer.slug ?? mcpServer.id,
  )}#${MCP_AUTHENTICATION_SECTION_ID}`;

  return (
    <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
      <RemoteMcpToolsSection
        mcpUrl={mcpUrl}
        isResolvingUrl={loading}
        mcpServerId={mcpServer.id}
        userSessionIssuerId={mcpServer.userSessionIssuerId}
        isDisabled={mcpServer.visibility === "disabled"}
        authSettingsHref={authSettingsHref}
      />
    </div>
  );
}
