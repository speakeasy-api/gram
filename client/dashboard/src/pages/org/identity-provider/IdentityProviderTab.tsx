import { Link } from "react-router";
import { SettingsSection } from "@/components/page-templates";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { identityTabHref } from "./identityProviderQueries";
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
  if (needsConnection) return <OktaConnectionTab connection={connection} />;

  return (
    <div className="flex flex-col gap-10">
      <OktaConnectionTab connection={connection} />
      <SettingsSection id="cross-app-access-setup">
        <SettingsSection.Header>
          <SettingsSection.Title>Cross App Access</SettingsSection.Title>
          <SettingsSection.Description>
            Cross App Access is required for Enterprise Managed Auth. Connecting
            Okta and syncing applications alone does not give AI agents access
            to your MCP servers.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <ol className="list-decimal space-y-2 pl-5 text-sm">
              <li>
                For an agent with authentication already configured separately,
                record its ID above to review Cross App Access.
              </li>
              <li>
                Open the Cross App Access tab to review the configuration for
                each MCP server your agents need to use.
              </li>
              <li>
                In Okta, review and reuse an existing connection from your AI
                agent to each resource app (the Okta Integration Network app for
                the service an MCP server connects to), or create one if needed.
              </li>
              <li>
                Return to Speakeasy to record the settings you configured in
                Okta.
              </li>
            </ol>
            <Text muted small>
              Saving a confirmation in Speakeasy does not create or verify an
              Okta connection or prove that access works.
            </Text>
            <div>
              <Button asChild variant="secondary">
                <Link to={identityTabHref("cross-app-access")}>
                  Configure Cross App Access
                </Link>
              </Button>
            </div>
          </SettingsSection.Body>
        </SettingsSection.Panel>
      </SettingsSection>
    </div>
  );
}
