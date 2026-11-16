import { policyCenterTabSearch } from "@/pages/security/policy-center-tabs";
import { useRoutes } from "@/routes";
import { Navigate, Outlet, useLocation, useParams } from "react-router";

export function ShadowAIRoot(): JSX.Element {
  return <Outlet />;
}

// Shadow AI lives in Guardrails: the inventory and its decisions are the
// Shadow AI policy and the Shadow AI targets tab. The section's own URLs stay
// as redirects, since they sit in tickets, bookmarks and block messages, and
// the tool and server detail pages under them still render.
export function ShadowAISectionRedirect(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.policyCenter.href()} replace />;
}

// The MCPs index of the section and the older Shadow MCP index were the
// approvals queue. Block messages, emails and bookmarks carry both paths, so
// they land on the MCP approvals tab of Guardrails, where the queue lives
// now, rather than on the policies list. Their query and fragment named
// filters of the old page, so they are dropped rather than carried over.
export function ShadowMCPIndexRedirect(): JSX.Element {
  const routes = useRoutes();
  return (
    <Navigate
      to={`${routes.policyCenter.href()}${policyCenterTabSearch("mcp-approvals")}`}
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
