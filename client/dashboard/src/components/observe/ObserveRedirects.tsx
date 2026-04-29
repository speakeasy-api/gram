import { Navigate, useLocation } from "react-router";
import { useRoutes } from "@/routes";

export function RedirectToInsightsTools() {
  const routes = useRoutes();
  const location = useLocation();
  return (
    <Navigate
      to={`${routes.insights.tools.href()}${location.search}${location.hash}`}
      replace
    />
  );
}

export function RedirectToLogAgents() {
  const routes = useRoutes();
  const location = useLocation();
  return (
    <Navigate
      to={`${routes.logs.agents.href()}${location.search}${location.hash}`}
      replace
    />
  );
}
