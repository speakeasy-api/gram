import { useOrgRoutes } from "@/routes";
import { Navigate, useSearchParams } from "react-router";

/**
 * Legacy route target: Platform MCP setup now lives in headless mode, so the
 * old standalone page redirects there. The `entrySource` deep link is carried
 * along, since the CTAs that pointed here used it to attribute the onboarding
 * workflow. `setup=1` is dropped: headless mode opens on the agent picker the
 * flag used to unfold, so there is nothing left for it to switch on.
 */
export default function PlatformMCPRedirect(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const [searchParams] = useSearchParams();
  const entrySource = searchParams.get("entrySource");
  const suffix = entrySource
    ? `?entrySource=${encodeURIComponent(entrySource)}`
    : "";
  return <Navigate to={`${orgRoutes.headless.href()}${suffix}`} replace />;
}
