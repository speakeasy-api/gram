import { Outlet } from "react-router";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import SourcesComponent from "@/components/sources/Sources";

export function SourcesRoot() {
  return <Outlet />;
}

/** Gates the shared Catalog behind project:write when accessed via sources/add-from-catalog. */
export function AddFromCatalogGate() {
  return (
    <RequireScope scope="project:write" level="page">
      <Outlet />
    </RequireScope>
  );
}

export function SourcesPage() {
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
