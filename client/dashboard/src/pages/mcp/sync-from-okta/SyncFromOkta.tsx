import { ApiErrorAlert } from "@/components/api-error-alert";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { useProject } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { isConnected } from "@/pages/org/identity-provider/connectionView";
import { useOktaConnection } from "@/pages/org/identity-provider/identityProviderQueries";
import { connectionsUnavailable } from "@/pages/org/identity-provider/oktaEma";
import { oktaViewHref } from "@/pages/org/identity-provider/tabs";
import { useOrgRoutes, useRoutes } from "@/routes";
import { Link, Navigate } from "react-router";
import { ApplicationsTab } from "./ApplicationsTab";

/**
 * The applications in the organization's Okta tenant that match catalog
 * servers, added to this project from here. An option of Add MCP server,
 * rolled out behind okta-mcp-import.
 */
export function SyncFromOkta(): JSX.Element | null {
  const flag = useFeatureFlag(FEATURE_FLAGS.oktaMcpImport);
  const routes = useRoutes();
  // Render nothing until flags resolve rather than flashing a redirect.
  if (flag.status === "loading") return null;
  if (flag.status !== "enabled") {
    return <Navigate to={routes.mcp.add.href()} replace />;
  }
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Sync from Okta</Page.Section.Title>
          <Page.Section.Description className="max-w-2xl">
            Catalog servers for the applications your people already use in
            Okta, ready to add to this project. Nothing is added until you
            choose it.
          </Page.Section.Description>
        </Page.Section>
        {/* The Okta connection and its applications are organization
            settings, so reading them takes an organization admin. */}
        <RequireScope scope="org:admin" level="page">
          <OktaApplications />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function OktaApplications(): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  const { query, connection } = useOktaConnection();
  const setupHref = (search: string) => `${orgRoutes.identity.href()}${search}`;

  if (query.isPending) return <SkeletonParagraph />;
  if (connectionsUnavailable(query.error)) {
    return (
      <InlineEmptyState
        icon="plug"
        heading="Okta can't be connected on this deployment"
        description="Syncing applications needs an Okta connection, which this deployment isn't set up to hold. Add servers from the catalog instead."
        action={
          <Button variant="secondary" asChild>
            <Link to={routes.mcp.catalog.href()}>Browse the catalog</Link>
          </Button>
        }
      />
    );
  }
  if (query.isError) return <ApiErrorAlert error={query.error} />;
  if (connection?.provider !== "okta" || !isConnected(connection)) {
    return (
      <InlineEmptyState
        icon="plug"
        heading="Connect Okta to see its applications here"
        description="Speakeasy reads which applications your people are assigned in Okta and suggests the catalog server for each one. An organization admin connects Okta once for every project."
        action={
          <Button asChild>
            <Link to={setupHref(oktaViewHref("overview"))}>Set up Okta</Link>
          </Button>
        }
      />
    );
  }
  return (
    <ApplicationsTab
      connection={connection}
      projectSlug={project.slug}
      setupHref={setupHref}
    />
  );
}
