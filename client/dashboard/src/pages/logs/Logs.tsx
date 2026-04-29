import { Outlet } from "react-router";
import { LogsContent } from "@/components/observe/LogsContent";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { LogsHooks } from "@/components/observe/LogsHooks";
import { ObserveTabNav } from "@/components/observe/ObserveTabNav";

export function LogsRoot() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
      </Page.Header>
      <ObserveTabNav base="logs" />
      <Page.Body fullWidth noPadding>
        <Outlet />
      </Page.Body>
    </Page>
  );
}

export function LogsHooksPage() {
  return (
    <RequireScope scope="project:read" level="page">
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
