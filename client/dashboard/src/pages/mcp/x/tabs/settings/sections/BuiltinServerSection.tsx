import { SettingsSection } from "@/components/detail/settings-section";

/** The settings of a built-in server, such as the approvals server: Speakeasy
 *  sets it up and runs it, so there is nothing to change here. */
export function BuiltinServerSection(): JSX.Element {
  return (
    <SettingsSection id="built-in">
      <SettingsSection.Header>
        <SettingsSection.Title>Built-in server</SettingsSection.Title>
        <SettingsSection.Description>
          Speakeasy sets up and runs this server, so its settings cannot be
          changed. To keep agents from using it, remove it from your plugins.
        </SettingsSection.Description>
      </SettingsSection.Header>
    </SettingsSection>
  );
}
