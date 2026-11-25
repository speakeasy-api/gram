import { useGatewayDetailChrome } from "@/components/mcp/useGatewayDetailChrome";
import { McpDetailFrame } from "@/components/mcp/McpDetailFrame";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { ClientsAndSessionsTab } from "@/components/sessions/ClientsAndSessionsTab";
import { useTabScrollReset } from "@/hooks/useTabScrollReset";
import { isNotNotFound } from "@/lib/query-errors";
import { isNotFoundError } from "@/lib/route-errors";
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
import { GatewayOverviewTab } from "./GatewayOverviewTab";
import { GatewaySettingsTab } from "./GatewaySettingsTab";
import { GatewaySkillsTab } from "./GatewaySkillsTab";

/**
 * Reads the gateway before the detail chrome mounts: a deleted or unknown id
 * answers 404, which gets its own page here instead of the error boundary.
 */
export default function GatewayDetails(): JSX.Element {
  const { gatewayId } = useParams<{ gatewayId: string }>();
  const id = gatewayId ?? "";
  const { data, error } = useGetMetaMcpServer({ id }, undefined, {
    enabled: id !== "",
    throwOnError: isNotNotFound,
  });
  if (isNotFoundError(error)) return <GatewayNotFound id={id} />;
  // The chrome reads the same query without the not-found guard, so it
  // mounts only once the gateway is known to exist.
  if (id !== "" && !data && !error) {
    return (
      <McpDetailFrame
        chrome={null}
        breadcrumbs={{
          substitutions: { [id]: "Gateway" },
          skipSegments: ["gateway", ...GATEWAY_TAB_URLS],
        }}
      >
        {null}
      </McpDetailFrame>
    );
  }
  return <GatewayDetailsContent />;
}

function GatewayNotFound({ id }: { id: string }): JSX.Element {
  const routes = useRoutes();
  return (
    <Page>
      <Page.Header>
        {/* The missing gateway's id is not a name: leave it, and the tab, out. */}
        <Page.Header.Breadcrumbs
          substitutions={{ [id]: "Gateway not found" }}
          skipSegments={["gateway", ...GATEWAY_TAB_URLS]}
        />
      </Page.Header>
      <Page.Body>
        <RouteNotFoundState
          title="Gateway not found"
          description="It may have been deleted."
          action={
            <routes.mcp.Link>
              <SecondaryRouteAction>Back to MCP</SecondaryRouteAction>
            </routes.mcp.Link>
          }
        />
      </Page.Body>
    </Page>
  );
}

function GatewayDetailsContent(): JSX.Element {
  const { gatewayId } = useParams<{ gatewayId: string }>();
  const location = useLocation();
  const routes = useRoutes();
  const id = gatewayId ?? "";
  const activeTab = activeTabFromPath(location.pathname, id);
  const tabContentRef = useTabScrollReset(activeTab);
  const chrome = useGatewayDetailChrome();

  const {
    data: metaMcpServer,
    isLoading,
    isError,
  } = useGetMetaMcpServer({ id }, undefined, {
    enabled: id !== "",
    throwOnError: isNotNotFound,
  });

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
      case "inspect":
        return (
          <GatewayInspectTab
            metaMcpServer={metaMcpServer}
            endpoints={endpoints}
            isLoadingEndpoints={isLoadingEndpoints}
          />
        );
      case "skills":
        return <GatewaySkillsTab metaMcpServer={metaMcpServer} />;
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
              <MCPTeamAccessTab
                resourceId={metaMcpServer.id}
                serverName={metaMcpServer.name}
                checkAccess={false}
              />
            </RequireScope>
          </RequireScope>
        );
      case "sessions":
        return (
          <RequireScope scope="project:read" level="page">
            <ClientsAndSessionsTab
              gateway={{ id: metaMcpServer.id }}
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
    <McpDetailFrame
      chrome={chrome}
      breadcrumbs={{
        substitutions: { [id]: metaMcpServer?.name || "Gateway" },
        skipSegments: ["gateway", ...GATEWAY_TAB_URLS],
      }}
      contentRef={tabContentRef}
    >
      {renderTabContent()}
    </McpDetailFrame>
  );
}
