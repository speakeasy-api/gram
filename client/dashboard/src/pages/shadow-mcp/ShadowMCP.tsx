import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ShadowMCPInventoryTable } from "@/components/shadow-mcp/ShadowMCPInventoryTable";
import { useProject } from "@/contexts/Auth";

export default function ShadowMCP(): JSX.Element {
  const project = useProject();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body fullHeight overflowHidden className="pb-8">
        <RequireScope scope="org:admin" level="page">
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="mb-6 shrink-0">
              <Page.Section.Title stage="beta">Shadow MCP</Page.Section.Title>
              <Page.Section.Description>
                Manage project-scoped Shadow MCP server inventory and URL access
                rules.
              </Page.Section.Description>
            </div>
            <ShadowMCPInventoryTable projectID={project.id} />
          </div>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
