import { Outlet } from "react-router";
import { LogsMCPContent } from "@/components/observe/LogsMCP";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { LogsTools } from "@/components/observe/LogsTools";
import { ObserveTabNav } from "@/components/observe/ObserveTabNav";

export function LogsRoot() {
  return (
    <div className="flex h-full flex-col">
      {/* ^ Wrapper needed to fill page height, allow inner content scrolls. */}
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs fullWidth />
        </Page.Header>
        <ObserveTabNav base="logs" />
        <Page.Body fullWidth fullHeight overflowHidden noPadding>
          <Outlet />
        </Page.Body>
      </Page>
    </div>
  );
}

export function LogsToolsPage() {
  return (
    <RequireScope scope={["project:read", "project:write"]} level="page">
      <LogsTools />
    </RequireScope>
  );
}

export function LogsMCPPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <LogsMCPContent />
    </RequireScope>
  );
}
