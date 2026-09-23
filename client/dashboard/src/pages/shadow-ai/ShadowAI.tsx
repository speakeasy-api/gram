import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { PageTabsList, PageTabsTrigger, Tabs } from "@/components/ui/Tabs";
import { useRoutes } from "@/routes";
import {
  shadowAIBreadcrumbSubstitutions,
  TAB_VALUES,
  type ShadowAITab,
} from "@/pages/shadow-ai/tabs";
import { Link, Navigate, Outlet, useLocation, useParams } from "react-router";

export function ShadowAIRoot(): JSX.Element {
  return <Outlet />;
}

// ShadowAIIndexRedirect sends the bare section URL to the harnesses tab: the
// tools people actually run, and the only ones an access decision reaches.
export function ShadowAIIndexRedirect(): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();
  return (
    <Navigate
      to={`${routes.shadowAI.harnesses.href()}${location.search}${location.hash}`}
      replace
    />
  );
}

// The Shadow MCP paths predate the section and are in block messages, emails
// and bookmarks, so they redirect permanently rather than 404.
export function ShadowMCPLegacyRedirect(): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();
  return (
    <Navigate
      to={`${routes.shadowAI.mcps.href()}${location.search}${location.hash}`}
      replace
    />
  );
}

export function ShadowMCPServerLegacyRedirect(): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();
  const { serverSlug = "" } = useParams<{ serverSlug: string }>();
  return (
    <Navigate
      to={`${routes.shadowAI.mcps.detail.href(serverSlug)}${location.search}${location.hash}`}
      replace
    />
  );
}

// ShadowAISection is the shell all four tab pages render inside. The detail
// pages under the tabs (a server under MCPs, a tool under the other three)
// render their own page chrome, so the shell is used by the tab index routes
// and not by them.
export function ShadowAISection({
  activeTab,
  children,
}: {
  activeTab: ShadowAITab;
  children: React.ReactNode;
}): JSX.Element {
  const routes = useRoutes();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={shadowAIBreadcrumbSubstitutions}
        />
      </Page.Header>
      <Page.Body fullHeight className="pb-8">
        {/* Organization admin to view. Every read behind these tabs (the AI
            detections inventory and the Shadow MCP inventory) requires
            org:admin on the server, so a narrower gate would only ever show
            an error state. The section still lives under a project because
            a project is how an organization segments the people it manages;
            the answer is the same whichever project you arrive from — the
            same reasoning the Identities routes are placed under. */}
        <RequireScope scope="org:admin" level="page">
          <Tabs value={activeTab}>
            <div className="border-border -mx-8 border-b px-8">
              <PageTabsList>
                <PageTabsTrigger value={TAB_VALUES.harnesses} asChild>
                  <Link to={routes.shadowAI.harnesses.href()}>Harnesses</Link>
                </PageTabsTrigger>
                <PageTabsTrigger value={TAB_VALUES.assistants} asChild>
                  <Link to={routes.shadowAI.assistants.href()}>Assistants</Link>
                </PageTabsTrigger>
                <PageTabsTrigger value={TAB_VALUES.models} asChild>
                  <Link to={routes.shadowAI.models.href()}>Local Models</Link>
                </PageTabsTrigger>
                <PageTabsTrigger value={TAB_VALUES.mcps} asChild>
                  <Link to={routes.shadowAI.mcps.href()}>MCPs</Link>
                </PageTabsTrigger>
              </PageTabsList>
            </div>
            <div className="mt-6">{children}</div>
          </Tabs>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
