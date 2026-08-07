import { useHideInsightsDock } from "@/components/insights-context";
import { Page } from "@/components/page-layout";
import { ProjectDashboard } from "@/components/project/ProjectDashboard";
import { RequireScope } from "@/components/require-scope";
import {
  GRAIN_TEXTURE_URL,
  RAINBOW_EDGE_GRADIENT,
  RAINBOW_EDGE_MASK,
} from "@/lib/brand-mesh";
import { ChatLanding } from "@/pages/chat/Chat";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { Navigate } from "react-router";

export default function Home(): JSX.Element {
  const { hasAnyScope, isLoading } = useRBAC();
  const routes = useRoutes();
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
      <Page.Body>
        <RequireScope scope="project:read" level="page">
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
            <div className="border-border from-card to-background relative isolate z-10 border bg-gradient-to-br p-8">
              <div
                aria-hidden="true"
                className="pointer-events-none absolute inset-0 -z-10 overflow-hidden opacity-60 saturate-[0.65]"
                style={{
                  background: RAINBOW_EDGE_GRADIENT,
                  maskImage: RAINBOW_EDGE_MASK,
                  WebkitMaskImage: RAINBOW_EDGE_MASK,
                }}
              />
              <div
                aria-hidden="true"
                // Multiply beds the grain into a light surface; in dark mode
                // multiply is a no-op on near-black, so switch to screen.
                className="pointer-events-none absolute inset-0 -z-10 opacity-[0.45] mix-blend-multiply grayscale dark:opacity-[0.5] dark:mix-blend-screen"
                style={{ backgroundImage: GRAIN_TEXTURE_URL }}
              />
              <ChatLanding compact />
            </div>
          </div>
          <ProjectDashboard />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
