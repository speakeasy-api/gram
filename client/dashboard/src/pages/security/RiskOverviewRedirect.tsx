import { useRoutes } from "@/routes";
import { Navigate } from "react-router";

/** The retired Risk Overview page and its drill-downs land on Watchdog. */
export default function RiskOverviewRedirect(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.watchdog.href()} replace />;
}
