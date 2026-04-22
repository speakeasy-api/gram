import { Page } from "@/components/page-layout";
import { ProjectDashboard } from "@/components/project/ProjectDashboard";
import { RequireScope } from "@/components/require-scope";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { Navigate } from "react-router";

export default function Home() {
  const { hasAnyScope, isRbacEnabled, isLoading } = useRBAC();
  const routes = useRoutes();

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
          <ProjectDashboard />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
