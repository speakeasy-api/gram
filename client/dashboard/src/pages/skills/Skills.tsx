import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";
import { Icon } from "@speakeasy-api/moonshine";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

/**
 * Skills route entry (`routes.clis`).
 *
 * Placeholder indirection: orgs with the `skills` product feature enabled see
 * the real Skills scaffold (filled out by follow-up frontend work); everyone
 * else keeps the "Coming Soon" placeholder. The route/URL and its nav position
 * are identical in both states — only the page body differs.
 */
export default function Skills(): JSX.Element {
  const { data: features, isLoading } = useProductFeatures();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:read" level="page">
          {/* Wait for the entitlement before choosing which surface to render
              so the placeholder doesn't flash for enabled orgs. */}
          {isLoading ? null : features?.skillsEnabled ? (
            <SkillsScaffold />
          ) : (
            <SkillsComingSoon />
          )}
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

/** Empty scaffold shown to orgs with the Skills feature enabled. */
function SkillsScaffold(): JSX.Element {
  return (
    <Page.Section>
      <Page.Section.Title>Skills</Page.Section.Title>
      <Page.Section.Description>
        Build and distribute skills with your team. Track usage, enable
        discovery and improve performance.
      </Page.Section.Description>
      <Page.Section.Body>{null}</Page.Section.Body>
    </Page.Section>
  );
}

/** "Coming Soon" placeholder shown to orgs without the Skills feature. */
function SkillsComingSoon(): JSX.Element {
  return (
    <Page.Section>
      <Page.Section.Title>Skills</Page.Section.Title>
      <Page.Section.Description>
        Build and distribute skills with your team. Track usage, enable
        discovery and improve performance.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
          <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
            <Icon name="terminal" className="text-muted-foreground h-6 w-6" />
          </div>
          <Type variant="subheading" className="mb-1">
            No skills yet
          </Type>
          <Type small muted className="max-w-md text-center">
            Build and distribute skills to your team. Track usage, enable
            discovery and improve performance.
          </Type>
          <Badge variant="secondary" className="mt-3">
            Coming Soon
          </Badge>
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
