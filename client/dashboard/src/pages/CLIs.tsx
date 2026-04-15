import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
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
          <div className="bg-muted/20 m-8 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-24">
            <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
              <Icon name="terminal" className="text-muted-foreground h-6 w-6" />
            </div>
            <Type variant="subheading" className="mb-1">
              Skills
            </Type>
            <Type small muted className="mb-4 max-w-md text-center">
              Build and distribute skills with your team. Track usage, enable
              discovery and improve performance.
            </Type>
            <Type small muted>
              Coming soon
            </Type>
          </div>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
