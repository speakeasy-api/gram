import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
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
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:write" level="page">
          <Page.Section.Title className="mb-2">
            Project Settings
          </Page.Section.Title>
          <Text muted small className="mb-6">
            Manage your project configuration and perform administrative
            actions.
          </Text>
          <div className="mb-8">
            <ModelProviderKeysSection />
          </div>

          <div>
            <SettingsDangerZone />
          </div>

          {isAdmin && (
            <div className="border-destructive-default bg-card mt-8 border p-4">
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
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
