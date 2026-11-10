import { SettingsSection } from "@/components/detail/settings-section";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";

const MCP_CALLER_IDENTITY_SECTION_ID = "caller-identity";

export function CallerIdentitySection(): JSX.Element {
  const organization = useOrganization();

  return (
    <SettingsSection id={MCP_CALLER_IDENTITY_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Caller Identity</SettingsSection.Title>
        <SettingsSection.Description>
          Requests forwarded through the tunnel carry a signed
          X-Speakeasy-Identity assertion. If this server is reachable other than
          through the tunnel agent, require the assertion's organization_id
          claim to match this organization ID.
        </SettingsSection.Description>
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
