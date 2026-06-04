import { PolicyAccessRequestsContent } from "@/components/access/PolicyAccessRequestsContent";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";

export default function ApprovalRequests() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="org:admin" level="page">
          <Page.Section>
            <Page.Section.Title stage="beta">
              Policy Access Requests
            </Page.Section.Title>
            <Page.Section.Description>
              Requests created when a risk policy blocks a caller. Approve to
              grant a bypass to a role (narrowed to the blocked server).
            </Page.Section.Description>
            <Page.Section.Body>
              <PolicyAccessRequestsContent />
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
