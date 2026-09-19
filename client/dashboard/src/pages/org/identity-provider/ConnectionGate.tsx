import { Link } from "react-router";
import { Button } from "@/components/ui/Button";
import { InlineEmptyState } from "@/components/inline-empty-state";
import type { IconName } from "@/components/ui/Icon/names";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";

import {
  canConfirmReadiness,
  isConnectionChecked,
  isConnectionVerified,
  verificationReasonLabel,
} from "./connectionView";
import {
  CONNECTION_SECTION_ID,
  identityTabHref,
} from "./identityProviderQueries";

/** Which server gate the tab sits behind: the applications snapshot needs a clean verification, readiness accepts a degraded one. */
export type GateRequirement = "verified" | "checked";

function meetsRequirement(
  connection: OktaIdentityProviderConnection,
  requires: GateRequirement,
): boolean {
  return requires === "verified"
    ? isConnectionVerified(connection)
    : canConfirmReadiness(connection);
}

function degradedDescription(
  connection: OktaIdentityProviderConnection,
): string {
  const missing = connection.missingScopes;
  const scopes =
    missing.length > 0
      ? `Okta has not granted these permissions (scopes): ${missing.join(", ")}.`
      : connection.verificationReasons.map(verificationReasonLabel).join(" ");
  const instruction =
    missing.length > 0
      ? "Grant the missing permissions to the app in Okta"
      : "Resolve the verification issues above in Okta";
  return `${scopes} ${instruction}, then re-verify on the Okta Setup tab.`.trim();
}

/** Shared gate for the tabs that need a checked connection; null when the tab can render. */
export function ConnectionGate({
  connection,
  rolloutEnabled,
  icon,
  purpose,
  requires = "verified",
}: {
  connection: OktaIdentityProviderConnection | undefined;
  rolloutEnabled: boolean;
  icon: IconName;
  purpose: string;
  requires?: GateRequirement;
}): JSX.Element | null {
  if (!connection) {
    if (!rolloutEnabled) {
      return (
        <InlineEmptyState
          icon={icon}
          heading="Identity provider connections are not enabled for this organization"
          description="Speakeasy is rolling this out gradually. Ask your Speakeasy contact to enable it."
        />
      );
    }
    return (
      <InlineEmptyState
        icon={icon}
        action={
          <Button asChild>
            <Link to={identityTabHref("provider")}>Go to Okta setup</Link>
          </Button>
        }
        heading="Connect your identity provider first"
        description={`Connect one on the Okta Setup tab ${purpose}.`}
      />
    );
  }
  if (connection.status === "revoked") {
    return (
      <InlineEmptyState
        icon={icon}
        action={
          <Button asChild>
            <Link to={identityTabHref("provider")}>Go to Okta setup</Link>
          </Button>
        }
        heading="This connection was revoked"
        description="Connect your identity provider again from the Okta Setup tab."
      />
    );
  }
  if (!isConnectionChecked(connection)) {
    return (
      <InlineEmptyState
        icon={icon}
        action={
          <Button asChild>
            <Link to={identityTabHref("provider")}>Go to Okta setup</Link>
          </Button>
        }
        heading="Verify the connection first"
        description={`Paste the Okta app’s Client ID and verify the connection on the Okta Setup tab ${purpose}.`}
      />
    );
  }
  if (!meetsRequirement(connection, requires)) {
    return (
      <InlineEmptyState
        icon={icon}
        action={
          <Button asChild>
            <Link to={identityTabHref("provider", CONNECTION_SECTION_ID)}>
              Re-verify the connection
            </Link>
          </Button>
        }
        heading="The connection needs attention"
        description={degradedDescription(connection)}
      />
    );
  }
  return null;
}
