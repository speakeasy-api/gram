import { SETTINGS_GROUPS } from "@/components/detail/settings-groups";
import { useHideInsightsDock } from "@/components/insights-context";
import { mcpServerRouteParam } from "@/lib/sources";
import {
  platformEndpointSlug,
  useResolvedMcpServerUrl,
} from "@/hooks/useToolsetUrl";
import { useRoutes } from "@/routes";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { mcpServerTabHref } from "../MCPServerDetailsRouting";
import { RemoteMcpToolsSection } from "./RemoteMcpToolsSection";
import { MCP_AUTHENTICATION_SECTION_ID } from "./settings/sections/authentication/AuthenticationSection";

type InspectTabProps = {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
};

// Every server that serves MCP through Speakeasy has this tab: remote, tunneled
// and hosted alike. Unproxied servers never reach it: MCPServerDetails
// redirects their /inspect route back to /overview, since there's no reliable
// way to discover a vendor's tools without live auth.
export function InspectTab({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
}: InspectTabProps): JSX.Element {
  // The Chat tab hosts its own chat runtime, so the floating assistant (and
  // its shared runtime) stays out of this page: the two cannot nest.
  useHideInsightsDock();
  const routes = useRoutes();
  const { mcpUrl, loading } = useResolvedMcpServerUrl(
    endpoints,
    isLoadingEndpoints,
  );
  const param = mcpServerRouteParam(mcpServer);

  // Deep-link to the Authentication section under Settings so the tools view
  // can point users at auth setup when a server has none configured yet.
  const authSettingsHref = `${routes.mcp.details.settings.href(
    param,
  )}#${MCP_AUTHENTICATION_SECTION_ID}`;
  // Availability sits at the top of Settings' General area.
  const availabilityHref = `${routes.mcp.details.settings.href(
    param,
  )}#${SETTINGS_GROUPS.general.id}`;

  return (
    <div className="flex min-h-0 w-full flex-1 flex-col">
      <RemoteMcpToolsSection
        mcpUrl={mcpUrl}
        isResolvingUrl={loading}
        mcpServerId={mcpServer.id}
        userSessionIssuerId={mcpServer.userSessionIssuerId}
        remoteMcpServerId={mcpServer.remoteMcpServerId ?? undefined}
        isDisabled={mcpServer.visibility === "disabled"}
        availabilityHref={availabilityHref}
        authSettingsHref={authSettingsHref}
        accessHref={mcpServerTabHref(routes, param, "team-access")}
        platformSlug={platformEndpointSlug(endpoints)}
        tunneledMcpServerId={mcpServer.tunneledMcpServerId}
        visibility={mcpServer.visibility}
      />
    </div>
  );
}
