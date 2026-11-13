import { policyCenterTabSearch } from "@/pages/security/policy-center-tabs";
import { useRoutes } from "@/routes";
import { Navigate, Outlet, useLocation, useParams } from "react-router";

export function ShadowAIRoot(): JSX.Element {
  return <Outlet />;
}

// Shadow AI lives in Guardrails: the inventory and its decisions are the
// Shadow AI policy and the Shadow AI targets tab. The section's own URLs stay
// as redirects to that tab, since they sit in tickets, bookmarks and block
// messages, and the tool and server detail pages under them still render.
export function ShadowAISectionRedirect(): JSX.Element {
  const routes = useRoutes();
  return (
    <Navigate
      to={`${routes.policyCenter.href()}${policyCenterTabSearch("shadow-ai-targets")}`}
      replace
    />
  );
}

// The MCPs index of the section was the approvals queue. Block messages,
// emails and bookmarks carry its path, so it lands on the Shadow MCP page,
// where the queue lives now. Its query and fragment named filters of the old
// page, so they are dropped rather than carried over.
export function ShadowMCPIndexRedirect(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.shadowMCP.href()} replace />;
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
