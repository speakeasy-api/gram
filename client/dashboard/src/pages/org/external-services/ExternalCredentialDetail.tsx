import { DetailHero } from "@/components/detail-hero";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  PageTabsList,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrgRoutes } from "@/routes";
import { Scope } from "@gram/client/models/components/rolegrant.js";
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

const ORG_READ_SCOPES: Scope[] = ["org:read", "org:admin"];

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

  // The read scope gates the fetch, not just the rendering: without it the
  // request would 403 and the not-found handling below would bounce the caller
  // back to the list, which reads as "this credential is gone" rather than "you
  // cannot see it".
  const { hasAnyScope, isLoading: rbacLoading } = useRBAC();
  const canRead = hasAnyScope(ORG_READ_SCOPES);

  const {
    data: credential,
    isLoading,
    isError,
  } = useGetGcpIamCredential({ id: credentialId }, undefined, {
    enabled: isGcp && credentialId !== "" && canRead,
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

  // Resolve access before any not-found handling. With the fetch disabled the
  // absence of a credential says nothing about whether it exists, so the checks
  // below would misreport it. RequireScope renders nothing while grants load and
  // the unauthorized panel once they deny.
  if (rbacLoading || !canRead) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs skipSegments={["credentials"]} />
        </Page.Header>
        <Page.Body>
          <RequireScope scope={ORG_READ_SCOPES} level="page">
            <Text muted>Loading…</Text>
          </RequireScope>
        </Page.Body>
      </Page>
    );
  }

  // The credential doesn't exist or failed to load; return to the listing.
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
          <Page.Eyebrow />
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
              <PageTabsList className="h-auto gap-6 bg-transparent p-0">
                <PageTabsTrigger value="overview" asChild>
                  <Link to={tabHref("overview")}>Overview</Link>
                </PageTabsTrigger>
                <PageTabsTrigger value="settings" asChild>
                  <Link to={tabHref("settings")}>Settings</Link>
                </PageTabsTrigger>
              </PageTabsList>
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
