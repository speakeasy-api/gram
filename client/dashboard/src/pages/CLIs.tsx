import { EmptyStateCard } from "@/components/empty-state-card";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import { Icon } from "@speakeasy-api/moonshine";

export default function CLIs() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:read" level="page">
          <Page.Section>
            <Page.Section.Title>Skills</Page.Section.Title>
            <Page.Section.Description>
              Build and distribute skills with your team. Track usage, enable
              discovery and improve performance.
            </Page.Section.Description>
            <Page.Section.Body>
              <EmptyStateCard
                icon={<Icon name="terminal" />}
                heading="No skills yet"
                description="Build and distribute skills to your team. Track usage, enable discovery and improve performance."
                cta={<Badge variant="secondary">Coming Soon</Badge>}
              />
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
