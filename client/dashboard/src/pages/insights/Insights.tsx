import { Outlet } from "react-router";
import { InsightsAgentsContent } from "@/components/observe/InsightsAgents";
import { InsightsToolsContent } from "@/components/observe/InsightsTools";
import { InsightsMCPContent } from "@/components/observe/InsightsMCP";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ObserveTabNav } from "@/components/observe/ObserveTabNav";

export function InsightsRoot() {
  return (
    <div className="flex h-full flex-col">
      {/* ^ Wrapper needed to fill page height, allow inner content scrolls. */}
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs fullWidth />
        </Page.Header>
        <ObserveTabNav base="insights" />
        <Page.Body fullWidth overflowHidden noPadding>
          <Outlet />
        </Page.Body>
      </Page>
    </div>
  );
}

export function InsightsHooksPage() {
  return (
    <RequireScope scope={["project:read", "project:write"]} level="page">
      <InsightsToolsContent />
    </RequireScope>
  );
}

export function InsightsMCPPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <InsightsMCPContent />
    </RequireScope>
  );
}

export function InsightsAgentsPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <InsightsAgentsContent />
    </RequireScope>
  );
}
