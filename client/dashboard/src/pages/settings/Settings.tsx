import { Page } from "@/components/page-layout";
import { SettingsLayout } from "@/components/layouts/settings-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import {
  useIsPlatformAdmin,
  useOrganization,
  useProject,
} from "@/contexts/Auth";
import { ShieldAlert } from "lucide-react";
import { Stack } from "@/components/ui/stack";
import { SettingsDangerZone } from "./SettingsDangerZone";
import { RegistryCacheSection } from "./RegistryCacheSection";
import { ModelProviderKeysSection } from "./ModelProviderKeysSection";

export default function Settings(): JSX.Element {
  const isAdmin = useIsPlatformAdmin();
  const organization = useOrganization();
  const project = useProject();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:write" level="page">
          <SettingsLayout>
            <SettingsLayout.Header
              title="Project Settings"
              subtitle="Manage your project configuration and perform administrative actions."
            />
            <SettingsLayout.Body>
              <ModelProviderKeysSection />
              <SettingsDangerZone />

              {isAdmin && (
                <div className="border-destructive-softest bg-destructive-softest mt-8 border p-4">
                  <Stack
                    direction="horizontal"
                    align="center"
                    gap={2}
                    className="mb-3"
                  >
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
            </SettingsLayout.Body>
          </SettingsLayout>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
