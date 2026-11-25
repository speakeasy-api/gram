import { useHideInsightsDock } from "@/components/insights-context";
import { useAssistantsAvailable } from "@/hooks/useAssistantsAvailability";
import { Page } from "@/components/page-layout";
import { ProjectDashboard } from "@/components/project/ProjectDashboard";
import { RequireScope } from "@/components/require-scope";
import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { cn } from "@/lib/utils";
import { ChatLanding } from "@/pages/chat/Chat";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { Navigate } from "react-router";
import {
  HomeWidgets,
  TALL_ATTENTION_ROWS,
} from "@/components/home/HomeWidgets";
import { HomeTerminal } from "@/components/home/HomeTerminal";
import { useConnectionProblems } from "@/components/home/useConnectionProblems";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { Skeleton } from "@/components/ui/Skeleton";

export default function Home(): JSX.Element {
  const { hasAnyScope, hasAnyScopeInProject, isLoading } = useRBAC();
  const terminalHome = useFeatureFlag(FEATURE_FLAGS.terminalHome);
  const routes = useRoutes();
  const { id: projectId } = useProject();
  const assistantsAvailable = useAssistantsAvailable();
  // Home carries its own "Ask anything" widget, so suppress the floating dock.
  useHideInsightsDock();

  // Keep MCP-only users on the MCP page instead of rendering the project
  // overview's project:read fallback.
  if (
    !isLoading &&
    !hasAnyScope(["project:read"], projectId) &&
    hasAnyScopeInProject(["mcp:read", "mcp:write"], projectId)
  ) {
    return <Navigate to={routes.mcp.href()} replace />;
  }

  // Without the assistant there is nothing to anchor the grid, so those
  // projects keep the classic overview.
  const showTerminalHome =
    terminalHome.status === "enabled" &&
    assistantsAvailable &&
    hasAnyScope(["assistant:read"], projectId);

  if (terminalHome.status === "loading") {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <Skeleton
            label="Loading the project home"
            className="h-[32rem] w-full"
          />
        </Page.Body>
      </Page>
    );
  }

  if (showTerminalHome) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RequireScope
            scope="project:read"
            resourceId={projectId}
            level="page"
          >
            <TerminalHomeGrid />
          </RequireScope>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:read" resourceId={projectId} level="page">
          {/* Full content width so the widget lines up with the dashboard
              below (the /chat page centers it; the home page does not). The
              compact variant drops pinned/recents — history lives on /chat. */}
          {assistantsAvailable && (
            <RequireScope
              scope="assistant:read"
              projectId={projectId}
              level="section"
            >
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
            </RequireScope>
          )}
          <ProjectDashboard />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

/**
 * Bento grid around the terminal: on wide screens the terminal takes the
 * right two of three columns and the top two rows; tiles fill the left column
 * and the bottom row. Below `lg` everything stacks, terminal first.
 */
function TerminalHomeGrid(): JSX.Element {
  // Who may see connection problems, not whether any exist, so the grid stays put.
  const tall = useConnectionProblems(TALL_ATTENTION_ROWS).display === "tall";
  return (
    // On wide screens the grid fills the viewport below the header: three
    // equal rows, the terminal spanning the top two, and every widget
    // scrolling inside its own card instead of growing the page.
    <div
      className={cn(
        "grid grid-cols-1 gap-6 pt-2 lg:h-[calc(100dvh-var(--header-height)-var(--banner-offset,0px)-7.5rem)] lg:grid-rows-3",
        tall ? "lg:grid-cols-12" : "lg:grid-cols-3",
      )}
    >
      <HomeTerminal
        className={cn(
          "h-[32rem] lg:row-span-2 lg:row-start-1 lg:h-auto",
          tall
            ? "lg:col-span-8 lg:col-start-5"
            : "lg:col-span-2 lg:col-start-2",
        )}
      />
      <HomeWidgets
        classNames={
          tall
            ? [
                "lg:col-span-4 lg:col-start-1 lg:row-span-2 lg:row-start-1",
                "lg:col-span-3 lg:row-start-3",
                "lg:col-span-3 lg:row-start-3",
                "lg:col-span-3 lg:row-start-3",
                "lg:col-span-3 lg:row-start-3",
              ]
            : [
                "lg:col-start-1 lg:row-start-1",
                "lg:col-start-1 lg:row-start-2",
                "",
                "",
                "",
              ]
        }
      />
    </div>
  );
}
