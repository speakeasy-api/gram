import { Outlet } from "react-router";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import SourcesComponent from "@/components/sources/Sources";

export function SourcesRoot() {
  return <Outlet />;
}

export function SourcesPage() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["build:read", "build:write"]} level="page">
          <SourcesComponent />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
