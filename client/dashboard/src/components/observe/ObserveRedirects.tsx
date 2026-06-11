import { Navigate, useLocation } from "react-router";
import { useRoutes } from "@/routes";

function RedirectPreservingLocation({ to }: { to: string }) {
  const location = useLocation();
  return <Navigate to={`${to}${location.search}${location.hash}`} replace />;
}

export function RedirectToInsightsTools(): JSX.Element {
  const routes = useRoutes();
  return <RedirectPreservingLocation to={routes.insights.tools.href()} />;
}

export function RedirectToLogTools(): JSX.Element {
  const routes = useRoutes();
  return <RedirectPreservingLocation to={routes.logs.tools.href()} />;
}
