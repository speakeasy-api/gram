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

// The coding agents tab was once called Harnesses, and its tool detail paths
// are in tickets and bookmarks, so they redirect permanently rather than 404.
export function ShadowAIHarnessDetailLegacyRedirect(): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();
  const { targetId = "" } = useParams<{ targetId: string }>();
  // useParams decodes the segment and href does not encode, so re-encode it:
  // ids like acme/agent would otherwise split into two path segments.
  return (
    <Navigate
      to={`${routes.shadowAI.agents.detail.href(encodeURIComponent(targetId))}${location.search}${location.hash}`}
      replace
    />
  );
}

// The Shadow MCP paths predate the section and are in block messages, emails
// and bookmarks, so they redirect permanently rather than 404.
export function ShadowMCPLegacyRedirect(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.policyCenter.href()} replace />;
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
