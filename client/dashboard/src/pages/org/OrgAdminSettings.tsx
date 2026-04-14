import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { useIsAdmin, useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { Building2, ArrowRightLeft } from "lucide-react";
import { Button, Stack } from "@speakeasy-api/moonshine";

export default function OrgAdminSettings() {
  const isAdmin = useIsAdmin();

  if (!isAdmin) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Title>Super Admin</Page.Header.Title>
        </Page.Header>
        <Page.Body>
          <Type muted>You do not have access to this page.</Type>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Title>Super Admin</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        <RequireScope scope="org:admin" level="page">
          <OrgAdminSettingsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

export function OrgAdminSettingsInner() {
  const organization = useOrganization();
  const client = useSdkClient();

  return (
    <>
      <Heading variant="h4" className="mb-2">
        Organization Info
      </Heading>
      <Type muted small className="mb-4">
        Details about the current organization.
      </Type>
      <div className="border-border bg-card mb-8 rounded-lg border p-4">
        <Stack direction="horizontal" align="center" gap={2} className="mb-3">
          <Building2 className="text-muted-foreground h-4 w-4" />
          <Type variant="body" className="font-medium">
            Organization
          </Type>
        </Stack>
        <div className="ml-6 space-y-1">
          <div className="flex gap-3">
            <Type variant="body" className="text-muted-foreground text-sm">
              Slug
            </Type>
            <Type variant="body" className="font-mono text-sm">
              {organization.slug}
            </Type>
          </div>
          <div className="flex gap-3">
            <Type variant="body" className="text-muted-foreground text-sm">
              ID
            </Type>
            <Type variant="body" className="font-mono text-sm">
              {organization.id}
            </Type>
          </div>
        </div>
      </div>

      <Heading variant="h4" className="mb-2">
        Organization Override
      </Heading>
      <Type muted small className="mb-4">
        Impersonate a different organization by switching to its slug. This will
        log you out and redirect you to the target organization.
      </Type>
      <div className="border-border bg-card rounded-lg border p-4">
        <Stack direction="horizontal" align="center" gap={2} className="mb-4">
          <ArrowRightLeft className="text-muted-foreground h-4 w-4" />
          <Type variant="body" className="font-medium">
            Switch Organization
          </Type>
        </Stack>
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
          className="ml-6 flex max-w-md gap-2"
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
      </div>
    </>
  );
}
