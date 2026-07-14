import { useHideInsightsDock } from "@/components/insights-context";
import { ObservabilityLayout } from "@/components/layouts/observability-layout";
import { Page } from "@/components/page-layout";
import { ProjectDashboard } from "@/components/project/ProjectDashboard";
import { RequireScope } from "@/components/require-scope";
import { ChatLanding } from "@/pages/chat/Chat";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { Navigate } from "react-router";

export default function Home(): JSX.Element {
  const { hasAnyScope, isRbacEnabled, isLoading } = useRBAC();
  const routes = useRoutes();
  // Home carries its own "Ask anything" widget, so suppress the floating dock.
  useHideInsightsDock();

  // Redirect MCP-only users (no project:read) to the MCP page
  if (
    isRbacEnabled &&
    !isLoading &&
    !hasAnyScope(["project:read"]) &&
    hasAnyScope(["mcp:read", "mcp:write"])
  ) {
    return <Navigate to={routes.mcp.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:read" level="page">
          <ObservabilityLayout>
            <ObservabilityLayout.Strip>
              <ChatLanding />
            </ObservabilityLayout.Strip>
            {/* Supplies its own Header (title + range picker), Stats and
                chart Grids/Sections for the rest of the layout. */}
            <ProjectDashboard />
          </ObservabilityLayout>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
