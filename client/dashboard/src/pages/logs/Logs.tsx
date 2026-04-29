import { Outlet } from "react-router";
import { LogsContent } from "@/components/observe/LogsContent";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { LogsHooks } from "@/components/observe/LogsHooks";
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

export function LogsHooksPage() {
  return (
    <RequireScope scope={["project:read", "project:write"]} level="page">
      <LogsHooks />
    </RequireScope>
  );
}

export function LogsMCPPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <LogsContent />
    </RequireScope>
  );
}
