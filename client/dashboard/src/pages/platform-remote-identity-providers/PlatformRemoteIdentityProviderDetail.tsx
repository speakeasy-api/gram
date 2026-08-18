import { DetailHero } from "@/components/detail-hero";
import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/Heading";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  PageTabsList,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import type { GlobalRemoteSessionIssuer } from "@gram/client/models/components/globalremotesessionissuer.js";
import { useGlobalRemoteSessionIssuer } from "@gram/client/react-query/globalRemoteSessionIssuer.js";
import { Link, Navigate, useLocation, useParams } from "react-router";
import { ScopeBadge } from "../remote-identity-providers/ScopeBadge";
import { issuerDisplayName } from "../remote-identity-providers/issuerDisplay";
import { activeDetailTab } from "@/lib/detail-tabs";
import { OverviewTab } from "../remote-identity-providers/tabs/issuer/OverviewTab";
import { PlatformAdminOnly } from "./PlatformAdminOnly";
import { PlatformConvergenceTab } from "./PlatformConvergenceTab";
import { PlatformSettingsTab } from "./PlatformSettingsTab";

// The catalog detail has no Clients tab. Global remote_session_clients exist in
// the schema but nothing can reach them at runtime — the resolver matches a
// client to a session by project or organization, and a global client has
// neither — so there is nothing to manage them for yet. Tenant clients on a
// platform issuer belong to their organizations and are managed there.
const PLATFORM_ISSUER_TABS = ["overview", "convergence", "settings"] as const;
type PlatformIssuerTab = (typeof PLATFORM_ISSUER_TABS)[number];

export default function PlatformRemoteIdentityProviderDetail(): JSX.Element {
  const { issuerId = "" } = useParams<{ issuerId: string }>();
  const isPlatformAdmin = useIsPlatformAdmin();
  const { data, isLoading, isError } = useGlobalRemoteSessionIssuer(
    { id: issuerId },
    undefined,
    // The endpoint refuses non-admins; PlatformAdminOnly below blocks the
    // render, and this keeps the request from being made at all.
    { enabled: isPlatformAdmin },
  );

  const label = data
    ? issuerDisplayName(data.issuer)
    : "Platform Remote Identity Provider";

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs substitutions={{ [issuerId]: label }} />
      </Page.Header>
      <Page.Body fullWidth noPadding className="gap-0">
        <PlatformAdminOnly feature="Platform Remote Identity Providers">
          <PlatformIssuerDetail
            issuerId={issuerId}
            issuer={data}
            isLoading={isLoading}
            isError={isError}
          />
        </PlatformAdminOnly>
      </Page.Body>
    </Page>
  );
}

function PlatformIssuerDetail({
  issuerId,
  issuer: data,
  isLoading,
  isError,
}: {
  issuerId: string;
  issuer: GlobalRemoteSessionIssuer | undefined;
  isLoading: boolean;
  isError: boolean;
}) {
  const orgRoutes = useOrgRoutes();
  const location = useLocation();

  const issuer = data?.issuer;
  const activeTab = activeDetailTab(location.pathname, PLATFORM_ISSUER_TABS);
  const tabHref = (tab: PlatformIssuerTab) =>
    orgRoutes.platformRemoteIdentityProviders.issuerDetail[tab].href(issuerId);

  // The issuer doesn't exist (or failed to load); return to the catalog.
  if (isError || (!isLoading && !issuer)) {
    return (
      <Navigate to={orgRoutes.platformRemoteIdentityProviders.href()} replace />
    );
  }

  // The bare /:issuerId URL has no tab; canonicalize to the Overview tab.
  if (!activeTab) {
    return <Navigate to={tabHref("overview")} replace />;
  }

  return (
    <>
      <DetailHero>
        <Page.Eyebrow />
        <div className="flex items-center gap-3">
          <Text small muted>
            Remote Identity Provider
          </Text>
          {issuer && (
            <ScopeBadge
              projectId={issuer.projectId}
              organizationId={issuer.organizationId}
            />
          )}
        </div>
        <Heading variant="h1" className="break-all normal-case">
          {issuer
            ? issuerDisplayName(issuer)
            : "Platform Remote Identity Provider"}
        </Heading>
      </DetailHero>

      <Tabs value={activeTab} className="flex w-full flex-1 flex-col">
        <div className="shrink-0 border-b">
          <div className="mx-auto max-w-[1270px] px-8">
            <PageTabsList className="h-auto gap-6 bg-transparent p-0">
              <PageTabsTrigger value="overview" asChild>
                <Link to={tabHref("overview")}>Overview</Link>
              </PageTabsTrigger>
              <PageTabsTrigger value="convergence" asChild>
                <Link to={tabHref("convergence")}>Convergence</Link>
              </PageTabsTrigger>
              <PageTabsTrigger value="settings" asChild>
                <Link to={tabHref("settings")}>Settings</Link>
              </PageTabsTrigger>
            </PageTabsList>
          </div>
        </div>

        <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
          <TabsContent value="overview" className="mt-0">
            {issuer && <OverviewTab issuer={issuer} />}
            {isLoading && <Text muted>Loading…</Text>}
          </TabsContent>
          <TabsContent value="convergence" className="mt-0">
            {issuer && <PlatformConvergenceTab issuer={issuer} />}
            {isLoading && <Text muted>Loading…</Text>}
          </TabsContent>
          <TabsContent value="settings" className="mt-0">
            {data && (
              <PlatformSettingsTab
                key={data.issuer.id}
                issuer={data.issuer}
                globalClientCount={data.globalClientCount}
                tenantClientCount={data.tenantClientCount}
              />
            )}
            {isLoading && <Text muted>Loading…</Text>}
          </TabsContent>
        </div>
      </Tabs>
    </>
  );
}
