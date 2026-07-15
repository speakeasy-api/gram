import { DetailLayout } from "@/components/layouts/detail-layout";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useOrgRoutes } from "@/routes";
import { useOrganizationRemoteSessionIssuer } from "@gram/client/react-query/organizationRemoteSessionIssuer.js";
import { Link, Navigate, useLocation, useParams } from "react-router";
import { ScopeBadge } from "./ScopeBadge";
import { issuerDisplayName } from "./issuerDisplay";
import { ClientsTab } from "./tabs/issuer/ClientsTab";
import { OverviewTab } from "./tabs/issuer/OverviewTab";
import { SettingsTab } from "./tabs/issuer/SettingsTab";
import { activeDetailTab, ISSUER_TABS, type IssuerTab } from "./tabs";

export default function RemoteIdentityProviderDetail(): JSX.Element {
  const { issuerId = "" } = useParams<{ issuerId: string }>();
  const orgRoutes = useOrgRoutes();
  const location = useLocation();
  const {
    data: issuer,
    isLoading,
    isError,
  } = useOrganizationRemoteSessionIssuer({
    id: issuerId,
  });

  const activeTab = activeDetailTab(location.pathname, ISSUER_TABS);
  const tabHref = (tab: IssuerTab) =>
    orgRoutes.remoteIdentityProviders.issuerDetail[tab].href(issuerId);

  const label = issuer ? issuerDisplayName(issuer) : "Remote Identity Provider";

  // The issuer doesn't exist (or failed to load); return to the listing.
  if (isError || (!isLoading && !issuer)) {
    return <Navigate to={orgRoutes.remoteIdentityProviders.href()} replace />;
  }

  // The bare /:issuerId URL has no tab; canonicalize to the Overview tab.
  if (!activeTab) {
    return <Navigate to={tabHref("overview")} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs substitutions={{ [issuerId]: label }} />
      </Page.Header>
      <Page.Body>
        <DetailLayout>
          <DetailLayout.Header
            eyebrow="Remote Identity Provider"
            title={
              <span className="inline-flex items-center gap-3 break-all">
                {label}
                {issuer && (
                  <ScopeBadge projectScoped={Boolean(issuer.projectId)} />
                )}
              </span>
            }
          />

          <RequireScope scope={["org:read", "org:admin"]} level="page">
            <Tabs value={activeTab} className="flex w-full flex-1 flex-col">
              <DetailLayout.Tabs>
                <TabsList className="h-auto gap-6 bg-transparent p-0">
                  <PageTabsTrigger value="overview" asChild>
                    <Link to={tabHref("overview")}>Overview</Link>
                  </PageTabsTrigger>
                  <PageTabsTrigger value="clients" asChild>
                    <Link to={tabHref("clients")}>Clients</Link>
                  </PageTabsTrigger>
                  <PageTabsTrigger value="settings" asChild>
                    <Link to={tabHref("settings")}>Settings</Link>
                  </PageTabsTrigger>
                </TabsList>
              </DetailLayout.Tabs>

              <DetailLayout.Content>
                <DetailLayout.Main>
                  <TabsContent value="overview" className="mt-0">
                    {issuer && <OverviewTab issuer={issuer} />}
                  </TabsContent>
                  <TabsContent value="clients" className="mt-0">
                    {issuer && <ClientsTab issuer={issuer} />}
                    {isLoading && <Type muted>Loading…</Type>}
                  </TabsContent>
                  <TabsContent value="settings" className="mt-0">
                    {issuer && <SettingsTab key={issuer.id} issuer={issuer} />}
                    {isLoading && <Type muted>Loading…</Type>}
                  </TabsContent>
                </DetailLayout.Main>
              </DetailLayout.Content>
            </Tabs>
          </RequireScope>
        </DetailLayout>
      </Page.Body>
    </Page>
  );
}
