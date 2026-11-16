import type { ReactNode } from "react";
import { Link, useSearchParams } from "react-router";
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
import { parseEmaReturn } from "./oktaEma";
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
  setupHref = (search) => search,
}: {
  connection: OktaIdentityProviderConnection;
  requires: GateRequirement;
  icon: IconName;
  purpose: string;
  children: ReactNode;
  /** Resolves an Okta workspace link; pages outside the identity page make it absolute. */
  setupHref?: (search: string) => string;
}): JSX.Element {
  if (!isConnectionChecked(connection)) {
    return (
      <InlineEmptyState
        icon={icon}
        action={
          <Button asChild>
            <SetupLink setupHref={setupHref}>Go to Okta setup</SetupLink>
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
            <SetupLink setupHref={setupHref} sectionId={CONNECTION_SECTION_ID}>
              Re-verify the connection
            </SetupLink>
          </Button>
        }
        heading="The connection needs attention"
        description={degradedDescription(connection)}
      />
    );
  }
  return <>{children}</>;
}

/** A link to Okta setup that keeps an Enterprise Managed Auth setup in progress. */
function SetupLink({
  setupHref,
  sectionId,
  children,
  ...props
}: {
  setupHref: (search: string) => string;
  sectionId?: string;
  children: ReactNode;
}): JSX.Element {
  const [searchParams] = useSearchParams();
  const emaReturn = parseEmaReturn(
    searchParams.get("emaFor"),
    searchParams.get("emaReturn"),
  );
  return (
    <Link
      {...props}
      to={setupHref(oktaViewHref("setup", sectionId, emaReturn))}
    >
      {children}
    </Link>
  );
}
