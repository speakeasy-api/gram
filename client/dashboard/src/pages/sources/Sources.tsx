import { Outlet } from "react-router";
import { Page } from "@/components/page-layout";
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
        <SourcesComponent />
      </Page.Body>
    </Page>
  );
}
