import { RequireScope } from "@/components/require-scope";
import { useRBAC } from "@/hooks/useRBAC";
import { SettingsPage } from "@/components/page-templates";
import { PlatformAdminOnlyPanel } from "@/components/platform-admin-only-panel";
import { useOrganization, useProject } from "@/contexts/Auth";
import { SettingsDangerZone } from "./SettingsDangerZone";
import { RegistryCacheSection } from "./RegistryCacheSection";
import { ModelProviderKeysSection } from "./ModelProviderKeysSection";
import { ProjectNameSection } from "./ProjectNameSection";
import { BuiltinMcpServersSection } from "./BuiltinMcpServersSection";

export default function Settings(): JSX.Element {
  const organization = useOrganization();
  const project = useProject();
  const { hasScope } = useRBAC();
  const isAdmin = hasScope("org:admin", organization.id);

  return (
    <SettingsPage
      scope={isAdmin ? "org:admin" : "project:write"}
      resourceId={isAdmin ? organization.id : project.id}
      title="Project Settings"
      description="Manage your project configuration and perform administrative actions."
    >
      <RequireScope
        scope="project:write"
        resourceId={project.id}
        level="section"
      >
        <ProjectNameSection />
        <ModelProviderKeysSection />
      </RequireScope>

      <BuiltinMcpServersSection />

      <RequireScope
        scope="project:write"
        resourceId={project.id}
        level="section"
      >
        <SettingsDangerZone />
      </RequireScope>

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
