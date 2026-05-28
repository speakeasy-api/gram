import { ApprovalRequestsContent } from "@/components/access/ApprovalRequestsContent";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useProject } from "@/contexts/Auth";

export default function ApprovalRequests() {
  const project = useProject();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="org:admin" level="page">
          <Page.Section>
            <Page.Section.Title stage="beta">
              Approval Requests
            </Page.Section.Title>
            <Page.Section.Description>
              Review blocked resource access requests and manage project-scoped
              access rules.
            </Page.Section.Description>
            <Page.Section.Body>
              <ApprovalRequestsContent projectId={project.id} />
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
