import { MetricCard } from "@/components/chart/MetricCard";
import { InsightsConfig } from "@/components/insights-sidebar";
import { useInsightsState } from "@/components/insights-context";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
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
import { ShieldCheck, Sparkles, UserRoundCheck } from "lucide-react";
import { useMemo } from "react";

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
  const {
    isExpanded: isInsightsOpen,
    sendPrompt,
    setIsExpanded,
  } = useInsightsState();
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
  const coverage =
    totalEmployees > 0 ? (compliantEmployees / totalEmployees) * 100 : 0;
  const prompt =
    "Using the Employees tab context, summarize whether all employees in this project are compliant based on whether they have any Gram token usage.";

  return (
    <>
      <InsightsConfig
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
        ]}
      />
      <div className="min-h-0 w-full flex-1 overflow-y-auto p-8 pb-24">
        <div className="mx-auto flex max-w-7xl flex-col gap-6">
          <section className="from-card via-card to-muted/40 relative overflow-hidden rounded-2xl border bg-gradient-to-br p-6">
            <div className="pointer-events-none absolute top-0 right-0 h-48 w-48 translate-x-12 -translate-y-16 rounded-full bg-emerald-500/10 blur-3xl" />
            <div className="relative flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
              <div className="max-w-3xl space-y-3">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="border-border bg-background/80 text-muted-foreground inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium">
                    <Sparkles className="size-3.5" />
                    Live data
                  </span>
                  <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-xs font-medium text-emerald-700">
                    <ShieldCheck className="size-3.5" />
                    Enrollment visibility
                  </span>
                </div>
                <div>
                  <p className="text-muted-foreground text-sm font-medium">
                    Employees
                  </p>
                  <h1 className="mt-1 text-3xl font-semibold tracking-tight">
                    Are all employees compliant?
                  </h1>
                </div>
                <p className="text-muted-foreground max-w-2xl text-sm leading-6">
                  Compliance is project-scoped for this first version: if an
                  employee has Gram token usage in the selected project during
                  the last {LOOKBACK_DAYS} days, they are compliant. If no data
                  is present, they are not compliant.
                </p>
              </div>
              <div className="bg-background/90 min-w-[220px] rounded-xl border p-4 shadow-sm">
                <div className="flex items-center gap-3">
                  <div className="rounded-full bg-emerald-500/10 p-2">
                    <UserRoundCheck className="size-5 text-emerald-600" />
                  </div>
                  <div>
                    <p className="text-2xl font-semibold">
                      {compliantEmployees}/{totalEmployees}
                    </p>
                    <p className="text-muted-foreground text-xs">
                      compliant employees
                    </p>
                  </div>
                </div>
                <Button
                  className="mt-4 w-full"
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    setIsExpanded(true);
                    sendPrompt(prompt);
                  }}
                >
                  Ask AI to summarize
                </Button>
              </div>
            </div>
          </section>

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
                  subtext="Token usage present"
                />
                <MetricCard
                  title="Not Compliant"
                  value={notCompliantEmployees}
                  icon="triangle-alert"
                  accentColor="orange"
                  subtext="No token usage found"
                />
                <MetricCard
                  title="Token Count"
                  value={totalTokenCount}
                  icon="gauge"
                  accentColor="purple"
                  subtext={`${coverage.toFixed(0)}% compliance coverage`}
                />
              </section>

              <EmployeeTable employees={employees} />
            </>
          )}
        </div>
      </div>
    </>
  );
}

function EmployeeTable({ employees }: { employees: Employee[] }) {
  return (
    <section className="bg-card rounded-xl border">
      <div className="flex flex-col gap-2 border-b p-5 md:flex-row md:items-start md:justify-between">
        <div>
          <h2 className="text-lg font-semibold">Employee Compliance</h2>
          <p className="text-muted-foreground mt-1 text-sm">
            Live project-scoped compliance based on member roles and Gram token
            usage.
          </p>
        </div>
        <span className="border-border bg-muted text-muted-foreground inline-flex w-fit items-center rounded-full border px-2.5 py-1 text-xs font-medium">
          Last {LOOKBACK_DAYS} days
        </span>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Employee</TableHead>
            <TableHead>Role</TableHead>
            <TableHead>Compliance</TableHead>
            <TableHead>Token Count</TableHead>
            <TableHead>Last Activity</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {employees.length > 0 ? (
            employees.map((item) => (
              <TableRow key={item.id}>
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
      const tokenCount = summary?.totalTokens ?? 0;
      const status: EmployeeStatus =
        tokenCount > 0 ? "compliant" : "not_compliant";

      return {
        id: member.id,
        name: member.name,
        email: member.email,
        role: roleNameById.get(member.roleId) ?? member.roleId,
        status,
        tokenCount,
        lastActivity: summary
          ? formatUnixNano(summary.lastSeenUnixNano)
          : "No token usage found",
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
