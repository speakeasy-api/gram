import { SettingsPage } from "@/components/page-templates";
import { Heading } from "@/components/ui/Heading";
import {
  useIsPlatformAdmin,
  useOrganization,
  useProject,
} from "@/contexts/Auth";
import { ShieldAlert } from "lucide-react";
import { Stack } from "@/components/ui/Stack";
import { SettingsDangerZone } from "./SettingsDangerZone";
import { RegistryCacheSection } from "./RegistryCacheSection";
import { ModelProviderKeysSection } from "./ModelProviderKeysSection";

export default function Settings(): JSX.Element {
  const isAdmin = useIsPlatformAdmin();
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

      {isAdmin && (
        <div className="border-destructive-default bg-card border p-4">
          <Stack direction="horizontal" align="center" gap={2} className="mb-3">
            <ShieldAlert className="text-default-destructive h-5 w-5" />
            <Heading variant="h4" className="text-default-destructive">
              Platform Admin Only
            </Heading>
          </Stack>
          <dl className="mb-4 grid grid-cols-[max-content_auto] gap-x-6 gap-y-2">
            <dt className="text-end">Organization ID</dt>
            <dd className="font-mono text-sm">{organization.id}</dd>
            <dt className="text-end">Project ID</dt>
            <dd className="font-mono text-sm">{project.id}</dd>
          </dl>
          <RegistryCacheSection />
        </div>
      )}
    </SettingsPage>
  );
}
