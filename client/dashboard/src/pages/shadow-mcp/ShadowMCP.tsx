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
      <Page.Body>
        <RequireScope scope="org:admin" level="page">
          <Page.Section>
            <Page.Section.Title stage="beta">Shadow MCP</Page.Section.Title>
            <Page.Section.Description>
              Manage project-scoped Shadow MCP server inventory and URL access
              rules.
            </Page.Section.Description>
            <Page.Section.Body>
              <ShadowMCPInventoryTable projectID={project.id} />
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
