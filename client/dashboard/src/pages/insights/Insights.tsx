import type { ReactNode } from "react";
import { useMemo } from "react";
import { Outlet, useParams } from "react-router";
import { InsightsAgentsContent } from "@/components/observe/InsightsAgents";
import { InsightsEmployeeDetailContent } from "@/components/observe/InsightsEmployeeDetail";
import { InsightsEmployeesContent } from "@/components/observe/InsightsEmployees";
import { InsightsToolsContent } from "@/components/observe/InsightsTools";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ObserveTabNav } from "@/components/observe/ObserveTabNav";
import { useMembers } from "@gram/client/react-query/members.js";
import { slugify } from "@/lib/constants";

export function InsightsRoot(): JSX.Element {
  return (
    <ObservePageShell>
      <Outlet />
    </ObservePageShell>
  );
}

function employeeBreadcrumbSubstitutions(
  userSlug: string | undefined,
  membersData: ReturnType<typeof useMembers>["data"],
) {
  if (!userSlug) return {};
  const decodedUserSlug = decodeURIComponent(userSlug);
  if (decodedUserSlug.includes("@")) {
    return {
      [userSlug]: decodedUserSlug,
      [encodeURIComponent(decodedUserSlug)]: decodedUserSlug,
    };
  }
  if (!membersData?.members) return {};
  const member = membersData.members.find((m) => slugify(m.name) === userSlug);
  return member ? { [userSlug]: member.email } : {};
}

function ObservePageShell({
  children,
  substitutions,
  tabsBase,
}: {
  children: ReactNode;
  substitutions?: Record<string, string | undefined>;
  tabsBase?: "insights" | "logs";
}) {
  return (
    <div className="flex h-full flex-col">
      {/* ^ Wrapper needed to fill page height, allow inner content scrolls. */}
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs fullWidth substitutions={substitutions} />
        </Page.Header>
        {tabsBase && <ObserveTabNav base={tabsBase} />}
        <Page.Body fullWidth overflowHidden noPadding>
          {children}
        </Page.Body>
      </Page>
    </div>
  );
}

export function InsightsEmployeesLayout(): JSX.Element {
  const { userSlug } = useParams<{ userSlug: string }>();
  const { data: membersData } = useMembers();
  const substitutions = useMemo(
    () => employeeBreadcrumbSubstitutions(userSlug, membersData),
    [userSlug, membersData],
  );

  return (
    <ObservePageShell substitutions={substitutions}>
      <Outlet />
    </ObservePageShell>
  );
}

export function InsightsHooksPage(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <InsightsToolsContent />
    </RequireScope>
  );
}

export function InsightsEmployeesPage(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <InsightsEmployeesContent />
    </RequireScope>
  );
}

export function InsightsEmployeeDetailPage(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <InsightsEmployeeDetailContent />
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
