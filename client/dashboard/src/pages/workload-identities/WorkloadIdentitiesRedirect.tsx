import { useRoutes } from "@/routes";
import { Navigate } from "react-router";

/** The Workload Identities page became the Access Hub; old links land there. */
export default function WorkloadIdentitiesRedirect(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.workloadIssuers.href()} replace />;
}
