import { Link } from "react-router";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { AGENT_SECTION_ID } from "./OktaConnectionSteps";
import { identityTabHref } from "./identityProviderQueries";

/** Keep the next task visible instead of ending setup at a wall of connection details. */
export function ConnectionNextSteps({
  connection,
}: {
  connection: Pick<OktaIdentityProviderConnection, "status" | "agentId">;
}): JSX.Element | null {
  if (connection.status !== "verified") return null;
  const hasAgent = Boolean(connection.agentId?.trim());
  return (
    <section aria-label="Next steps" className="flex flex-col gap-3 border p-4">
      <Text className="font-medium">Okta is connected</Text>
      <Text muted small>
        {hasAgent
          ? "Application sync is ready. Review your agent’s resource connections in Cross App Access. A saved Agent ID does not configure or verify agent authentication."
          : "Application sync is ready. Agent authentication is not connected by this setup. Recording an existing agent is optional."}
      </Text>
      <div className="flex flex-wrap gap-2">
        <Button asChild>
          <Link to={identityTabHref("applications")}>View applications</Link>
        </Button>
        <Button asChild variant="secondary">
          <Link
            to={
              hasAgent
                ? identityTabHref("cross-app-access")
                : identityTabHref("provider", AGENT_SECTION_ID)
            }
          >
            {hasAgent ? "Set up Cross App Access" : "Record existing agent"}
          </Link>
        </Button>
      </div>
    </section>
  );
}
