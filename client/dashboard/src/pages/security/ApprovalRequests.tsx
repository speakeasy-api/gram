import { ApprovalRequestsContent } from "@/components/access/ApprovalRequestsContent";
import { ListLayout } from "@/components/layouts/list-layout";
import { Page } from "@/components/page-layout";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { RequireScope } from "@/components/require-scope";
import { useProject } from "@/contexts/Auth";

export default function ApprovalRequests(): JSX.Element {
  const project = useProject();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="org:admin" level="page">
          <ListLayout>
            <ListLayout.Header
              title={
                <span className="inline-flex items-center gap-2">
                  Approval Requests
                  <ReleaseStageBadge stage="beta" />
                </span>
              }
              subtitle="Review blocked resource access requests and manage project-scoped access rules."
            />
            <ListLayout.List>
              <ApprovalRequestsContent projectSlug={project.slug} />
            </ListLayout.List>
          </ListLayout>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
