import { Outlet } from "react-router";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import SourcesComponent from "@/components/sources/Sources";

export function SourcesRoot(): JSX.Element {
  return <Outlet />;
}

export function SourcesPage(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["project:read", "project:write"]} level="page">
          <SourcesComponent />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
