import { SettingsPage } from "@/components/page-templates";
import { PlatformAdminOnlyPanel } from "@/components/platform-admin-only-panel";
import { useOrganization, useProject } from "@/contexts/Auth";
import { SettingsDangerZone } from "./SettingsDangerZone";
import { RegistryCacheSection } from "./RegistryCacheSection";
import { ModelProviderKeysSection } from "./ModelProviderKeysSection";

export default function Settings(): JSX.Element {
  const organization = useOrganization();
  const project = useProject();

  return (
    <SettingsPage
      scope="project:write"
      title="Project Settings"
      description="Manage your project configuration and perform administrative actions."
    >
      <ModelProviderKeysSection />

      <SettingsDangerZone />

      <PlatformAdminOnlyPanel>
        <dl className="mb-4 grid grid-cols-[max-content_auto] gap-x-6 gap-y-2">
          <dt className="text-end">Organization ID</dt>
          <dd className="font-mono text-sm">{organization.id}</dd>
          <dt className="text-end">Project ID</dt>
          <dd className="font-mono text-sm">{project.id}</dd>
        </dl>
        <RegistryCacheSection />
      </PlatformAdminOnlyPanel>
    </SettingsPage>
  );
}
