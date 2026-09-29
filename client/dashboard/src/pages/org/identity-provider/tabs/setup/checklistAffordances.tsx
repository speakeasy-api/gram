import { Link } from "react-router";

import { Button } from "@/components/ui/Button";

import type { StepAffordances } from "./ConnectionChecklist";
import { connectionStep, type LiveConnection } from "../../connectionView";
import { CopyableValue } from "./OktaConnectionDetails";
import { AgentSetupForm, ClientIdForm } from "./OktaConnectionForms";
import {
  oktaAdminConsoleUrl,
  oktaApiServiceIntegrationsUrl,
} from "../../oktaConsoleLinks";
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

function apiServiceIntegrationsLink(
  connection: LiveConnection,
): JSX.Element | null {
  const href = oktaApiServiceIntegrationsUrl(connection.orgUrl);
  if (!href) return null;
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-sm underline underline-offset-4"
    >
      Open API Service Integrations in Okta
    </a>
  );
}

export const STEP_AFFORDANCES: StepAffordances = {
  create_api_services_app: adminConsoleLink,
  add_oin_app: apiServiceIntegrationsLink,
  public_key_auth: (connection) =>
    connection.jwksUrl ? (
      <span className="font-mono text-xs">
        <CopyableValue
          value={connection.jwksUrl}
          tooltip="Copy public key URL (JWKS)"
        />
      </span>
    ) : null,
  submit_client_id: (connection) =>
    connectionStep(connection) === "submit_client_id" ? (
      <ClientIdForm key={connection.id} connection={connection} />
    ) : null,
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
