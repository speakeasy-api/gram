import { Page } from "@/components/page-layout";
import { ProjectDashboard } from "@/components/project/ProjectDashboard";
import { RequireScope } from "@/components/require-scope";

export default function Home() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="build:read" level="page">
          <ProjectDashboard />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
