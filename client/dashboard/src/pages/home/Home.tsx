import { useHideInsightsDock } from "@/components/insights-context";
import { Page } from "@/components/page-layout";
import { ProjectDashboard } from "@/components/project/ProjectDashboard";
import { RequireScope } from "@/components/require-scope";
import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { cn } from "@/lib/utils";
import { ChatLanding } from "@/pages/chat/Chat";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { Navigate } from "react-router";

export default function Home(): JSX.Element {
  const { hasAnyScope, isLoading } = useRBAC();
  const routes = useRoutes();
  // Home carries its own "Ask anything" widget, so suppress the floating dock.
  useHideInsightsDock();

  // Keep MCP-only users on the MCP page instead of rendering the project
  // overview's project:read fallback.
  if (
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
              below (the /chat page centers it; the home page does not). The
              compact variant drops pinned/recents — history lives on /chat. */}
          <div className="w-full pt-2 pb-6">
            <div
              className={cn(
                BRAND_MESH_SURFACE_CLASS,
                "border-border z-10 border p-8",
              )}
            >
              <BrandMeshLayers />
              <ChatLanding compact />
            </div>
          </div>
          <ProjectDashboard />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
