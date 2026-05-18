import { useMemo } from "react";
import { Outlet, useParams } from "react-router";
import { InsightsAgentsContent } from "@/components/observe/InsightsAgents";
import { InsightsEmployeeDetailContent } from "@/components/observe/InsightsEmployeeDetail";
import { InsightsEmployeesContent } from "@/components/observe/InsightsEmployees";
import { InsightsToolsContent } from "@/components/observe/InsightsTools";
import { InsightsMCPContent } from "@/components/observe/InsightsMCP";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ObserveTabNav } from "@/components/observe/ObserveTabNav";
import { useMembers } from "@gram/client/react-query";
import { slugify } from "@/lib/constants";

export function InsightsRoot() {
  const { userSlug } = useParams<{ userSlug: string }>();
  const { data: membersData } = useMembers();
  const substitutions = useMemo(() => {
    if (!userSlug || !membersData?.members) return {};
    const member = membersData.members.find(
      (m) => slugify(m.name) === userSlug,
    );
    return member ? { [userSlug]: member.email } : {};
  }, [userSlug, membersData]);

  return (
    <div className="flex h-full flex-col">
      {/* ^ Wrapper needed to fill page height, allow inner content scrolls. */}
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs fullWidth substitutions={substitutions} />
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

export function InsightsEmployeesLayout() {
  return <Outlet />;
}

export function InsightsEmployeesPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <InsightsEmployeesContent />
    </RequireScope>
  );
}

export function InsightsEmployeeDetailPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <InsightsEmployeeDetailContent />
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
