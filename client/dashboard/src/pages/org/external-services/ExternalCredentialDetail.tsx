import { DetailHero } from "@/components/detail-hero";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import { useGetGcpIamCredential } from "@gram/client/react-query/getGcpIamCredential";
import { Link, Navigate, useLocation, useParams } from "react-router";
import { providerFromSlug, providerLabel } from "./providers";
import {
  activeDetailTab,
  EXTERNAL_CREDENTIAL_TABS,
  type ExternalCredentialTab,
} from "./tabs";
import { OverviewTab } from "./tabs/OverviewTab";
import { SettingsTab } from "./tabs/SettingsTab";

export default function ExternalCredentialDetail(): JSX.Element {
  const { provider: providerParam = "", credentialId = "" } = useParams<{
    provider: string;
    credentialId: string;
  }>();
  const orgRoutes = useOrgRoutes();
  const location = useLocation();

  const provider = providerFromSlug(providerParam);

  // Only GCP has a detail page. A URL naming any other provider — a hand-edited
  // segment, or an AWS credential created through the API — must not fire a GCP
  // query whose 404 would read as "this credential does not exist".
  const isGcp = provider === "gcp_iam";

  const {
    data: credential,
    isLoading,
    isError,
  } = useGetGcpIamCredential({ id: credentialId }, undefined, {
    enabled: isGcp && credentialId !== "",
  });

  const activeTab = activeDetailTab(
    location.pathname,
    EXTERNAL_CREDENTIAL_TABS,
  );
  const tabHref = (tab: ExternalCredentialTab) =>
    orgRoutes.externalServices.credentialDetail[tab].href(
      providerParam,
      credentialId,
    );

  const label = credential?.name ?? "External Credential";

  if (!isGcp) {
    return <UnsupportedProvider provider={provider} />;
  }

  // The credential doesn't exist, failed to load, or the caller lacks the scope
  // (the endpoint 403s); return to the listing.
  if (isError || (!isLoading && !credential)) {
    return <Navigate to={orgRoutes.externalServices.href()} replace />;
  }

  // The bare /:provider/:credentialId URL has no tab; canonicalize to Overview.
  if (!activeTab) {
    return <Navigate to={tabHref("overview")} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            [credentialId]: label,
            [providerParam]: providerLabel("gcp_iam"),
          }}
          // "credentials" is a grouping segment with no page of its own, so a
          // crumb for it would link nowhere.
          skipSegments={["credentials"]}
        />
      </Page.Header>
      <Page.Body fullWidth noPadding className="gap-0">
        <DetailHero>
          <Text small muted>
            {credential
              ? providerLabel(credential.provider)
              : "External Credential"}
          </Text>
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
              {isLoading && <Text muted>Loading…</Text>}
            </TabsContent>
            <TabsContent value="settings" className="mt-0">
              {credential && (
                <RequireScope
                  scope="org:admin"
                  level="section"
                  fallback={
                    <Text muted>
                      Editing this credential requires an organization admin.
                    </Text>
                  }
                >
                  <SettingsTab key={credential.id} credential={credential} />
                </RequireScope>
              )}
              {isLoading && <Text muted>Loading…</Text>}
            </TabsContent>
          </div>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

// UnsupportedProvider explains a provider the dashboard cannot render yet,
// rather than redirecting silently — a stored AWS credential is a real record,
// and a bounce back to the list looks like the page lost it.
function UnsupportedProvider({
  provider,
}: {
  provider: string | undefined;
}): JSX.Element {
  const message = provider
    ? `${providerLabel(provider)} credentials cannot be managed in the dashboard yet.`
    : "This external service is not recognized.";

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Text muted className="py-8 text-center">
          {message}
        </Text>
      </Page.Body>
    </Page>
  );
}
