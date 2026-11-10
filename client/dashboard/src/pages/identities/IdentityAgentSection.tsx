import { Navigate, useLocation } from "react-router";

/**
 * Placeholder for the agent-only sections of an identity.
 *
 * The detail root renders the agent profile in place of this outlet whenever
 * the subject is an agent, so for an agent this never runs — the route exists
 * to give those sections an address whose name matches the tab and the
 * breadcrumb.
 *
 * A person reaching one of these has followed a link to a section their kind
 * does not have, so they go to the section every subject has.
 */
export default function IdentityAgentSection(): JSX.Element {
  const location = useLocation();
  return <Navigate to={`../overview${location.search}`} replace />;
}
