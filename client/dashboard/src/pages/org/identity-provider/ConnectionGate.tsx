import type { ReactNode } from "react";
import { Link } from "react-router";
import { Button } from "@/components/ui/Button";
import { InlineEmptyState } from "@/components/inline-empty-state";
import type { IconName } from "@/components/ui/Icon/names";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";

import {
  canConfirmReadiness,
  isConnectionChecked,
  isConnectionVerified,
  VERIFICATION_REASON_LABELS,
} from "./connectionView";
import { CONNECTION_SECTION_ID, oktaViewHref } from "./tabs";

/** Which server gate the tab sits behind: the applications snapshot needs a clean verification, readiness accepts a degraded one. */
type GateRequirement = "verified" | "checked";

function degradedDescription(
  connection: OktaIdentityProviderConnection,
): string {
  const missing = connection.missingScopes;
  const scopes =
    missing.length > 0
      ? `Okta has not granted these permissions (scopes): ${missing.join(", ")}.`
      : connection.verificationReasons
          .map((reason) => VERIFICATION_REASON_LABELS[reason])
          .join(" ");
  const instruction =
    missing.length > 0
      ? "Grant the missing permissions to the app in Okta"
      : "Resolve the verification issues above in Okta";
  return `${scopes} ${instruction}, then re-verify on the Okta Setup tab.`.trim();
}

/** Renders the tab once the connection meets its server gate, otherwise the way back to setup. */
export function ConnectionGate({
  connection,
  requires,
  icon,
  purpose,
  children,
}: {
  connection: OktaIdentityProviderConnection;
  requires: GateRequirement;
  icon: IconName;
  purpose: string;
  children: ReactNode;
}): JSX.Element {
  if (!isConnectionChecked(connection)) {
    return (
      <InlineEmptyState
        icon={icon}
        action={
          <Button asChild>
            <Link to={oktaViewHref("setup")}>Go to Okta setup</Link>
          </Button>
        }
        heading="Verify the connection first"
        description={`Paste the Okta app’s Client ID and verify the connection on the Okta Setup tab ${purpose}.`}
      />
    );
  }
  const allowed =
    requires === "verified"
      ? isConnectionVerified(connection)
      : canConfirmReadiness(connection);
  if (!allowed) {
    return (
      <InlineEmptyState
        icon={icon}
        action={
          <Button asChild>
            <Link to={oktaViewHref("setup", CONNECTION_SECTION_ID)}>
              Re-verify the connection
            </Link>
          </Button>
        }
        heading="The connection needs attention"
        description={degradedDescription(connection)}
      />
    );
  }
  return <>{children}</>;
}
