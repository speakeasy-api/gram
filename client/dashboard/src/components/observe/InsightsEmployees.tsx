import { MetricCard } from "@/components/chart/MetricCard";
import { InsightsConfig } from "@/components/insights-sidebar";
import { useInsightsState } from "@/components/insights-context";
import { ErrorAlert } from "@/components/ui/alert";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import type {
  AccessMember,
  Role,
  UserSummary,
} from "@gram/client/models/components";
import { useGramContext, useMembers, useRoles } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Info, Sparkles } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { useSlugs } from "@/contexts/Sdk";
import { slugify } from "@/lib/constants";

type EmployeeStatus = "compliant" | "not_compliant";

type Employee = {
  id: string;
  name: string;
  email: string;
  role: string;
  status: EmployeeStatus;
  tokenCount: number;
  lastActivity: string;
};

const LOOKBACK_DAYS = 30;

const statusMeta: Record<EmployeeStatus, { label: string; className: string }> =
  {
    compliant: {
      label: "Compliant",
      className: "border-emerald-200 bg-emerald-50 text-emerald-700",
    },
    not_compliant: {
      label: "Not Compliant",
      className: "border-rose-200 bg-rose-50 text-rose-700",
    },
  };

export function InsightsEmployeesContent() {
  const client = useGramContext();
  const { orgSlug, projectSlug } = useSlugs();
  const navigate = useNavigate();
  const {
    isExpanded: isInsightsOpen,
    sendPrompt,
    setIsExpanded,
  } = useInsightsState();
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: ["gram_search_users", "gram_list_organization_users"],
  });
  const {
    data: membersData,
    isLoading: membersLoading,
    error: membersError,
  } = useMembers();
  const { data: rolesData, isLoading: rolesLoading } = useRoles();
  const { from, to } = useMemo(() => {
    const end = new Date();
    const start = new Date(end);
    start.setDate(start.getDate() - LOOKBACK_DAYS);

    return { from: start, to: end };
  }, []);
  const members = useMemo(() => membersData?.members ?? [], [membersData]);
  const roles = useMemo(() => rolesData?.roles ?? [], [rolesData]);
  const memberIds = useMemo(
    () => members.map((member) => member.id),
    [members],
  );
  const usageQuery = useQuery({
    queryKey: [
      "insights",
      "employees",
      "usage",
      from.toISOString(),
      to.toISOString(),
      memberIds,
    ],
    queryFn: () => fetchEmployeeUsage(client, from, to, memberIds),
    enabled: memberIds.length > 0,
    throwOnError: false,
  });
  const usageSummaries = useMemo(
    () => usageQuery.data ?? [],
    [usageQuery.data],
  );
  const isLoading =
    membersLoading ||
    rolesLoading ||
    (memberIds.length > 0 && usageQuery.isLoading);
  const error = membersError ?? usageQuery.error;
  const employees = useMemo(
    () => buildEmployees(members, roles, usageSummaries),
    [members, roles, usageSummaries],
  );
  const totalEmployees = employees.length;
  const compliantEmployees = employees.filter(
    (item) => item.status === "compliant",
  ).length;
  const notCompliantEmployees = totalEmployees - compliantEmployees;
  const totalTokenCount = employees.reduce(
    (sum, item) => sum + item.tokenCount,
    0,
  );
  const employeesBase = `/${orgSlug}/projects/${projectSlug}/insights/employees`;
  const openUser = (name: string) => {
    navigate(`${employeesBase}/${slugify(name)}`);
  };
  const coverage =
    totalEmployees > 0 ? (compliantEmployees / totalEmployees) * 100 : 0;
  const prompt =
    "Using the Employees tab context, summarize whether all employees in this project are compliant based on whether they have any Gram token usage.";

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title="What would you like to know about employee uptake?"
        subtitle="Ask about enrollment, agent setup, and Gram adoption across the team"
        contextInfo={`Project-scoped Employees tab: ${compliantEmployees} of ${totalEmployees} employees have Gram token usage in the last ${LOOKBACK_DAYS} days and are compliant; ${notCompliantEmployees} employees have no Gram token usage and are not compliant.`}
        suggestions={[
          {
            title: "Enrollment Coverage",
            label: "Are all employees compliant?",
            prompt,
          },
          {
            title: "Missing Data",
            label: "Who has no token usage?",
            prompt:
              "Which employees are not compliant because they have no Gram token usage in this project?",
          },
          {
            title: "Compliance Summary",
            label: "Summarize compliance",
            prompt:
              "Summarize project employee compliance based on whether each employee has Gram token usage.",
          },
          {
            title: "User Usage",
            label: "Show user usage",
            prompt:
              "Show me a table of organization users' Gram usage for the last 30 days, including token counts, last activity, and hook source breakdowns.",
          },
        ]}
      />
      <div className="min-h-0 w-full flex-1 overflow-y-auto p-8 pb-24">
        <div className="mx-auto flex max-w-7xl flex-col gap-6">
          <div
            className={cn(
              "flex gap-4 transition-all duration-300",
              isInsightsOpen
                ? "flex-col items-stretch"
                : "flex-row items-center justify-between",
            )}
          >
            <div className="flex min-w-0 flex-col gap-1">
              <h1 className="text-xl font-semibold">Employee Compliance</h1>
              <p className="text-muted-foreground text-sm">
                Track Gram uptake for organization members in this project over
                the last {LOOKBACK_DAYS} days. Employees with hook activity are
                marked compliant; employees without any activity are marked not
                compliant.
              </p>
            </div>
            <div
              className={cn(
                "flex items-center gap-3",
                isInsightsOpen ? "justify-start" : "shrink-0",
              )}
            >
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  setIsExpanded(true);
                  sendPrompt(prompt);
                }}
              >
                <Sparkles className="mr-1.5 size-3.5" />
                Ask AI to summarize
              </Button>
            </div>
          </div>

          {error ? (
            <ErrorAlert
              title="Unable to load employee compliance data"
              error={error}
            />
          ) : isLoading ? (
            <EmployeesLoadingState isInsightsOpen={isInsightsOpen} />
          ) : (
            <>
              <section
                className={cn(
                  "grid gap-4 transition-all duration-300",
                  isInsightsOpen
                    ? "grid-cols-1 md:grid-cols-2"
                    : "grid-cols-1 md:grid-cols-2 lg:grid-cols-4",
                )}
              >
                <MetricCard
                  title="Employees"
                  value={totalEmployees}
                  icon="user"
                  accentColor="blue"
                  subtext="Organization members"
                />
                <MetricCard
                  title="Compliant"
                  value={compliantEmployees}
                  icon="circle-check"
                  accentColor="green"
                  subtext="Hook activity present"
                />
                <MetricCard
                  title="Not Compliant"
                  value={notCompliantEmployees}
                  icon="triangle-alert"
                  accentColor="orange"
                  subtext="No hook activity found"
                />
                <MetricCard
                  title="Token Count"
                  value={totalTokenCount}
                  icon="gauge"
                  accentColor="purple"
                  subtext={`${coverage.toFixed(0)}% compliance coverage`}
                />
              </section>

              <EmployeeTable employees={employees} onSelectUser={openUser} />
            </>
          )}
        </div>
      </div>
    </>
  );
}

const PAGE_SIZE = 25;

function EmployeeTable({
  employees,
  onSelectUser,
}: {
  employees: Employee[];
  onSelectUser: (name: string) => void;
}) {
  const [page, setPage] = useState(0);
  const totalPages = Math.ceil(employees.length / PAGE_SIZE);
  const pageEmployees = employees.slice(
    page * PAGE_SIZE,
    (page + 1) * PAGE_SIZE,
  );

  return (
    <section className="bg-card rounded-xl border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>
              <span className="flex items-center gap-1">
                Employee
                <SimpleTooltip tooltip="Usage is attributed by matching the email reported by each AI coding tool to the member's Gram account. Members without a Gram account won't appear as compliant until they sign up or directory sync is configured.">
                  <Info className="text-muted-foreground size-3 shrink-0" />
                </SimpleTooltip>
              </span>
            </TableHead>
            <TableHead>Role</TableHead>
            <TableHead>Compliance</TableHead>
            <TableHead>Token Count</TableHead>
            <TableHead>Last Activity</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {pageEmployees.length > 0 ? (
            pageEmployees.map((item) => (
              <TableRow
                key={item.id}
                className="cursor-pointer"
                onClick={() => onSelectUser(item.name)}
              >
                <TableCell>
                  <div className="flex items-center gap-3">
                    <div className="bg-muted flex size-9 items-center justify-center rounded-full text-sm font-semibold">
                      {getInitials(item.name)}
                    </div>
                    <div>
                      <p className="font-medium">{item.name}</p>
                      <p className="text-muted-foreground text-xs">
                        {item.email}
                      </p>
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <p className="text-sm">{item.role}</p>
                </TableCell>
                <TableCell>
                  <StatusPill status={item.status} />
                </TableCell>
                <TableCell>
                  <span className="font-mono text-sm">
                    {item.tokenCount.toLocaleString()}
                  </span>
                </TableCell>
                <TableCell className="text-muted-foreground text-sm">
                  {item.lastActivity}
                </TableCell>
              </TableRow>
            ))
          ) : (
            <TableRow>
              <TableCell
                colSpan={5}
                className="text-muted-foreground py-10 text-center text-sm"
              >
                No organization members found.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      {totalPages > 1 && (
        <div className="flex items-center justify-between border-t px-4 py-3">
          <p className="text-muted-foreground text-sm">
            {page * PAGE_SIZE + 1}–
            {Math.min((page + 1) * PAGE_SIZE, employees.length)} of{" "}
            {employees.length}
          </p>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setPage((p) => p - 1)}
              disabled={page === 0}
            >
              <ChevronLeft className="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setPage((p) => p + 1)}
              disabled={page >= totalPages - 1}
            >
              <ChevronRight className="size-4" />
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}

function EmployeesLoadingState({
  isInsightsOpen,
}: {
  isInsightsOpen: boolean;
}) {
  return (
    <>
      <section
        className={cn(
          "grid gap-4 transition-all duration-300",
          isInsightsOpen
            ? "grid-cols-1 md:grid-cols-2"
            : "grid-cols-1 md:grid-cols-2 lg:grid-cols-4",
        )}
      >
        {Array.from({ length: 4 }).map((_, index) => (
          <div key={index} className="bg-card rounded-lg border p-5">
            <Skeleton className="mb-4 h-4 w-28" />
            <Skeleton className="h-9 w-20" />
            <Skeleton className="mt-3 h-3 w-36" />
          </div>
        ))}
      </section>
      <section className="bg-card rounded-xl border p-5">
        <Skeleton className="h-5 w-44" />
        <Skeleton className="mt-2 h-4 w-80" />
        <div className="mt-6 space-y-3">
          {Array.from({ length: 5 }).map((_, index) => (
            <Skeleton key={index} className="h-12 w-full" />
          ))}
        </div>
      </section>
    </>
  );
}

function StatusPill({ status }: { status: EmployeeStatus }) {
  const meta = statusMeta[status];

  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-medium",
        meta.className,
      )}
    >
      {meta.label}
    </span>
  );
}

function getInitials(name: string) {
  return name
    .split(" ")
    .map((part) => part[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

function buildEmployees(
  members: AccessMember[],
  roles: Role[],
  summaries: UserSummary[],
): Employee[] {
  const roleNameById = new Map(roles.map((role) => [role.id, role.name]));
  const summaryByUserId = new Map(
    summaries.map((summary) => [summary.userId, summary]),
  );

  return members
    .map((member) => {
      const summary = summaryByUserId.get(member.id);
      const tokenCount =
        (summary?.totalInputTokens ?? 0) + (summary?.totalOutputTokens ?? 0);
      const status: EmployeeStatus =
        summary != null ? "compliant" : "not_compliant";

      return {
        id: member.id,
        name: member.name,
        email: member.email,
        role: roleNameById.get(member.roleId) ?? member.roleId,
        status,
        tokenCount,
        lastActivity: summary
          ? formatUnixNano(summary.lastSeenUnixNano)
          : "No activity found",
      };
    })
    .sort((a, b) => {
      if (a.status !== b.status) {
        return a.status === "not_compliant" ? -1 : 1;
      }

      return a.name.localeCompare(b.name);
    });
}

async function fetchEmployeeUsage(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
  userIds: string[],
): Promise<UserSummary[]> {
  const users: UserSummary[] = [];
  let cursor: string | undefined;

  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        searchUsersPayload: {
          cursor,
          filter: {
            from,
            to,
            userIds,
            eventSource: "hook",
          },
          limit: 1000,
          sort: "desc",
          userType: "internal",
        },
      }),
    );

    users.push(...result.users);
    cursor = result.nextCursor;
  } while (cursor);

  return users;
}

function formatUnixNano(value: string) {
  const nanos = BigInt(value);
  const millis = Number(nanos / 1_000_000n);

  return dateTimeFormatters.humanize(new Date(millis));
}
