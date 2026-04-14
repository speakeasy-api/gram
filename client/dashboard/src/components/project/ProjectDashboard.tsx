import { ChevronRight } from "lucide-react";
import { Link } from "react-router";
import { MetricCard } from "@/components/chart/MetricCard";
import { Page } from "@/components/page-layout";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { DashboardCard } from "@/components/ui/dashboard-card";
import { Skeleton } from "@/components/ui/skeleton";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useOrgRoutes, useRoutes } from "@/routes";
import {
  useAuditLogs,
  useGetHooksSummary,
  useGetPeriodUsage,
  useGetProjectMetricsSummary,
  useListFilterOptions,
} from "@gram/client/react-query";
import { FilterType } from "@gram/client/models/components/listfilteroptionspayload";
import { formatDistanceToNow, subDays } from "date-fns";
import { useMemo } from "react";
import { Badge } from "@speakeasy-api/moonshine";

export function ProjectDashboard() {
  const { projectSlug } = useSlugs();
  const project = useProject();
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();

  const to = useMemo(() => new Date(), []);
  const from = useMemo(() => subDays(to, 7), [to]);

  const { data: periodUsage, isPending: isPeriodUsagePending } =
    useGetPeriodUsage();

  const { data: metricsData, isPending: isMetricsPending } =
    useGetProjectMetricsSummary({
      getProjectMetricsSummaryPayload: { from, to },
    });

  const { data: filterOptionsData, isPending: isFilterOptionsPending } =
    useListFilterOptions({
      listFilterOptionsPayload: { filterType: FilterType.User, from, to },
    });

  const { data: hooksSummary, isPending: isHooksPending } = useGetHooksSummary({
    getProjectMetricsSummaryPayload: { from, to },
  });

  const { data: auditLogsData, isPending: isAuditLogsPending } = useAuditLogs({
    projectSlug,
  });

  const topUsers = useMemo(
    () =>
      [...(filterOptionsData?.options ?? [])]
        .sort((a, b) => b.count - a.count)
        .slice(0, 5),
    [filterOptionsData],
  );

  const topServers = useMemo(
    () =>
      [...(hooksSummary?.servers ?? [])]
        .sort((a, b) => b.eventCount - a.eventCount)
        .slice(0, 5),
    [hooksSummary],
  );

  const topUsersByHooks = useMemo(
    () =>
      [...(hooksSummary?.users ?? [])]
        .sort((a, b) => b.eventCount - a.eventCount)
        .slice(0, 5),
    [hooksSummary],
  );

  const topModels = useMemo(
    () =>
      [...(metricsData?.metrics.models ?? [])]
        .sort((a, b) => b.count - a.count)
        .slice(0, 5),
    [metricsData],
  );

  const recentLogs = useMemo(
    () => (auditLogsData?.result.logs ?? []).slice(0, 10),
    [auditLogsData],
  );

  return (
    <Page.Section>
      <Page.Section.Title>Project Overview</Page.Section.Title>
      <Page.Section.Description>
        <Badge variant="neutral">
          <Badge.Text>{project.name}</Badge.Text>
        </Badge>
      </Page.Section.Description>
      <Page.Section.CTA>7-day summary</Page.Section.CTA>

      <Page.Section.Body>
        <div className="space-y-8">
          {/* Row 0: KPI Cards */}
          <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
            {isPeriodUsagePending ? (
              <Skeleton className="h-[100px] rounded-lg" />
            ) : (
              <MetricCard
                title="Active Servers"
                value={periodUsage?.actualEnabledServerCount ?? 0}
                icon="server"
              />
            )}
            {isMetricsPending ? (
              <Skeleton className="h-[100px] rounded-lg" />
            ) : (
              <MetricCard
                title="Tool Calls (7d)"
                value={metricsData?.metrics.totalToolCalls ?? 0}
                icon="wrench"
              />
            )}
            {isFilterOptionsPending ? (
              <Skeleton className="h-[100px] rounded-lg" />
            ) : (
              <MetricCard
                title="End Users (7d)"
                value={filterOptionsData?.options.length ?? 0}
                icon="users"
              />
            )}
            {isMetricsPending ? (
              <Skeleton className="h-[100px] rounded-lg" />
            ) : (
              <MetricCard
                title="Sessions (7d)"
                value={metricsData?.metrics.totalChats ?? 0}
                icon="message-circle"
              />
            )}
          </div>

          {/* Row 1: Top Activity */}
          <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
            <DashboardCard
              title="Top Users"
              action={<ViewAllLink to={routes.hooks.href()} />}
            >
              {isFilterOptionsPending ? (
                <SkeletonList />
              ) : topUsers.length === 0 ? (
                <EmptyState message="No user activity recorded" />
              ) : (
                <RankedBarList
                  items={topUsers.map((u) => ({
                    key: u.id,
                    label: u.label,
                    value: u.count,
                  }))}
                />
              )}
            </DashboardCard>

            <DashboardCard
              title="Top Servers"
              action={<ViewAllLink to={routes.hooks.href()} />}
            >
              {isHooksPending ? (
                <SkeletonList />
              ) : topServers.length === 0 ? (
                <EmptyState message="No server activity recorded" />
              ) : (
                <RankedBarList
                  items={topServers.map((s) => ({
                    key: s.serverName,
                    label: s.serverName,
                    value: s.eventCount,
                  }))}
                />
              )}
            </DashboardCard>
          </div>

          {/* Row 2: Sessions */}
          <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
            <DashboardCard
              title="Agent Sessions by User"
              action={<ViewAllLink to={routes.chatSessions.href()} />}
            >
              {isHooksPending ? (
                <SkeletonList />
              ) : topUsersByHooks.length === 0 ? (
                <EmptyState message="No session activity recorded" />
              ) : (
                <ul className="divide-border divide-y">
                  {topUsersByHooks.map((user) => (
                    <li
                      key={user.userEmail}
                      className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0"
                    >
                      <Avatar className="size-8 shrink-0">
                        <AvatarFallback className="bg-primary/10 text-primary text-xs font-medium">
                          {emailInitials(user.userEmail)}
                        </AvatarFallback>
                      </Avatar>
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium">
                          {user.userEmail}
                        </p>
                        <p className="text-muted-foreground text-xs">
                          {user.eventCount.toLocaleString()} calls &middot;{" "}
                          {user.uniqueTools} tools
                        </p>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </DashboardCard>

            <DashboardCard
              title="Most Used LLM Clients"
              action={<ViewAllLink to={routes.observability.href()} />}
            >
              {isMetricsPending ? (
                <SkeletonList />
              ) : topModels.length === 0 ? (
                <EmptyState message="No LLM activity recorded" />
              ) : (
                <RankedBarList
                  items={topModels.map((m) => ({
                    key: m.name,
                    label: m.name,
                    value: m.count,
                  }))}
                />
              )}
            </DashboardCard>
          </div>

          {/* Row 3: Activity Timeline */}
          <DashboardCard
            title="Audit Log"
            action={<ViewAllLink to={orgRoutes.auditLogs.href()} />}
          >
            {isAuditLogsPending ? (
              <div className="space-y-3">
                {Array.from({ length: 5 }).map((_, i) => (
                  <Skeleton key={i} className="h-8 w-full" />
                ))}
              </div>
            ) : recentLogs.length === 0 ? (
              <EmptyState message="No recent activity" />
            ) : (
              <ul className="divide-border divide-y">
                {recentLogs.map((log) => (
                  <li
                    key={log.id}
                    className="flex items-start gap-3 py-3 first:pt-0 last:pb-0"
                  >
                    <div className="bg-muted-foreground/30 mt-2 h-1.5 w-1.5 shrink-0 rounded-full" />
                    <p className="min-w-0 flex-1 text-sm">
                      <span className="font-medium">
                        {log.actorDisplayName ?? log.actorSlug ?? "Unknown"}
                      </span>{" "}
                      <span className="text-muted-foreground">
                        {log.action}
                      </span>
                      {log.subjectDisplayName && (
                        <>
                          {" "}
                          <span className="font-medium">
                            {log.subjectDisplayName}
                          </span>
                        </>
                      )}
                    </p>
                    <time className="text-muted-foreground shrink-0 text-xs">
                      {formatDistanceToNow(log.createdAt, { addSuffix: true })}
                    </time>
                  </li>
                ))}
              </ul>
            )}
          </DashboardCard>
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}

function ViewAllLink({ to }: { to: string }) {
  return (
    <Link
      to={to}
      className="text-muted-foreground hover:text-foreground flex items-center gap-0.5 text-xs no-underline"
    >
      View all
      <ChevronRight className="size-3" />
    </Link>
  );
}

type RankedBarListItem = { key: string; label: string; value: number };

function RankedBarList({ items }: { items: RankedBarListItem[] }) {
  const max = items[0]?.value ?? 1;
  return (
    <ul className="my-1 space-y-3">
      {items.map((item, i) => (
        <li key={item.key} className="flex items-center gap-3">
          <span className="text-muted-foreground w-4 shrink-0 text-right text-xs">
            {i + 1}
          </span>
          <div className="min-w-0 flex-1">
            <div className="mb-1 flex items-center justify-between">
              <span className="truncate text-sm">{item.label}</span>
              <span className="text-muted-foreground ml-2 shrink-0 text-xs">
                {item.value.toLocaleString()}
              </span>
            </div>
            <div className="bg-muted h-1 w-full rounded-full">
              <div
                className="h-1 rounded-full bg-blue-700 dark:bg-blue-500"
                style={{ width: `${(item.value / max) * 100}%` }}
              />
            </div>
          </div>
        </li>
      ))}
    </ul>
  );
}

function SkeletonList() {
  return (
    <div className="space-y-2">
      {Array.from({ length: 5 }).map((_, i) => (
        <Skeleton key={i} className="h-6 w-full" />
      ))}
    </div>
  );
}

function EmptyState({ message }: { message: string }) {
  return <p className="text-muted-foreground text-sm">{message}</p>;
}

function emailInitials(email: string): string {
  const name = email.split("@")[0] ?? "";
  const parts = name.split(/[._-]/);
  if (parts.length >= 2) {
    return `${parts[0][0]}${parts[1][0]}`.toUpperCase();
  }
  return name.slice(0, 2).toUpperCase();
}
