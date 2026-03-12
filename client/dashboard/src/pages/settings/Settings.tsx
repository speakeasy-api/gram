import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { useIsAdmin, useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { ShieldAlert } from "lucide-react";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { SettingsDangerZone } from "./SettingsDangerZone";
import { RegistryCacheSection } from "./RegistryCacheSection";

export default function Settings() {
  const organization = useOrganization();
  const isAdmin = useIsAdmin();
  const client = useSdkClient();
  const project = useProject();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Title>Project Settings</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        <Heading variant="h4" className="mb-2">
          Project Settings
        </Heading>
        <Type muted small className="mb-6">
          Manage your project configuration and perform administrative actions.
        </Type>
        <SettingsDangerZone />

        {isAdmin && (
          <div className="mt-8 p-4 rounded-lg bg-red-500/5 border border-red-500/20">
            <Stack
              direction="horizontal"
              align="center"
              gap={2}
              className="mb-3"
            >
              <ShieldAlert className="w-5 h-5 text-red-500" />
              <Heading variant="h4" className="text-red-600 dark:text-red-400">
                Admin Only
              </Heading>
            </Stack>
            <dl className="grid grid-cols-[max-content_auto] gap-x-6 gap-y-2 mb-8">
              <dt className="text-end">Organization ID</dt>
              <dd className="font-mono">{organization.id}</dd>
              <dt className="text-end">Project ID</dt>
              <dd className="font-mono">{project.id}</dd>
            </dl>

            <Type variant="body" className="text-muted-foreground mb-4">
              Override to a different organization by entering its slug below.
            </Type>
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                const formData = new FormData(e.currentTarget);
                const val = formData.get("gram_admin_override");
                if (typeof val !== "string" || !val.trim()) {
                  return;
                }

                document.cookie = `gram_admin_override=${val.trim()}; path=/; max-age=31536000;`;
                await client.auth.logout();
                window.location.href = "/login";
              }}
              className="flex gap-2 max-w-md"
            >
              <Input
                placeholder="organization-slug"
                name="gram_admin_override"
                className="flex-1"
                required
              />
              <Button type="submit">Go to Org</Button>
              <Button
                variant="secondary"
                type="button"
                onClick={async () => {
                  document.cookie = `gram_admin_override=; path=/; max-age=0;`;
                  await client.auth.logout();
                  window.location.href = "/login";
                }}
              >
                Clear override
              </Button>
            </form>

            <RegistryCacheSection />
          </div>
        )}
      </Page.Body>
    </Page>
  );
}
