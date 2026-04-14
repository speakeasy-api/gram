import { MetricCard } from "@/components/chart/MetricCard";
import { Skeleton } from "@/components/ui/skeleton";
import { useSlugs } from "@/contexts/Sdk";
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

export function ProjectDashboard() {
  const { projectSlug } = useSlugs();

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
    <div className="space-y-8">
      {/* Row 0: KPI Cards */}
      <div>
        <h2 className="mb-4 text-lg font-semibold">Overview</h2>
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
      </div>

      {/* Row 1: Top Activity */}
      <div>
        <h2 className="mb-4 text-lg font-semibold">Top Activity (7d)</h2>
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
          <div className="bg-card rounded-lg border p-4">
            <h3 className="mb-3 text-sm font-semibold">Top Users</h3>
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
          </div>

          <div className="bg-card rounded-lg border p-4">
            <h3 className="mb-3 text-sm font-semibold">Top Servers</h3>
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
          </div>
        </div>
      </div>

      {/* Row 2: Sessions */}
      <div>
        <h2 className="mb-4 text-lg font-semibold">Sessions (7d)</h2>
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
          <div className="bg-card rounded-lg border p-4">
            <h3 className="mb-3 text-sm font-semibold">
              Agent Sessions by User
            </h3>
            {isHooksPending ? (
              <SkeletonList />
            ) : topUsersByHooks.length === 0 ? (
              <EmptyState message="No session activity recorded" />
            ) : (
              <ul className="space-y-3">
                {topUsersByHooks.map((user, i) => (
                  <li key={user.userEmail} className="flex items-start gap-3">
                    <span className="text-muted-foreground mt-0.5 w-4 shrink-0 text-right text-xs">
                      {i + 1}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between">
                        <span className="truncate text-sm">
                          {user.userEmail}
                        </span>
                        <span className="text-muted-foreground ml-2 shrink-0 text-xs">
                          {user.eventCount.toLocaleString()} events
                        </span>
                      </div>
                      <span className="text-muted-foreground text-xs">
                        {user.uniqueTools} tools used
                      </span>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="bg-card rounded-lg border p-4">
            <h3 className="mb-3 text-sm font-semibold">
              Most Used LLM Clients
            </h3>
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
          </div>
        </div>
      </div>

      {/* Row 3: Activity Timeline */}
      <div>
        <h2 className="mb-4 text-lg font-semibold">Recent Activity</h2>
        <div className="bg-card rounded-lg border p-4">
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
                    <span className="text-muted-foreground">{log.action}</span>
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
        </div>
      </div>
    </div>
  );
}

type RankedBarListItem = { key: string; label: string; value: number };

function RankedBarList({ items }: { items: RankedBarListItem[] }) {
  const max = items[0]?.value ?? 1;
  return (
    <ul className="space-y-2">
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
                className="bg-primary h-1 rounded-full"
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
