import { Link } from "react-router";

import { Button } from "@/components/ui/Button";

import type { StepAffordances } from "./ConnectionChecklist";
import type { LiveConnection } from "../../connectionView";
import { CopyableValue } from "./OktaConnectionDetails";
import { AgentSetupForm } from "./OktaConnectionForms";
import { oktaAdminConsoleUrl } from "../../oktaConsoleLinks";
import { oktaViewHref } from "../../tabs";

function adminConsoleLink(connection: LiveConnection): JSX.Element {
  return (
    <a
      href={oktaAdminConsoleUrl(connection.orgUrl)}
      target="_blank"
      rel="noopener noreferrer"
      className="text-sm underline underline-offset-4"
    >
      Open the Okta Admin Console
    </a>
  );
}

export const STEP_AFFORDANCES: StepAffordances = {
  create_api_services_app: adminConsoleLink,
  add_oin_app: adminConsoleLink,
  public_key_auth: (connection) => (
    <span className="font-mono text-xs">
      <CopyableValue
        value={connection.jwksUrl}
        tooltip="Copy public key URL (JWKS)"
      />
    </span>
  ),
  record_ai_agent: (connection) => (
    <AgentSetupForm
      key={`${connection.agentId ?? ""}|${connection.agentAppId ?? ""}`}
      connection={connection}
    />
  ),
  first_resource_connection: () => (
    <div>
      <Button asChild variant="secondary">
        <Link to={oktaViewHref("cross-app-access")}>
          Configure Cross App Access
        </Link>
      </Button>
    </div>
  ),
};
