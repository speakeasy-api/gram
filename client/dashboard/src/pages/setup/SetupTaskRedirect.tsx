import { useOrgRoutes } from "@/routes";
import { Navigate, useParams } from "react-router";
import { setupTaskKeyForSlug, setupTaskSlug } from "./setup-cards";

/**
 * Legacy route target: each setup card used to have its own page at
 * setup/:taskSlug (and the wizard lived at setup/wizard). The wizard at
 * /setup is the only view now, so these open it on the named card.
 */
export default function SetupTaskRedirect(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const { taskSlug = "" } = useParams();
  const key = setupTaskKeyForSlug(taskSlug);
  const suffix = key ? `?task=${setupTaskSlug(key)}` : "";
  return <Navigate to={`${orgRoutes.setup.href()}${suffix}`} replace />;
}
