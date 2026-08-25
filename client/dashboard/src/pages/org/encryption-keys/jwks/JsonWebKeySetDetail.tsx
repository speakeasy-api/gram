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
import { useGetJsonWebKeySet } from "@gram/client/react-query/getJsonWebKeySet";
import { Link, Navigate, Outlet, useLocation, useParams } from "react-router";
import { JSON_WEB_KEY_SET_TABS, type JsonWebKeySetTab } from "./tabs";
import { KeysTab } from "./tabs/KeysTab";
import { OverviewTab } from "./tabs/OverviewTab";
import { SettingsTab } from "./tabs/SettingsTab";

const ORG_READ_SCOPES: Scope[] = ["org:read", "org:admin"];

export function SigningKeySetsRoot(): JSX.Element {
  return <Outlet />;
}

// The sets are listed on the Encryption Keys page; the bare /signing-keys URL
// only exists so the detail pages have a parent segment of their own.
export function SigningKeySetsIndex(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  return <Navigate to={orgRoutes.encryptionKeys.href()} replace />;
}

export default function JsonWebKeySetDetail(): JSX.Element {
  const { setId = "" } = useParams<{ setId: string }>();
  const orgRoutes = useOrgRoutes();
  const location = useLocation();

  // The read scope gates the fetch, not just the rendering: without it the
  // request would 403 and the not-found handling below would bounce the caller
  // back to the list, which reads as "this set is gone" rather than "you cannot
  // see it".
  const { hasAnyScope, isLoading: rbacLoading } = useRBAC();
  const canRead = hasAnyScope(ORG_READ_SCOPES);

  const {
    data: set,
    isLoading,
    isError,
  } = useGetJsonWebKeySet({ id: setId }, undefined, {
    enabled: setId !== "" && canRead,
    // A missing set is handled below by returning to the list; left to the
    // default, the 404 would surface as a crash in the error boundary.
    throwOnError: false,
  });

  const activeTab = activeDetailTab(location.pathname, JSON_WEB_KEY_SET_TABS);
  const tabHref = (tab: JsonWebKeySetTab) =>
    orgRoutes.signingKeySets.setDetail[tab].href(setId);

  const label = set?.name ?? "Signing Key Set";

  // Resolve access before any not-found handling. With the fetch disabled the
  // absence of a set says nothing about whether it exists, so the checks below
  // would misreport it.
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

  // The set doesn't exist or failed to load; return to the listing.
  if (isError || (!isLoading && !set)) {
    return <Navigate to={orgRoutes.encryptionKeys.href()} replace />;
  }

  // The bare /signing-keys/:setId URL has no tab; canonicalize to Overview.
  if (!activeTab) {
    return <Navigate to={tabHref("overview")} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs substitutions={{ [setId]: label }} />
      </Page.Header>
      <Page.Body fullWidth noPadding className="gap-0">
        <DetailHero>
          <Page.Eyebrow />
          <Text small muted>
            Signing Key Set
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
                <PageTabsTrigger value="keys" asChild>
                  <Link to={tabHref("keys")}>Keys</Link>
                </PageTabsTrigger>
                <PageTabsTrigger value="settings" asChild>
                  <Link to={tabHref("settings")}>Settings</Link>
                </PageTabsTrigger>
              </PageTabsList>
            </div>
          </div>

          <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
            <TabsContent value="overview" className="mt-0">
              {set && <OverviewTab key={set.id} set={set} />}
              {isLoading && <Text muted>Loading…</Text>}
            </TabsContent>
            <TabsContent value="keys" className="mt-0">
              {/* Keyed so the revoked toggle and any open dialog reset when
                  navigating from one set straight to another. */}
              {set && <KeysTab key={set.id} set={set} />}
              {isLoading && <Text muted>Loading…</Text>}
            </TabsContent>
            <TabsContent value="settings" className="mt-0">
              {set && (
                <RequireScope
                  scope="org:admin"
                  level="section"
                  fallback={
                    <Text muted>
                      Editing this key set requires an organization admin.
                    </Text>
                  }
                >
                  <SettingsTab key={set.id} set={set} />
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
