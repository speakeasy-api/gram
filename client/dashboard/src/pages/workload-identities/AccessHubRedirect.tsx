import { useOrgRoutes } from "@/routes";
import { Navigate, useLocation, useParams } from "react-router";

/**
 * The Access Hub configures the organization's trust policy, so it lives at
 * the organization level. Project-scoped Access Hub and Workload Identities
 * links land on that page, keeping any trusted platform id, query and hash.
 */
export default function AccessHubRedirect(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const location = useLocation();
  const { "*": rest = "" } = useParams();
  const base = orgRoutes.workloadIssuers.href();
  const path = rest ? `${base}/${rest}` : base;
  return <Navigate to={`${path}${location.search}${location.hash}`} replace />;
}
