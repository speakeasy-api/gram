import { Page } from "@/components/page-layout";
import { Outlet } from "react-router";
import Sources from "@/components/sources/Sources";

export function ToolsetsRoot() {
  return <Outlet />;
}

export default function Toolsets() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Sources />
      </Page.Body>
    </Page>
  );
}
