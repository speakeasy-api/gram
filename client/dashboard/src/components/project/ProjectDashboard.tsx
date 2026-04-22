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
import { useCallback, useEffect, useMemo, type ReactNode } from "react";
import { Badge, Button, Card, Icon } from "@speakeasy-api/moonshine";
import { Wand2 } from "lucide-react";
import {
  INSIGHTS_AI_RAINBOW_CLASS,
  type InsightsConfigOptions,
} from "@/components/insights-sidebar";
import { useInsightsState } from "@/components/insights-context";
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
  } = useFeaturesGet();
  const logsEnabled = featuresData?.logsEnabled === true;

  const { data: overview, isPending: isOverviewPending } =
    useGetProjectOverview({ from, to }, undefined, { enabled: logsEnabled });

  const featuresSettled = !isFeaturesPending || isFeaturesError;
  const isOverviewLoading =
    !featuresSettled || (logsEnabled && isOverviewPending);

  const { data: auditLogsData, isPending: isAuditLogsPending } = useAuditLogs({
    projectSlug,
  });

  const recentLogs = useMemo(
    () => (auditLogsData?.result.logs ?? []).slice(0, 10),
    [auditLogsData],
  );

  const isProjectEmpty =
    logsEnabled &&
    !isOverviewLoading &&
    !isAuditLogsPending &&
    !!overview &&
    overview?.summary?.activeServersCount === 0 &&
    overview?.summary?.totalToolCalls === 0;

  const showDisabledBanner =
    !isFeaturesPending && !isFeaturesError && !logsEnabled;

  const {
    isExpanded: isInsightsExpanded,
    setIsExpanded: setInsightsExpanded,
    setOverride: setInsightsOverride,
    sendPrompt: sendInsightsPrompt,
  } = useInsightsState();

  const exploreWithAI = useCallback(
    (opts: InsightsConfigOptions) => {
      // Apply the override synchronously so it lands in the same commit as
      // setIsExpanded + sendPrompt. Routing through <InsightsConfig> adds a
      // useEffect-deferred setOverride, which (a) loses the chart contextInfo
      // on the first runtime.append call and (b) triggered a click-outside
      // crash via the unmount→cleanup chain.
      setInsightsOverride(opts);
      setInsightsExpanded(true);
      const firstPrompt = opts.suggestions?.[0]?.prompt;
      if (firstPrompt) sendInsightsPrompt(firstPrompt);
    },
    [setInsightsOverride, setInsightsExpanded, sendInsightsPrompt],
  );

  // Clear the per-chart override when the panel is closed so the next opening
  // (e.g. via the header trigger) falls back to the page defaults.
  useEffect(() => {
    if (!isInsightsExpanded) setInsightsOverride(null);
  }, [isInsightsExpanded, setInsightsOverride]);

  // Also clear on unmount: otherwise navigating away with the sidebar still
  // open leaves a stale chart-specific override in InsightsProvider state,
  // which would leak into pages that don't mount their own <InsightsConfig>.
  // Kept as a separate effect so the cleanup fires only on unmount, not on
  // every isInsightsExpanded transition.
  useEffect(() => {
    return () => setInsightsOverride(null);
  }, [setInsightsOverride]);

  const timeWindowContext = `The user is on the Project Overview dashboard. The selected period is the last 7 days (from ${from.toISOString()} to ${to.toISOString()}).`;

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

          {showDisabledBanner && (
            <LoggingDisabledBanner settingsHref={orgRoutes.logs.href()} />
          )}

          {logsEnabled && (
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
                    tooltip="Unique MCP servers that received at least one tool call via hook telemetry in the selected period. Servers with no activity in the window are not counted."
                  />
                )}
                {isOverviewPending ? (
                  <Skeleton className="h-[100px] rounded-lg" />
                ) : (
                  <MetricCard
                    title="Tool Calls"
                    value={overview?.summary.totalToolCalls ?? 0}
                    icon="wrench"
                    tooltip="Total tool invocations recorded across all servers and sources (Elements, MCP, hooks, and the Gram SDK) in the selected period."
                  />
                )}
                {isOverviewPending ? (
                  <Skeleton className="h-[100px] rounded-lg" />
                ) : (
                  <MetricCard
                    title="End Users"
                    value={overview?.summary.activeUsersCount ?? 0}
                    icon="users"
                    tooltip="Unique end users identified during the selected period. When chat sessions exist they are counted from chat messages; otherwise they are counted from tool-call hook events."
                  />
                )}
                {isOverviewPending ? (
                  <Skeleton className="h-[100px] rounded-lg" />
                ) : (
                  <MetricCard
                    title="Sessions"
                    value={overview?.summary.totalChats ?? 0}
                    icon="message-circle"
                    tooltip="Chat sessions started in the selected period across Elements, MCP clients, hooks, and any other source that opens a Gram chat."
                  />
                )}
              </div>

              {/* Row 1: Top Activity */}
              <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
                <DashboardCard
                  title="Top Users"
                  tooltip="End users ranked by activity in the selected period. Activity is measured in tool calls, skill invocations or in chat messages when agent sessions exist."
                  action={
                    <CardActions>
                      <ExploreWithAIButton
                        onClick={() =>
                          exploreWithAI({
                            title: "Analyze your top users",
                            subtitle:
                              "Dig into who is driving the most activity.",
                            contextInfo: `${timeWindowContext} The user clicked "Explore with AI" on the Top Users chart.`,
                            suggestions: [
                              {
                                title: "Top users & usage patterns",
                                label: "Last 7 days",
                                prompt:
                                  "Who are my top 5 end users in the last 7 days, and what is each user's main usage pattern — tool calls, skill invocations, agent sessions, or a mix?",
                              },
                            ],
                          })
                        }
                      />
                      <ViewAllLink to={routes.hooks.href()} />
                    </CardActions>
                  }
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
                  tooltip="MCP servers ranked by the number of tool calls they served in the selected period, based on hook telemetry."
                  action={
                    <CardActions>
                      <ExploreWithAIButton
                        onClick={() =>
                          exploreWithAI({
                            title: "Analyze your top servers",
                            subtitle:
                              "See which MCP servers are driving the most traffic.",
                            contextInfo: `${timeWindowContext} The user clicked "Explore with AI" on the Top Servers chart.`,
                            suggestions: [
                              {
                                title: "Top servers & hot tools",
                                label: "Last 7 days",
                                prompt:
                                  "Which MCP servers received the most tool calls in the last 7 days, and which specific tools on each server are driving that volume?",
                              },
                            ],
                          })
                        }
                      />
                      <ViewAllLink to={routes.hooks.href()} />
                    </CardActions>
                  }
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
                  tooltip="End users ranked by agent activity in the selected period. When chat sessions exist, activity counts chat messages; otherwise it counts tool calls from hook events."
                  action={
                    <CardActions>
                      <ExploreWithAIButton
                        onClick={() =>
                          exploreWithAI({
                            title: "Analyze agent sessions",
                            subtitle:
                              "Understand how your power users interact with agents.",
                            contextInfo: `${timeWindowContext} The user clicked "Explore with AI" on the Most Agent Sessions by User chart.`,
                            suggestions: [
                              {
                                title: "Power users & agent behavior",
                                label: "Last 7 days",
                                prompt:
                                  "For the users with the most agent sessions in the last 7 days, what are the common prompts they send and which tools get invoked most often?",
                              },
                            ],
                          })
                        }
                      />
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
                    </CardActions>
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
                  tooltip="LLM clients (e.g. Claude, Cursor, Windsurf) ranked by activity volume in the selected period, identified from client metadata sent with each call."
                  action={
                    <CardActions>
                      <ExploreWithAIButton
                        onClick={() =>
                          exploreWithAI({
                            title: "Analyze LLM client usage",
                            subtitle:
                              "Compare how different LLM clients exercise your tools.",
                            contextInfo: `${timeWindowContext} The user clicked "Explore with AI" on the Most Used LLM Clients chart.`,
                            suggestions: [
                              {
                                title: "LLM clients & reliability",
                                label: "Last 7 days",
                                prompt:
                                  "Break down tool-call activity by LLM client in the last 7 days and highlight any clients with unusually high error rates or latency.",
                              },
                            ],
                          })
                        }
                      />
                      <ViewAllLink to={routes.hooks.href()} />
                    </CardActions>
                  }
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
            </>
          )}

          <ActivityTimelineCard
            logs={recentLogs}
            isPending={isAuditLogsPending}
            viewAllHref={orgRoutes.auditLogs.href()}
          />
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

function CardActions({ children }: { children: ReactNode }) {
  return <div className="flex items-center gap-3">{children}</div>;
}

function ExploreWithAIButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label="Explore with AI"
      title="Explore with AI"
      className={cn(
        "text-muted-foreground inline-flex items-center justify-center rounded-md p-1 transition-colors",
        INSIGHTS_AI_RAINBOW_CLASS,
      )}
    >
      <Wand2 className="size-3.5" />
    </button>
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
