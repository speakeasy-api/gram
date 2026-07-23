import { DetailHero } from "@/components/detail-hero";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useOrgRoutes } from "@/routes";
import { useOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { useOrganizationRemoteSessionIssuer } from "@gram/client/react-query/organizationRemoteSessionIssuer.js";
import { Link, Navigate, useLocation, useParams } from "react-router";
import { ScopeBadge } from "./ScopeBadge";
import { remoteSessionClientDisplayName } from "./clientDisplay";
import { issuerDisplayName } from "./issuerDisplay";
import { OverviewTab } from "./tabs/client/OverviewTab";
import { McpServersTab } from "./tabs/client/McpServersTab";
import { SessionsTab } from "./tabs/client/SessionsTab";
import { SettingsTab } from "./tabs/client/SettingsTab";
import { activeDetailTab, CLIENT_TABS, type ClientTab } from "./tabs";

// Maps a client tab value to its route subpage key (the MCP Servers tab's URL
// segment is "mcp-servers" but its route key is camelCase "mcpServers").
const CLIENT_TAB_ROUTE_KEY: Record<
  ClientTab,
  "overview" | "mcpServers" | "sessions" | "settings"
> = {
  overview: "overview",
  "mcp-servers": "mcpServers",
  sessions: "sessions",
  settings: "settings",
};

export default function RemoteSessionClientDetail(): JSX.Element {
  const { issuerId = "", clientId = "" } = useParams<{
    issuerId: string;
    clientId: string;
  }>();
  const orgRoutes = useOrgRoutes();
  const location = useLocation();
  const {
    data: client,
    isLoading: isClientLoading,
    isError: isClientError,
  } = useOrganizationRemoteSessionClient({
    id: clientId,
  });
  const { data: issuer } = useOrganizationRemoteSessionIssuer({ id: issuerId });

  const activeTab = activeDetailTab(location.pathname, CLIENT_TABS);
  const tabHref = (tab: ClientTab) =>
    orgRoutes.remoteIdentityProviders.clientDetail[
      CLIENT_TAB_ROUTE_KEY[tab]
    ].href(issuerId, clientId);

  const label = client
    ? remoteSessionClientDisplayName(client)
    : "Remote Session Client";
  // Mirror the issuer's own breadcrumb: its display name (name, falling back to
  // the issuer URL), and the raw id only while the issuer is still loading.
  const issuerLabel = issuer ? issuerDisplayName(issuer) : issuerId;

  // The client doesn't exist (or failed to load); return to the issuer's
  // Clients tab.
  if (isClientError || (!isClientLoading && !client)) {
    return (
      <Navigate
        to={orgRoutes.remoteIdentityProviders.issuerDetail.clients.href(
          issuerId,
        )}
        replace
      />
    );
  }

  // The bare /:clientId URL has no tab; canonicalize to the Overview tab.
  if (!activeTab) {
    return <Navigate to={tabHref("overview")} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [issuerId]: issuerLabel, [clientId]: label }}
        />
      </Page.Header>
      <Page.Body fullWidth noPadding className="gap-0">
        <DetailHero>
          <div className="flex items-center gap-3">
            <Type small muted>
              Remote Session Client
            </Type>
            {client && (
              <ScopeBadge
                projectId={client.projectId}
                organizationId={client.organizationId}
              />
            )}
          </div>
          <Heading variant="h1" className="break-all normal-case">
            {label}
          </Heading>
        </DetailHero>

        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <Tabs value={activeTab} className="flex w-full flex-1 flex-col">
            <div className="shrink-0 border-b">
              <div className="mx-auto max-w-[1270px] px-8">
                <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                  <PageTabsTrigger value="overview" asChild>
                    <Link to={tabHref("overview")}>Overview</Link>
                  </PageTabsTrigger>
                  <PageTabsTrigger value="mcp-servers" asChild>
                    <Link to={tabHref("mcp-servers")}>MCP Servers</Link>
                  </PageTabsTrigger>
                  <PageTabsTrigger value="sessions" asChild>
                    <Link to={tabHref("sessions")}>Sessions</Link>
                  </PageTabsTrigger>
                  <PageTabsTrigger value="settings" asChild>
                    <Link to={tabHref("settings")}>Settings</Link>
                  </PageTabsTrigger>
                </TabsList>
              </div>
            </div>

            <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
              <TabsContent value="overview" className="mt-0">
                {client && <OverviewTab client={client} />}
              </TabsContent>
              <TabsContent value="mcp-servers" className="mt-0">
                <McpServersTab clientId={clientId} />
              </TabsContent>
              <TabsContent value="sessions" className="mt-0">
                <SessionsTab clientId={clientId} />
              </TabsContent>
              <TabsContent value="settings" className="mt-0">
                {client && (
                  <SettingsTab
                    key={client.id}
                    client={client}
                    issuerId={issuerId}
                  />
                )}
              </TabsContent>
            </div>
          </Tabs>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
