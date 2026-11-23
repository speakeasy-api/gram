import { SettingsSection } from "@/components/detail/settings-section";
import { TechnicalDetails } from "@/components/technical-details";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";

const MCP_CALLER_IDENTITY_SECTION_ID = "caller-identity";

export function CallerIdentitySection(): JSX.Element {
  const organization = useOrganization();

  return (
    <SettingsSection id={MCP_CALLER_IDENTITY_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Caller identity</SettingsSection.Title>
        <SettingsSection.Description>
          Requests Speakeasy forwards for signed-in callers carry a signature
          naming this organization. If this server can also be reached without
          the tunnel, check that signature.
        </SettingsSection.Description>
        <TechnicalDetails>
          Signed-in requests carry a signed X-Speakeasy-Identity assertion;
          anonymous and background requests carry none. Require its
          organization_id claim to match the organization ID below.
        </TechnicalDetails>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="flex items-center gap-2">
            <Text small muted>
              Organization ID
            </Text>
            <Text small mono>
              {organization.id}
            </Text>
            <CopyButton
              text={organization.id}
              size="sm"
              tooltip="Copy organization ID"
            />
          </div>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Assertions also carry organization_slug for readability. Slugs can
            change, so do not authorize on them.
          </SettingsSection.FooterHint>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
