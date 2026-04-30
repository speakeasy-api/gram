import { Outlet } from "react-router";
import { AgentInsights } from "@/components/observe/AgentInsights";
import { InsightsHooksContent } from "@/components/observe/InsightsHooksContent";
import { MCPInsights } from "@/components/observe/MCPInsights";
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
      <InsightsHooksContent />
    </RequireScope>
  );
}

export function InsightsMCPPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <MCPInsights />
    </RequireScope>
  );
}

export function InsightsAgentsPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <AgentInsights />
    </RequireScope>
  );
}
