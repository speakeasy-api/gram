import { Link } from "react-router";
import { MetricCard } from "@/components/chart/MetricCard";
import { Page } from "@/components/page-layout";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { DashboardCard } from "@/components/ui/dashboard-card";
import { Skeleton } from "@/components/ui/skeleton";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useOrgRoutes, useRoutes } from "@/routes";
import { useAuditLogs, useGetProjectOverview } from "@gram/client/react-query";
import { useFeaturesGet } from "@gram/client/react-query/featuresGet";
import { cn } from "@/lib/utils";
import { subDays } from "date-fns";
import { useMemo } from "react";
import { Badge, Button, Card, Icon } from "@speakeasy-api/moonshine";
import { ActivityTimelineCard } from "./ActivityTimelineCard";
import { ProjectOnboardingBanner } from "./ProjectOnboarding";

export function ProjectDashboard() {
  const { projectSlug } = useSlugs();
  const project = useProject();
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();

  const to = useMemo(() => new Date(), []);
  const from = useMemo(() => subDays(to, 7), [to]);

  const {
    data: featuresData,
    isPending: isFeaturesPending,
    isError: isFeaturesError,
  } = useFeaturesGet(undefined, undefined, { throwOnError: false });
  const logsEnabled = featuresData?.logsEnabled === true;

  const { data: overview, isPending: isOverviewPending } =
    useGetProjectOverview(
      { getProjectMetricsSummaryPayload: { from, to } },
      undefined,
      { enabled: logsEnabled, throwOnError: false },
    );

  const featuresSettled = !isFeaturesPending || isFeaturesError;
  const isOverviewLoading =
    !featuresSettled || (logsEnabled && isOverviewPending);

  const { data: auditLogsData, isPending: isAuditLogsPending } = useAuditLogs(
    { projectSlug },
    undefined,
    { throwOnError: false },
  );

  const recentLogs = useMemo(
    () => (auditLogsData?.result.logs ?? []).slice(0, 10),
    [auditLogsData],
  );

  const isProjectEmpty =
    isFeaturesError ||
    (logsEnabled &&
      !isOverviewLoading &&
      !isAuditLogsPending &&
      !!overview &&
      overview?.summary?.activeServersCount === 0 &&
      overview?.summary?.totalToolCalls === 0);

  const showDisabledBanner =
    !isFeaturesPending && !isFeaturesError && !logsEnabled;

  return (
    <Page.Section>
      <Page.Section.Title>Project Overview</Page.Section.Title>
      <Page.Section.Description>
        <Badge variant="neutral">
          <Badge.Text>{project.name}</Badge.Text>
        </Badge>
      </Page.Section.Description>
      <Page.Section.CTA>
        {logsEnabled && !isProjectEmpty && (
          <p className="text-muted text-xs">
            Showing data from the last 7 days
          </p>
        )}
      </Page.Section.CTA>

      <Page.Section.Body>
        <div className="space-y-8">
          {(isProjectEmpty || showDisabledBanner) && (
            <ProjectOnboardingBanner />
          )}

          {showDisabledBanner ? (
            <LoggingDisabledBanner settingsHref={orgRoutes.logs.href()} />
          ) : isFeaturesError ? null : (
            <>
              {/* Row 0: KPI Cards */}
              <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
                {isOverviewPending ? (
                  <Skeleton className="h-[100px] rounded-lg" />
                ) : (
                  <MetricCard
                    title="Active Servers"
                    value={overview?.summary.activeServersCount ?? 0}
                    icon="server"
                  />
                )}
                {isOverviewPending ? (
                  <Skeleton className="h-[100px] rounded-lg" />
                ) : (
                  <MetricCard
                    title="Tool Calls"
                    value={overview?.summary.totalToolCalls ?? 0}
                    icon="wrench"
                  />
                )}
                {isOverviewPending ? (
                  <Skeleton className="h-[100px] rounded-lg" />
                ) : (
                  <MetricCard
                    title="End Users"
                    value={overview?.summary.activeUsersCount ?? 0}
                    icon="users"
                  />
                )}
                {isOverviewPending ? (
                  <Skeleton className="h-[100px] rounded-lg" />
                ) : (
                  <MetricCard
                    title="Sessions"
                    value={overview?.summary.totalChats ?? 0}
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
                  {isOverviewPending ? (
                    <SkeletonList />
                  ) : (overview?.summary.topUsers.length ?? 0) === 0 ? (
                    <EmptyState message="No user activity recorded" />
                  ) : (
                    <RankedBarList
                      items={(overview?.summary.topUsers ?? [])
                        .slice(0, 5)
                        .map((u) => ({
                          key: u.userId,
                          label: u.userId,
                          value: u.activityCount,
                        }))}
                    />
                  )}
                </DashboardCard>

                <DashboardCard
                  title="Top Servers"
                  action={<ViewAllLink to={routes.hooks.href()} />}
                >
                  {isOverviewPending ? (
                    <SkeletonList />
                  ) : (overview?.summary.topServers.length ?? 0) === 0 ? (
                    <EmptyState message="No server activity recorded" />
                  ) : (
                    <RankedBarList
                      items={(overview?.summary.topServers ?? [])
                        .slice(0, 5)
                        .map((s) => ({
                          key: s.serverName,
                          label: s.serverName,
                          value: s.toolCallCount,
                        }))}
                    />
                  )}
                </DashboardCard>
              </div>

              {/* Row 2: Sessions */}
              <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
                <DashboardCard
                  title="Most Agent Sessions by User"
                  action={
                    <ViewAllLink
                      to={
                        // no hooks data and no chat sessions
                        isProjectEmpty && overview?.summary.totalChats === 0
                          ? routes.hooks.href()
                          : // has hooks data but no chat sessions
                            !isProjectEmpty &&
                              overview?.summary.totalChats === 0
                            ? routes.observability.href()
                            : routes.chatSessions.href()
                      }
                    />
                  }
                >
                  {isOverviewPending ? (
                    <SkeletonList />
                  ) : (overview?.summary.topUsers.length ?? 0) === 0 ? (
                    <EmptyState message="No session activity recorded" />
                  ) : (
                    <ul className="divide-border divide-y">
                      {(overview?.summary.topUsers ?? [])
                        .slice(0, 5)
                        .map((user, i) => (
                          <li
                            key={user.userId}
                            className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0"
                          >
                            <Avatar className="size-8 shrink-0">
                              <AvatarFallback
                                className={cn(
                                  "text-xs font-medium",
                                  avatarColor(i),
                                )}
                              >
                                {emailInitials(user.userId)}
                              </AvatarFallback>
                            </Avatar>
                            <div className="min-w-0 flex-1">
                              <p className="truncate text-sm font-medium">
                                {user.userId}
                              </p>
                              <p className="text-muted-foreground text-xs">
                                {user.activityCount.toLocaleString()} calls
                              </p>
                            </div>
                          </li>
                        ))}
                    </ul>
                  )}
                </DashboardCard>

                <DashboardCard
                  title="Most Used LLM Clients"
                  action={<ViewAllLink to={routes.hooks.href()} />}
                >
                  {isOverviewPending ? (
                    <SkeletonList />
                  ) : (overview?.summary.llmClientBreakdown.length ?? 0) ===
                    0 ? (
                    <EmptyState message="No LLM activity recorded" />
                  ) : (
                    <RankedBarList
                      items={(overview?.summary.llmClientBreakdown ?? [])
                        .slice(0, 5)
                        .map((m) => ({
                          key: m.clientName,
                          label: m.clientName,
                          value: m.activityCount,
                        }))}
                    />
                  )}
                </DashboardCard>
              </div>
              <ActivityTimelineCard
                logs={recentLogs}
                isPending={isAuditLogsPending}
                viewAllHref={orgRoutes.auditLogs.href()}
              />
            </>
          )}
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
      <Icon name="arrow-right" />
    </Link>
  );
}

function LoggingDisabledBanner({ settingsHref }: { settingsHref: string }) {
  return (
    <Card>
      <Card.Content className="flex flex-col items-start gap-6">
        <div className="space-y-1">
          <h3 className="text-lg font-medium">Logging is disabled</h3>
          <p className="text-muted-foreground text-sm">
            Enable logging to see an overview of your project metrics, top
            activity, and session data.
          </p>
        </div>
        <Link to={settingsHref}>
          <Button variant="secondary" size="sm">
            <Button.Text>Enable in settings</Button.Text>
            <Button.RightIcon>
              <Icon name="arrow-right" />
            </Button.RightIcon>
          </Button>
        </Link>
      </Card.Content>
    </Card>
  );
}

type RankedBarListItem = { key: string; label: string; value: number };

function RankedBarList({ items }: { items: RankedBarListItem[] }) {
  const max = items[0]?.value || 1;
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

const AVATAR_COLORS = [
  "bg-blue-100 text-blue-700 dark:bg-blue-950 dark:text-blue-300",
  "bg-violet-100 text-violet-700 dark:bg-violet-950 dark:text-violet-300",
  "bg-teal-100 text-teal-700 dark:bg-teal-950 dark:text-teal-300",
  "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
  "bg-pink-100 text-pink-700 dark:bg-pink-950 dark:text-pink-300",
] as const;

function avatarColor(index: number): string {
  return AVATAR_COLORS[index % AVATAR_COLORS.length]!;
}

function emailInitials(email: string): string {
  const name = email.split("@")[0] ?? "";
  const parts = name.split(/[._-]/).filter(Boolean);
  if (parts.length >= 2) {
    return `${parts[0]![0]}${parts[1]![0]}`.toUpperCase();
  }
  if (parts.length === 1) {
    return parts[0]!.slice(0, 2).toUpperCase();
  }
  return "??";
}
