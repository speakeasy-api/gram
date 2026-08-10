import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Switch } from "@/components/ui/Switch";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { AdminRow, AdminSection } from "./AdminSection";
import { PlatformAdminGate } from "./PlatformAdminGate";

const PLATFORM_ADMIN_KEY = "gram-dev-platform-admin";

export default function PlatformAdminOverview(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title area="Platform Admin">
            Overview
          </Page.Section.Title>
          <Page.Section.Description>
            Inspect the organization you are viewing and switch into another
            one.
          </Page.Section.Description>
          <Page.Section.Body>
            <PlatformAdminGate>
              <div className="space-y-8">
                <OrgInfoSection />
                <OrgOverrideSection />
                {import.meta.env.DEV && <PlatformAdminImpersonationSection />}
              </div>
            </PlatformAdminGate>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

function OrgInfoSection(): JSX.Element {
  const organization = useOrganization();

  return (
    <AdminSection title="Organization">
      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 px-4 py-3 text-sm">
        <dt className="text-muted-foreground">Slug</dt>
        <dd className="text-foreground font-mono">{organization.slug}</dd>
        <dt className="text-muted-foreground">ID</dt>
        <dd className="text-foreground font-mono break-all">
          {organization.id}
        </dd>
      </dl>
    </AdminSection>
  );
}

function OrgOverrideSection(): JSX.Element {
  const client = useSdkClient();
  const [slug, setSlug] = useState("");

  const applyOverride = async () => {
    if (!slug.trim()) return;
    await client.auth.logout();
    document.cookie = `gram_admin_override=${slug.trim()}; path=/; max-age=31536000;`;
    window.location.href = "/login";
  };

  const clearOverride = async () => {
    document.cookie = `gram_admin_override=; path=/; max-age=0;`;
    await client.auth.logout();
    window.location.href = "/login";
  };

  return (
    <AdminSection
      title="Organization override"
      description="Impersonate a different organization by switching to its slug. This logs you out and redirects you to the target organization."
    >
      <form
        className="flex items-center gap-2 px-4 py-3"
        onSubmit={(e) => {
          e.preventDefault();
          void applyOverride();
        }}
      >
        <Input
          placeholder="organization-slug"
          name="gram_admin_override"
          value={slug}
          onChange={setSlug}
          required
          className="max-w-xs"
        />
        <Button
          type="button"
          variant="tertiary"
          size="sm"
          onClick={() => void clearOverride()}
        >
          Clear override
        </Button>
        <Button type="submit" size="sm" disabled={!slug.trim()}>
          Go to org
        </Button>
      </form>
    </AdminSection>
  );
}

// Dev-only impersonation toggle: flips useIsPlatformAdmin locally so
// non-admins can exercise admin-gated UI. Not rendered outside local dev.
function PlatformAdminImpersonationSection(): JSX.Element {
  const queryClient = useQueryClient();
  const [platformAdmin, setPlatformAdmin] = useState(
    () => localStorage.getItem(PLATFORM_ADMIN_KEY) === "1",
  );

  return (
    <AdminSection
      title="Development"
      description="Local-only overrides for this browser."
    >
      <AdminRow
        label={platformAdmin ? "Platform admin active" : "Platform admin off"}
        description="Treat this browser as a platform admin so admin-gated UI renders."
        action={
          <Switch
            checked={platformAdmin}
            onCheckedChange={(checked) => {
              setPlatformAdmin(checked);
              localStorage.setItem(PLATFORM_ADMIN_KEY, checked ? "1" : "0");
              void queryClient.invalidateQueries();
            }}
            aria-label="Toggle platform admin"
          />
        }
      />
    </AdminSection>
  );
}
