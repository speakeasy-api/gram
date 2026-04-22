import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";
import { Icon } from "@speakeasy-api/moonshine";

export default function CLIs() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="build:read" level="page">
          <Page.Section>
            <Page.Section.Title>Skills</Page.Section.Title>
            <Page.Section.Description>
              Build and distribute skills with your team. Track usage, enable
              discovery and improve performance.
            </Page.Section.Description>
            <Page.Section.Body>
              <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
                <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
                  <Icon
                    name="terminal"
                    className="text-muted-foreground h-6 w-6"
                  />
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
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
