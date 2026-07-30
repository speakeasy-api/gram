import { DetailHero } from "@/components/detail-hero";
import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/Heading";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/Tabs";
import { Type } from "@/components/ui/Type";
import { useOrgRoutes } from "@/routes";
import { useGetGcpIamPlatformCredential } from "@gram/client/react-query/getGcpIamPlatformCredential";
import { Link, Navigate, useLocation, useParams } from "react-router";
import { providerLabel } from "./providers";
import {
  activeDetailTab,
  EXTERNAL_CREDENTIAL_TABS,
  type ExternalCredentialTab,
} from "./tabs";
import { OverviewTab } from "./tabs/OverviewTab";
import { SettingsTab } from "./tabs/SettingsTab";

export default function ExternalCredentialDetail(): JSX.Element {
  const { credentialId = "" } = useParams<{ credentialId: string }>();
  const orgRoutes = useOrgRoutes();
  const location = useLocation();
  const {
    data: credential,
    isLoading,
    isError,
  } = useGetGcpIamPlatformCredential({ id: credentialId });

  const activeTab = activeDetailTab(
    location.pathname,
    EXTERNAL_CREDENTIAL_TABS,
  );
  const tabHref = (tab: ExternalCredentialTab) =>
    orgRoutes.externalServices.credentialDetail[tab].href(credentialId);

  const label = credential?.name ?? "External Credential";

  // The credential doesn't exist, failed to load, or the caller isn't a platform
  // admin (the endpoint 403s); return to the listing.
  if (isError || (!isLoading && !credential)) {
    return <Navigate to={orgRoutes.externalServices.href()} replace />;
  }

  // The bare /:credentialId URL has no tab; canonicalize to the Overview tab.
  if (!activeTab) {
    return <Navigate to={tabHref("overview")} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs substitutions={{ [credentialId]: label }} />
      </Page.Header>
      <Page.Body fullWidth noPadding className="gap-0">
        <DetailHero>
          <Type small muted>
            {credential
              ? providerLabel(credential.provider)
              : "External Credential"}
          </Type>
          <Heading variant="h1" className="break-all normal-case">
            {label}
          </Heading>
        </DetailHero>

        <Tabs value={activeTab} className="flex w-full flex-1 flex-col">
          <div className="shrink-0 border-b">
            <div className="mx-auto max-w-[1270px] px-8">
              <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                <PageTabsTrigger value="overview" asChild>
                  <Link to={tabHref("overview")}>Overview</Link>
                </PageTabsTrigger>
                <PageTabsTrigger value="settings" asChild>
                  <Link to={tabHref("settings")}>Settings</Link>
                </PageTabsTrigger>
              </TabsList>
            </div>
          </div>

          <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
            <TabsContent value="overview" className="mt-0">
              {credential && <OverviewTab credential={credential} />}
              {isLoading && <Type muted>Loading…</Type>}
            </TabsContent>
            <TabsContent value="settings" className="mt-0">
              {credential && (
                <SettingsTab key={credential.id} credential={credential} />
              )}
              {isLoading && <Type muted>Loading…</Type>}
            </TabsContent>
          </div>
        </Tabs>
      </Page.Body>
    </Page>
  );
}
