import { InlineEmptyState } from "@/components/inline-empty-state";
import type { ConnectionTabProps } from "./IdentityProviderArea";
import { OktaConnectionTab } from "./OktaConnectionTab";

/** Setup for this Okta integration, not an organization-wide provider selection. */
export function IdentityProviderTab({
  connection,
  rolloutEnabled,
}: ConnectionTabProps): JSX.Element {
  const needsConnection = !connection || connection.status === "revoked";
  if (needsConnection && !rolloutEnabled) {
    return (
      <InlineEmptyState
        icon="plug"
        heading="Okta setup is not enabled for this organization"
        description="Speakeasy is rolling this out gradually. Ask your Speakeasy contact to enable it."
      />
    );
  }
  return <OktaConnectionTab connection={connection} />;
}
