import { useRoutes } from "@/routes";
import { Navigate, useLocation } from "react-router";

/**
 * The retired Risk Overview page and its drill-downs land on Watchdog. Both
 * read the same range/from/to params, so the query string carries over.
 */
export default function RiskOverviewRedirect(): JSX.Element {
  const routes = useRoutes();
  const location = useLocation();
  return (
    <Navigate to={`${routes.watchdog.href()}${location.search}`} replace />
  );
}
