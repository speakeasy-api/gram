import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ClientsAndSessionsTab } from "@/components/sessions/ClientsAndSessionsTab";
import { useRoutes } from "@/routes";
import { useGetMetaMcpServer } from "@gram/client/react-query/getMetaMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { Navigate, useLocation, useParams } from "react-router";
import {
  activeTabFromPath,
  gatewayTabHref,
  GATEWAY_TAB_URLS,
  initialTabFromHash,
} from "./GatewayDetailsRouting";
import { MCPTeamAccessTab } from "@/pages/mcp/MCPTeamAccessTab";
import { GatewayInspectTab } from "./GatewayInspectTab";
import { GatewayMembersTab } from "./GatewayMembersTab";
import { GatewayOverviewTab } from "./GatewayOverviewTab";
import { GatewaySettingsTab } from "./GatewaySettingsTab";

export default function GatewayDetails(): JSX.Element {
  const { gatewayId } = useParams<{ gatewayId: string }>();
  const location = useLocation();
  const routes = useRoutes();
  const id = gatewayId ?? "";
  const activeTab = activeTabFromPath(location.pathname, id);

  const {
    data: metaMcpServer,
    isLoading,
    isError,
  } = useGetMetaMcpServer({ id }, undefined, { enabled: id !== "" });

  const { data: endpointsResult, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({ metaMcpServerId: id }, undefined, {
      enabled: id !== "",
    });
  const endpoints = endpointsResult?.mcpEndpoints ?? [];

  if (!id) {
    return <Navigate to={routes.mcp.href()} replace />;
  }
  if (isError || (!isLoading && !metaMcpServer)) {
    return <Navigate to={routes.mcp.href()} replace />;
  }
  if (!activeTab) {
    return (
      <Navigate
        to={gatewayTabHref(routes, id, initialTabFromHash(location.hash))}
        replace
      />
    );
  }

  const renderTabContent = () => {
    if (!metaMcpServer) return null;
    switch (activeTab) {
      case "overview":
        return (
          <GatewayOverviewTab
            metaMcpServer={metaMcpServer}
            endpoints={endpoints}
            isLoadingEndpoints={isLoadingEndpoints}
          />
        );
      case "members":
        return (
          <RequireScope scope="mcp:read" level="page">
            <GatewayMembersTab metaMcpServer={metaMcpServer} />
          </RequireScope>
        );
      case "inspect":
        return (
          <GatewayInspectTab
            metaMcpServer={metaMcpServer}
            endpoints={endpoints}
            isLoadingEndpoints={isLoadingEndpoints}
          />
        );
      case "team-access":
        return (
          <RequireScope scope="org:read" level="page">
            <RequireScope
              scope="mcp:read"
              resourceId={metaMcpServer.id}
              level="page"
            >
              {/* Gateways grant under the same `mcp:*` scope kind as every
                  other MCP resource (selector.go), so the tab is reused with
                  the gateway id. No `tools` prop: a gateway exposes the fixed
                  meta-tools, not a per-tool catalog. */}
              <MCPTeamAccessTab resourceId={metaMcpServer.id} />
            </RequireScope>
          </RequireScope>
        );
      case "sessions":
        return (
          <RequireScope scope="project:read" level="page">
            <ClientsAndSessionsTab
              issuerId={metaMcpServer.userSessionIssuerId}
            />
          </RequireScope>
        );
      case "settings":
        return (
          <GatewaySettingsTab
            metaMcpServer={metaMcpServer}
            endpoints={endpoints}
            isLoadingEndpoints={isLoadingEndpoints}
          />
        );
    }
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [id]: metaMcpServer?.name || "Gateway" }}
          skipSegments={["gateway", ...GATEWAY_TAB_URLS]}
        />
      </Page.Header>

      <Page.Body fullWidth className="gap-0">
        <div className="mx-auto w-full max-w-[1270px] flex-1">
          {renderTabContent()}
        </div>
      </Page.Body>
    </Page>
  );
}
