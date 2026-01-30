import { Page } from "@/components/page-layout";
import { useListToolsets } from "@gram/client/react-query/index.js";
import { Outlet } from "react-router";
import Sources from "@/components/sources/Sources";

export function useToolsets() {
  const { data: toolsets, refetch, isLoading } = useListToolsets();
  return Object.assign(toolsets?.toolsets || [], { refetch, isLoading });
}

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
