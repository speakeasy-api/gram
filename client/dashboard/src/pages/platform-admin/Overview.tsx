import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Switch } from "@/components/ui/Switch";
import { useOrganization } from "@/contexts/Auth";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { AdminRow, AdminSection } from "./AdminSection";
import { OrganizationActivitySection } from "./Activity";
import { PlatformAdminGate } from "./PlatformAdminGate";

const PLATFORM_ADMIN_KEY = "gram-dev-platform-admin";

// localStorage throws when persistence is blocked (sandboxed frames, site
// data disabled); degrade to the off state instead of crashing the page.
function readPlatformAdminOverride(): boolean {
  try {
    return localStorage.getItem(PLATFORM_ADMIN_KEY) === "1";
  } catch {
    return false;
  }
}

function writePlatformAdminOverride(checked: boolean): void {
  try {
    localStorage.setItem(PLATFORM_ADMIN_KEY, checked ? "1" : "0");
  } catch {
    // The in-memory state above still reflects the toggle for this render.
  }
}

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
                <OrganizationActivityOverview />
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

function OrganizationActivityOverview(): JSX.Element {
  const organization = useOrganization();
  return <OrganizationActivitySection organizationId={organization.id} />;
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

export function OrgOverrideSection(): JSX.Element {
  const [slug, setSlug] = useState("");

  return (
    <AdminSection
      title="Organization override"
      description="Start one hour of support access in another organization."
    >
      <form
        className="flex items-center gap-2 px-4 py-3"
        method="post"
        action="/rpc/auth.startSupportSession"
      >
        <Input
          placeholder="organization-slug"
          name="organization_slug"
          value={slug}
          onChange={setSlug}
          required
          className="max-w-xs"
        />
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
  const [platformAdmin, setPlatformAdmin] = useState(readPlatformAdminOverride);

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
              writePlatformAdminOverride(checked);
              void queryClient.invalidateQueries();
            }}
            aria-label="Toggle platform admin"
          />
        }
      />
    </AdminSection>
  );
}
