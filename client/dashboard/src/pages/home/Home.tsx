import { useHideInsightsDock } from "@/components/insights-context";
import { ProjectGuide } from "@/components/project-guide/ProjectGuide";
import { Page } from "@/components/page-layout";
import { ProjectDashboard } from "@/components/project/ProjectDashboard";
import { RequireScope } from "@/components/require-scope";
import { Skeleton } from "@/components/ui/Skeleton";
import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { cn } from "@/lib/utils";
import { ChatLanding } from "@/pages/chat/Chat";
import { useProjectGuide } from "@/hooks/useProjectGuide";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { Navigate } from "react-router";

export default function Home(): JSX.Element {
  const { hasAnyScope, isLoading } = useRBAC();
  const routes = useRoutes();
  const { status: projectGuideStatus } = useProjectGuide({
    enabled: !isLoading && hasAnyScope(["project:read"]),
  });
  // Home carries its own "Ask anything" widget, so suppress the floating dock.
  useHideInsightsDock();

  // Redirect MCP-only users (no project:read) to the MCP page
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
      <Page.Body
        fullWidth={projectGuideStatus === "guide" || undefined}
        fullHeight={projectGuideStatus === "guide" || undefined}
        noPadding={projectGuideStatus === "guide" || undefined}
      >
        <RequireScope scope="project:read" level="page">
          {projectGuideStatus === "pending" ? (
            <div data-testid="project-guide-pending" className="w-full p-8">
              <Skeleton className="h-64 w-full" />
            </div>
          ) : projectGuideStatus === "guide" ? (
            <ProjectGuide />
          ) : (
            <>
              {/* Full content width so the widget lines up with the dashboard
                  below (the /chat page centers it; the home page does not). The
                  compact variant drops pinned/recents — history lives on /chat. */}
              <div className="w-full pt-2 pb-6">
                {/* Neutral theme-following gradient surface with the brand
                    rainbow breathing in from the top-right corner as an edge
                    affordance, and film grain throughout. */}
                {/* No `overflow-hidden` here — it would clip the composer's slash
                    menu. The decorative layers clip themselves instead. */}
                {/* `z-10` lifts the card's stacking context above the dashboard
                    below, so the slash menu overlays it instead of the other way
                    round. */}
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
            </>
          )}
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
