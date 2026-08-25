import { DetailHero } from "@/components/detail-hero";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { activeDetailTab } from "@/lib/detail-tabs";
import { useOrgRoutes } from "@/routes";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { useGetGcpKmsKey } from "@gram/client/react-query/getGcpKmsKey";
import { isNotNotFound } from "@/lib/query-errors";
import { Link, Navigate, useLocation, useParams } from "react-router";
import { providerFromSlug, providerLabel } from "./providers";
import { EXTERNAL_KEY_TABS, type ExternalKeyTab } from "./tabs";
import { OverviewTab } from "./tabs/OverviewTab";
import { SettingsTab } from "./tabs/SettingsTab";
import { SigningKeysTab } from "./tabs/SigningKeysTab";

const ORG_READ_SCOPES: Scope[] = ["org:read", "org:admin"];

// Route keys are camelCase while tab segments are kebab-case, so the tab id has
// to be translated before indexing the route helpers.
const EXTERNAL_KEY_TAB_ROUTE_KEY = {
  overview: "overview",
  "signing-keys": "signingKeys",
  settings: "settings",
} as const satisfies Record<ExternalKeyTab, string>;

export default function ExternalKeyDetail(): JSX.Element {
  const { provider: providerParam = "", keyId = "" } = useParams<{
    provider: string;
    keyId: string;
  }>();
  const orgRoutes = useOrgRoutes();
  const location = useLocation();

  const provider = providerFromSlug(providerParam);

  // Only GCP has a detail page. A URL naming any other provider — a hand-edited
  // segment, or an AWS key created through the API — must not fire a GCP query
  // whose 404 would read as "this key does not exist".
  const isGcp = provider === "gcp_kms";

  // The read scope gates the fetch, not just the rendering: without it the
  // request would 403 and the not-found handling below would bounce the caller
  // back to the list, which reads as "this key is gone" rather than "you cannot
  // see it".
  const { hasAnyScope, isLoading: rbacLoading } = useRBAC();
  const canRead = hasAnyScope(ORG_READ_SCOPES);

  const {
    data: externalKey,
    isLoading,
    isError,
  } = useGetGcpKmsKey({ id: keyId }, undefined, {
    enabled: isGcp && keyId !== "" && canRead,
    // A missing key is handled below by returning to the list; left to the
    // default, the 404 would surface as a crash in the error boundary. Every
    // other failure still belongs to the boundary.
    throwOnError: isNotNotFound,
  });

  const activeTab = activeDetailTab(location.pathname, EXTERNAL_KEY_TABS);
  const tabHref = (tab: ExternalKeyTab) =>
    orgRoutes.encryptionKeys.keyDetail[EXTERNAL_KEY_TAB_ROUTE_KEY[tab]].href(
      providerParam,
      keyId,
    );

  const label = externalKey?.name ?? "Encryption Key";

  if (!isGcp) {
    return <UnsupportedProvider provider={provider} />;
  }

  // Resolve access before any not-found handling. With the fetch disabled the
  // absence of a key says nothing about whether it exists, so the checks below
  // would misreport it. RequireScope renders nothing while grants load and the
  // unauthorized panel once they deny.
  if (rbacLoading || !canRead) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RequireScope scope={ORG_READ_SCOPES} level="page">
            <Text muted>Loading…</Text>
          </RequireScope>
        </Page.Body>
      </Page>
    );
  }

  // The key doesn't exist or failed to load; return to the listing.
  if (isError || (!isLoading && !externalKey)) {
    return <Navigate to={orgRoutes.encryptionKeys.href()} replace />;
  }

  // The bare /:provider/:keyId URL has no tab; canonicalize to Overview.
  if (!activeTab) {
    return <Navigate to={tabHref("overview")} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [keyId]: label }}
          // The provider segment is part of the key's path but has no page of
          // its own, so a crumb for it would render a link to a route nothing
          // matches.
          skipSegments={[providerParam]}
        />
      </Page.Header>
      <Page.Body fullWidth noPadding className="gap-0">
        <DetailHero>
          <Page.Eyebrow />
          <Text small muted>
            {externalKey
              ? providerLabel(externalKey.provider)
              : "Encryption Key"}
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
                <PageTabsTrigger value="signing-keys" asChild>
                  <Link to={tabHref("signing-keys")}>Signing Keys</Link>
                </PageTabsTrigger>
                <PageTabsTrigger value="settings" asChild>
                  <Link to={tabHref("settings")}>Settings</Link>
                </PageTabsTrigger>
              </PageTabsList>
            </div>
          </div>

          <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
            <TabsContent value="overview" className="mt-0">
              {/* Keyed like the Settings tab: the verify result lives in tab
                  state, so without this a key-to-key navigation would keep
                  showing the previous key's panel. */}
              {externalKey && (
                <OverviewTab key={externalKey.id} externalKey={externalKey} />
              )}
              {isLoading && <Text muted>Loading…</Text>}
            </TabsContent>
            <TabsContent value="signing-keys" className="mt-0">
              <SigningKeysTab externalKeyId={keyId} />
            </TabsContent>
            <TabsContent value="settings" className="mt-0">
              {externalKey && (
                <RequireScope
                  scope="org:admin"
                  level="section"
                  fallback={
                    <Text muted>
                      Editing this key requires an organization admin.
                    </Text>
                  }
                >
                  <SettingsTab key={externalKey.id} externalKey={externalKey} />
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
// rather than redirecting silently — a stored AWS key is a real record, and a
// bounce back to the list looks like the page lost it.
function UnsupportedProvider({
  provider,
}: {
  provider: string | undefined;
}): JSX.Element {
  const message = provider
    ? `${providerLabel(provider)} keys cannot be managed in the dashboard yet.`
    : "This encryption key provider is not recognized.";

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
