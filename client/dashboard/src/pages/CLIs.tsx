import { Page } from "@/components/page-layout";
import { Type } from "@/components/ui/type";
import { Icon } from "@speakeasy-api/moonshine";

export default function CLIs() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="flex flex-col items-center justify-center py-24 px-8 m-8 rounded-xl border border-dashed bg-muted/20">
          <div className="w-12 h-12 rounded-full bg-muted/50 flex items-center justify-center mb-4">
            <Icon name="terminal" className="w-6 h-6 text-muted-foreground" />
          </div>
          <Type variant="subheading" className="mb-1">
            Skills
          </Type>
          <Type small muted className="text-center mb-4 max-w-md">
            Build and distribute skills with your team. Track usage, enable discovery and improve performance.
            and track usage alongside your MCP insights and logs.
          </Type>
          <Type small muted>
            Coming soon
          </Type>
        </div>
      </Page.Body>
    </Page>
  );
}
