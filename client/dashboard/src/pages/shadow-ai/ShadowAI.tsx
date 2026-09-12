import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { PageTabsList, PageTabsTrigger, Tabs } from "@/components/ui/Tabs";
import { useRoutes } from "@/routes";
import { Link, Navigate, Outlet, useLocation, useParams } from "react-router";

// Shadow MCP is one part of Shadow AI, not a feature beside it: a shadow MCP
// server is reached by some AI tool, and the same admin answers "what are
// people running?" and "what is it talking to?" in one sitting.
//
// Three routed tabs rather than one page with a toggle, so a link to any of
// them survives being pasted into a ticket. The order is deliberate:
// harnesses and assistants are what people run and the only things blocking
// applies to; models are local runtimes that never speak MCP to Gram, so they
// are inventory only; MCP servers are what those tools reached.
const SHADOW_AI_SEGMENT = "shadow-ai";

const TAB_VALUES = {
  harnesses: "harnesses",
  assistants: "assistants",
  models: "models",
  mcps: "mcps",
} as const;

export type ShadowAITab = (typeof TAB_VALUES)[keyof typeof TAB_VALUES];

export const shadowAIBreadcrumbSubstitutions: Record<string, string> = {
  [SHADOW_AI_SEGMENT]: "Shadow AI",
  [TAB_VALUES.harnesses]: "Harnesses",
  [TAB_VALUES.assistants]: "Assistants",
  [TAB_VALUES.models]: "Models",
  [TAB_VALUES.mcps]: "MCPs",
};

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
      to={`${routes.shadowAI.harnesses.href()}${location.search}`}
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
    <Navigate to={`${routes.shadowAI.mcps.href()}${location.search}`} replace />
  );
}

export function ShadowMCPServerLegacyRedirect(): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();
  const { serverSlug = "" } = useParams<{ serverSlug: string }>();
  return (
    <Navigate
      to={`${routes.shadowAI.mcps.detail.href(serverSlug)}${location.search}`}
      replace
    />
  );
}

// ShadowAISection is the shell both tabs render inside. A server detail page
// lives under the MCP tab and renders its own page chrome, so the shell is
// only used by the two tab index routes.
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
        {/* Project read to view, organization admin to act. The reads behind
            both tabs are organization-scoped, but a project is how an
            organization segments the people it manages and the answer is the
            same whichever project you arrive from — the same reasoning the
            Identities routes are placed under. The server withholds every
            attribution field from this scope; see inventoryProjection. */}
        <RequireScope scope={["project:read", "project:write"]} level="page">
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
                  <Link to={routes.shadowAI.models.href()}>Models</Link>
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
