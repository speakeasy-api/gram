import { useHideInsightsDock } from "@/components/insights-context";
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
          {/* Full content width so the widget lines up with the dashboard
              below (the /chat page centers it; the home page does not). */}
          <div className="w-full pt-2 pb-6">
            <ChatLanding />
          </div>
          <ProjectDashboard />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
