import { Navigate, useSearchParams } from "react-router";

import { useRoutes } from "@/routes";
import { encodeIdentityUrn } from "@/lib/identity-urn";

/**
 * `agent-management` was a second home for agents: a roster and a detail page
 * that showed what the Identities pages already show. An agent has one page
 * now, so this is what is left of it — the links that named this address keep
 * working.
 *
 * `?id=` named an agent, `?create=true` opened the wizard, and `?credential=`
 * asked that page to start issuing a key. Each has a home of its own.
 */
export default function AgentManagementRedirect(): JSX.Element {
  const routes = useRoutes();
  const [params] = useSearchParams();
  const agentID = params.get("id");

  if (params.get("create") === "true") {
    return <Navigate to={routes.identities.agents.new.href()} replace />;
  }

  if (agentID) {
    const urn = encodeIdentityUrn(`agent:${agentID}`);
    // Issuing a key is a step on the agent's provisioning section, so a link
    // that asked for it lands there rather than on the overview.
    const to =
      params.get("credential") === "new"
        ? `${routes.identities.detail.provisioning.href(urn)}?credential=new`
        : routes.identities.detail.overview.href(urn);
    return <Navigate to={to} replace />;
  }

  return <Navigate to={routes.identities.agents.href()} replace />;
}
