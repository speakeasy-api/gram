import type { ReactNode } from "react";
import { Outlet } from "react-router";
import { InsightsAgentsContent } from "@/components/observe/InsightsAgents";
import { InsightsToolsContent } from "@/components/observe/InsightsTools";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";

export function InsightsRoot(): JSX.Element {
  return (
    <ObservePageShell>
      <Outlet />
    </ObservePageShell>
  );
}

function ObservePageShell({
  children,
  substitutions,
}: {
  children: ReactNode;
  substitutions?: Record<string, string | undefined>;
}) {
  return (
    <div className="flex h-full flex-col">
      {/* ^ Wrapper needed to fill page height, allow inner content scrolls. */}
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs fullWidth substitutions={substitutions} />
        </Page.Header>
        <Page.Body fullWidth overflowHidden noPadding>
          {children}
        </Page.Body>
      </Page>
    </div>
  );
}

export function InsightsHooksPage(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <InsightsToolsContent />
    </RequireScope>
  );
}

export function InsightsAgentsPage(): JSX.Element {
  return (
    <ObservePageShell>
      <RequireScope scope="org:admin" level="page">
        <InsightsAgentsContent />
      </RequireScope>
    </ObservePageShell>
  );
}
