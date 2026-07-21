import { Link, useNavigate } from "react-router";
import { MetricCard } from "@/components/chart/MetricCard";
import { RankedBarList } from "@/components/chart/RankedBarList";
import { Page } from "@/components/page-layout";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { DashboardCard } from "@/components/ui/dashboard-card";
import { Skeleton } from "@/components/ui/skeleton";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useOrgRoutes, useRoutes } from "@/routes";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useAuditLogs } from "@gram/client/react-query/auditLogs.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { GroupBy } from "@gram/client/models/components/querypayload.js";
import type { QueryMeasures } from "@gram/client/models/components/querymeasures.js";
import type { SearchUsersFilter } from "@gram/client/models/components/searchusersfilter.js";
import type { UserSummary } from "@gram/client/models/components/usersummary.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { cn } from "@/lib/utils";
import { useCallback, useEffect, useMemo, type ReactNode } from "react";
import { Button, Card, Icon } from "@speakeasy-api/moonshine";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { Wand2 } from "lucide-react";
import {
  INSIGHTS_AI_RAINBOW_CLASS,
  type InsightsConfigOptions,
} from "@/components/insights-dock";
import { useInsightsState } from "@/components/insights-context";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { ActivityTimelineCard } from "./ActivityTimelineCard";
import { buildProjectOverviewQuery } from "./projectOverviewQuery";

export function ProjectDashboard(): JSX.Element {
  const { orgSlug, projectSlug } = useSlugs();
  const project = useProject();
  const projectId = project.id;
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();

  const {
    dateRange,
    customRange,
    customRangeLabel,
    from,
    to,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  } = useDateRangeFilter();

  const rangeLabel = useMemo(
    () => formatDateRangeLabel(dateRange, customRangeLabel),
    [dateRange, customRangeLabel],
  );

  const {
    data: featuresData,
    isPending: isFeaturesPending,
    isError: isFeaturesError,
  } = useProductFeatures();
  const logsEnabled = featuresData?.logsEnabled === true;

  // The SDK's useGetProjectOverview omits the request body from its query
  // key; the shared builder keys by org/project/range instead and is also
  // used by the org-home prefetch.
  const client = useGramContext();
  const {
    data: overview,
    isPending: isOverviewPending,
    isFetching: isOverviewFetching,
  } = useQuery({
    ...buildProjectOverviewQuery(client, {
      organization: orgSlug ?? "",
      project: projectSlug ?? "",
      range: customRange
        ? {
            from: customRange.from.toISOString(),
            to: customRange.to.toISOString(),
          }
        : { preset: dateRange },
    }),
    enabled: logsEnabled && !!orgSlug && !!projectSlug,
    placeholderData: keepPreviousData,
  });
  // Cached (possibly stale or previous-range) data is on screen while a
  // refetch runs; the overview cards swap their icon for a spinner.
  const isOverviewRefreshing = isOverviewFetching && !isOverviewPending;

  const { data: membersData, isPending: isMembersPending } = useMembers();
  const members = useMemo(() => membersData?.members ?? [], [membersData]);
  const memberByEmail = useMemo(
    () => new Map(members.map((m) => [m.email, m])),
    [members],
  );

  // Hook/agent-view metrics read the pre-aggregated attribute_metrics_summaries
  // table through the org-scoped telemetry.query endpoint, filtered to this
  // project (project_id is an allowlisted dimension) — the same source as the
  // Costs page, so the numbers agree. This replaces paginating every user
  // through telemetry.searchUsers, which scanned raw telemetry_logs. The
  // generated useTelemetryQuery hook keys its cache on gramSession only
  // (ignores the body), so drive useQuery directly. throwOnError is off so a
  // member without org:read degrades to the MCP view instead of crashing the
  // page on the app-wide error boundary.
  const projectFilter = useMemo(
    () => [{ dimension: Dimension.ProjectId, values: [projectId] }],
    [projectId],
  );

  const {
    data: usageByUserData,
    isPending: isUsageByUserPending,
    isError: isUsageByUserError,
  } = useQuery({
    queryKey: [
      "project",
      "usageByUser",
      projectId,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from,
            to,
            groupBy: GroupBy.Email,
            sortBy: "total_tokens",
            topN: 100,
            filters: projectFilter,
          },
        }),
      ),
    enabled: logsEnabled && !!projectId,
    placeholderData: keepPreviousData,
    throwOnError: false,
  });
  const usageByUserRows = usageByUserData?.table;

  // Mode detection: attribute_metrics_summaries only admits agent-surface
  // telemetry (Claude/Codex/Cursor), so any row with usage means the project
  // has hook data. A project that only hosts MCP servers produces none, and
  // falls back to the MCP-hosting view.
  const hookDataLoaded = usageByUserRows !== undefined || isUsageByUserError;
  const hasHookData = (usageByUserRows ?? []).some((r) => hasUsage(r.measures));

  // Per-user rows suitable for ranking: drop the unattributed '' bucket and
  // the synthetic "Other" rollup. Both still count toward the totals below.
  const rankableUserRows = useMemo(
    () =>
      (usageByUserRows ?? []).filter(
        (r) => r.groupValue !== "" && r.groupValue !== "Other",
      ),
    [usageByUserRows],
  );

  const topUsersByTokens = useMemo(() => {
    return [...rankableUserRows]
      .sort((a, b) => b.measures.totalTokens - a.measures.totalTokens)
      .slice(0, 5)
      .filter((r) => r.measures.totalTokens > 0)
      .map((r) => ({
        key: r.groupValue,
        label: memberByEmail.get(r.groupValue)?.name ?? r.groupValue,
        value: r.measures.totalTokens,
      }));
  }, [rankableUserRows, memberByEmail]);

  // Most Agent Sessions by User ranks the same per-email rows by distinct
  // agent sessions (total_chats = unique gen_ai.conversation.id). Names
  // resolve via the members list; group keys are emails, so raw auth IDs
  // never surface.
  const topUsersBySessions = useMemo(() => {
    return [...rankableUserRows]
      .filter((r) => r.measures.totalChats > 0)
      .sort((a, b) => b.measures.totalChats - a.measures.totalChats)
      .slice(0, 5)
      .map((r) => {
        const member = memberByEmail.get(r.groupValue);
        return {
          userId: r.groupValue,
          name: member?.name ?? r.groupValue,
          initialsSource: r.groupValue,
          sessions: r.measures.totalChats,
        };
      });
  }, [rankableUserRows, memberByEmail]);

  // Total agent sessions = sum of per-user distinct sessions. Each session id
  // (gen_ai.conversation.id) belongs to a single user, so summing per-user
  // counts gives the project-wide distinct-session total. Includes the
  // unattributed '' bucket and the "Other" rollup.
  const totalSessions = (usageByUserRows ?? []).reduce(
    (sum, r) => sum + r.measures.totalChats,
    0,
  );

  // Total Spend sums per-user cost across every row (including '' and
  // "Other"), matching the Costs page's figure for this project since both
  // read the same aggregate.
  const totalSpend = (usageByUserRows ?? []).reduce(
    (sum, r) => sum + r.measures.totalCost,
    0,
  );

  // Most Used Agents: one grouped-by-hook_source read, ranked by token volume.
  // Only fetched in the hook/agent view.
  const { data: usageByAgentData, isPending: isUsageByAgentPending } = useQuery(
    {
      queryKey: [
        "project",
        "usageByAgent",
        projectId,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: () =>
        unwrapAsync(
          telemetryQuery(client, {
            queryPayload: {
              from,
              to,
              groupBy: GroupBy.HookSource,
              sortBy: "total_tokens",
              topN: 10,
              filters: projectFilter,
            },
          }),
        ),
      enabled: logsEnabled && !!projectId && hasHookData,
      placeholderData: keepPreviousData,
      throwOnError: false,
    },
  );

  const mostUsedAgents = useMemo(() => {
    return (usageByAgentData?.table ?? [])
      .filter(
        (r) =>
          r.groupValue !== "" &&
          r.groupValue !== "Other" &&
          r.measures.totalTokens > 0,
      )
      .slice(0, 5)
      .map((r) => ({
        key: r.groupValue,
        label: r.groupValue,
        value: r.measures.totalTokens,
      }));
  }, [usageByAgentData]);

  // MCP-hosting fallback: external end-users (customer-supplied IDs) and their
  // tool-call activity. Fetched only when the project has no hook data. No
  // eventSource filter — these projects' activity is MCP tool calls, not hooks.
  const { data: externalUsersData } = useQuery({
    queryKey: [
      "project",
      "externalUsers",
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchAllUsers(client, { from, to }, "external"),
    enabled: logsEnabled && hookDataLoaded && !hasHookData,
    placeholderData: keepPreviousData,
  });

  // Top end-users by MCP tool-call volume. External IDs are customer-supplied,
  // not Gram members, so they render raw (no member resolution).
  const topEndUsers = useMemo(
    () =>
      [...(externalUsersData ?? [])]
        .sort((a, b) => b.totalToolCalls - a.totalToolCalls)
        .slice(0, 5)
        .map((u) => ({
          key: u.userId,
          label: u.userId,
          value: u.totalToolCalls,
        })),
    [externalUsersData],
  );

  // Most-used tools = aggregate per-user tool breakdowns by URN across all
  // external users (replaces Most Used Agents, which has no MCP equivalent).
  const mostUsedTools = useMemo(() => {
    const byTool = new Map<string, number>();
    for (const u of externalUsersData ?? []) {
      for (const t of u.tools) {
        byTool.set(t.urn, (byTool.get(t.urn) ?? 0) + t.count);
      }
    }
    return [...byTool.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 5)
      .map(([urn, count]) => ({
        key: urn,
        label: toolLabelFromUrn(urn),
        value: count,
      }));
  }, [externalUsersData]);

  // Top tools by failure rate (MCP view): aggregate per-tool call + failure
  // counts across external users; rank by failure rate, tie-broken by absolute
  // failures so a high-volume failing tool outranks a one-off 100% failure.
  const topToolsByFailureRate = useMemo(() => {
    const agg = new Map<string, { calls: number; failures: number }>();
    for (const u of externalUsersData ?? []) {
      for (const t of u.tools) {
        const cur = agg.get(t.urn) ?? { calls: 0, failures: 0 };
        cur.calls += t.count;
        cur.failures += t.failureCount;
        agg.set(t.urn, cur);
      }
    }
    return [...agg.entries()]
      .filter(([, v]) => v.failures > 0)
      .map(([urn, v]) => ({
        key: urn,
        label: toolLabelFromUrn(urn),
        rate: v.calls > 0 ? (v.failures / v.calls) * 100 : 0,
        failures: v.failures,
      }))
      .sort((a, b) => b.rate - a.rate || b.failures - a.failures)
      .slice(0, 5)
      .map((t) => ({
        key: t.key,
        label: t.label,
        // Keep the raw rate for the bar width: every tool here has ≥1 failure,
        // so rounding (e.g. 0.4% → 0) would zero out the bar and the label.
        value: t.rate,
        // Never render "0%" in a failures-only list; show "<1%" below 1%.
        valueLabel: t.rate < 1 ? "<1%" : `${Math.round(t.rate)}%`,
      }));
  }, [externalUsersData]);

  const endUsersCount = externalUsersData?.length ?? 0;

  const isTopUsersLoading =
    logsEnabled &&
    ((isUsageByUserPending && !isUsageByUserError) || isMembersPending);

  // Mode is unknown until the per-user usage fetch + members settle.
  const modePending = isTopUsersLoading;
  // Agent breakdown (hook view) / external users (MCP view) still loading
  // after mode is known.
  const isAgentsLoading = hasHookData && isUsageByAgentPending;
  const mcpUsersPending = !hasHookData && externalUsersData === undefined;

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
    available: insightsDockAvailable,
    isExpanded: isInsightsExpanded,
    setIsExpanded: setInsightsExpanded,
    setOverride: setInsightsOverride,
    sendPrompt: sendInsightsPrompt,
  } = useInsightsState();
  const navigate = useNavigate();

  const exploreWithAI = useCallback(
    (opts: InsightsConfigOptions) => {
      // Apply the override synchronously so it lands in the same commit as
      // setIsExpanded + sendPrompt. Routing through <InsightsConfig> adds a
      // useEffect-deferred setOverride, which (a) loses the chart contextInfo
      // on the first runtime.append call and (b) triggered a click-outside
      // crash via the unmount→cleanup chain.
      setInsightsOverride(opts);
      const firstPrompt = opts.suggestions?.[0]?.prompt;
      // When the dock is hidden (e.g. the home page provides its own chat
      // widget), there's no panel to expand into — drop the user into the
      // full-page chat with the prompt instead.
      if (!insightsDockAvailable) {
        if (firstPrompt) sendInsightsPrompt(firstPrompt);
        void navigate(routes.chat.conversation.href("new"));
        return;
      }
      setInsightsExpanded(true);
      if (firstPrompt) sendInsightsPrompt(firstPrompt);
    },
    [
      insightsDockAvailable,
      setInsightsOverride,
      setInsightsExpanded,
      sendInsightsPrompt,
      navigate,
      routes,
    ],
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

  const timeWindowContext = `The user is on the Project Overview dashboard. The selected period is the ${rangeLabel} (from ${from.toISOString()} to ${to.toISOString()}).`;

  return (
    <Page.Section>
      <Page.Section.Title>Project Overview</Page.Section.Title>
      <Page.Section.CTA>
        {logsEnabled && (
          <TimeRangePicker
            preset={customRange ? null : dateRange}
            customRange={customRange}
            customRangeLabel={customRangeLabel}
            onPresetChange={setDateRangeParam}
            onCustomRangeChange={setCustomRangeParam}
            onClearCustomRange={clearCustomRange}
          />
        )}
      </Page.Section.CTA>

      <Page.Section.Body>
        <div className="space-y-8">
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
                    isRefreshing={isOverviewRefreshing}
                    tooltip="Unique MCP servers used by project members that received at least one tool call in the selected period. Servers with no activity in the window are not counted."
                  />
                )}
                {isOverviewPending ? (
                  <Skeleton className="h-[100px] rounded-lg" />
                ) : (
                  <MetricCard
                    title="Tool Calls"
                    value={overview?.summary.totalToolCalls ?? 0}
                    icon="wrench"
                    isRefreshing={isOverviewRefreshing}
                    tooltip="Total tool invocations recorded across all servers and sources in the selected period."
                  />
                )}
                {modePending || (!hasHookData && mcpUsersPending) ? (
                  <Skeleton className="h-[100px] rounded-lg" />
                ) : hasHookData ? (
                  <MetricCard
                    title="Total Spend"
                    value={totalSpend}
                    format="currency"
                    icon="dollar-sign"
                    tooltip="Total LLM spend recorded for this project in the selected period. Matches the figure on the Costs page."
                  />
                ) : (
                  <MetricCard
                    title="End Users"
                    value={endUsersCount}
                    icon="users"
                    tooltip="Distinct external end users that made MCP tool calls in the selected period."
                  />
                )}
                {modePending || isOverviewPending ? (
                  <Skeleton className="h-[100px] rounded-lg" />
                ) : hasHookData ? (
                  <MetricCard
                    title="Sessions"
                    value={totalSessions}
                    icon="message-circle"
                    tooltip="Distinct agent sessions across project members in the selected period."
                  />
                ) : (
                  <MetricCard
                    title="Failed Tool Calls"
                    value={overview?.summary.failedToolCalls ?? 0}
                    icon="circle-alert"
                    isRefreshing={isOverviewRefreshing}
                    tooltip="MCP tool calls that returned an error (HTTP 4xx/5xx) in the selected period."
                  />
                )}
              </div>

              {/* Row 1: Top Activity */}
              <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
                <DashboardCard
                  title={hasHookData ? "Top Users" : "Top End Users"}
                  tooltip={
                    hasHookData
                      ? "Employees ranked by total token consumption in the selected period."
                      : "External end users ranked by MCP tool calls in the selected period."
                  }
                  action={
                    <CardActions>
                      <ExploreWithAIButton
                        onClick={() =>
                          exploreWithAI({
                            title: "Analyze your top users",
                            subtitle:
                              "Dig into who is driving the most activity.",
                            contextInfo: `${timeWindowContext} The user clicked "Explore with AI" on the Top Users chart.`,
                            suggestions:
                              INSIGHTS_SUGGESTIONS["home#top-users"](
                                rangeLabel,
                              ),
                          })
                        }
                      />
                      <ViewAllLink to={routes.employees.href()} />
                    </CardActions>
                  }
                >
                  {modePending || (!hasHookData && mcpUsersPending) ? (
                    <SkeletonList />
                  ) : (hasHookData ? topUsersByTokens : topEndUsers).length ===
                    0 ? (
                    <EmptyState message="No user activity recorded" />
                  ) : (
                    <RankedBarList
                      items={hasHookData ? topUsersByTokens : topEndUsers}
                    />
                  )}
                </DashboardCard>

                <DashboardCard
                  title="Top Servers"
                  tooltip="Servers ranked by the number of tool calls they served in the selected period, based on logs captured from user sessions in addition to MCP servers hosted in your project."
                  action={
                    <CardActions>
                      <ExploreWithAIButton
                        onClick={() =>
                          exploreWithAI({
                            title: "Analyze your top servers",
                            subtitle:
                              "See which MCP servers are driving the most traffic.",
                            contextInfo: `${timeWindowContext} The user clicked "Explore with AI" on the Top Servers chart.`,
                            suggestions:
                              INSIGHTS_SUGGESTIONS["home#top-servers"](
                                rangeLabel,
                              ),
                          })
                        }
                      />
                      <ViewAllLink to={routes.insights.href()} />
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

              {/* Row 2: Sessions (hook view) / Tools (MCP view) */}
              <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
                {hasHookData ? (
                  <>
                    <DashboardCard
                      title="Most Agent Sessions by User"
                      tooltip="Employees ranked by the number of distinct agent sessions in the selected period."
                      action={
                        <CardActions>
                          <ExploreWithAIButton
                            onClick={() =>
                              exploreWithAI({
                                title: "Analyze agent sessions",
                                subtitle:
                                  "Understand how your power users interact with agents.",
                                contextInfo: `${timeWindowContext} The user clicked "Explore with AI" on the Most Agent Sessions by User chart.`,
                                suggestions:
                                  INSIGHTS_SUGGESTIONS["home#agent-sessions"](
                                    rangeLabel,
                                  ),
                              })
                            }
                          />
                          <ViewAllLink
                            to={
                              // no hooks data and no chat sessions
                              isProjectEmpty &&
                              overview?.summary.totalChats === 0
                                ? routes.insights.href()
                                : // has hooks data but no chat sessions
                                  !isProjectEmpty &&
                                    overview?.summary.totalChats === 0
                                  ? routes.insights.href()
                                  : routes.agentSessions.href()
                            }
                          />
                        </CardActions>
                      }
                    >
                      {isTopUsersLoading ? (
                        <SkeletonList />
                      ) : topUsersBySessions.length === 0 ? (
                        <EmptyState message="No session activity recorded" />
                      ) : (
                        <ul className="divide-border divide-y">
                          {topUsersBySessions.map((user, i) => (
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
                                  {emailInitials(user.initialsSource)}
                                </AvatarFallback>
                              </Avatar>
                              <div className="min-w-0 flex-1">
                                <p className="truncate text-sm font-medium">
                                  {user.name}
                                </p>
                                <p className="text-muted-foreground text-xs">
                                  {user.sessions.toLocaleString()}{" "}
                                  {user.sessions === 1 ? "session" : "sessions"}
                                </p>
                              </div>
                            </li>
                          ))}
                        </ul>
                      )}
                    </DashboardCard>

                    <DashboardCard
                      title="Most Used Agents"
                      tooltip="Agents (e.g. Claude, Cursor, Codex) ranked by token volume in the selected period, identified from client metadata sent with each call."
                      action={
                        <CardActions>
                          <ExploreWithAIButton
                            onClick={() =>
                              exploreWithAI({
                                title: "Analyze LLM client usage",
                                subtitle:
                                  "Compare how different LLM clients exercise your tools.",
                                contextInfo: `${timeWindowContext} The user clicked "Explore with AI" on the Most Used LLM Clients chart.`,
                                suggestions:
                                  INSIGHTS_SUGGESTIONS["home#llm-clients"](
                                    rangeLabel,
                                  ),
                              })
                            }
                          />
                          <ViewAllLink to={routes.insights.href()} />
                        </CardActions>
                      }
                    >
                      {modePending || isAgentsLoading ? (
                        <SkeletonList />
                      ) : mostUsedAgents.length === 0 ? (
                        <EmptyState message="No agent activity recorded" />
                      ) : (
                        <RankedBarList items={mostUsedAgents} />
                      )}
                    </DashboardCard>
                  </>
                ) : (
                  <>
                    <DashboardCard
                      title="Most Used Tools"
                      tooltip="Tools ranked by the number of MCP calls they served in the selected period."
                      action={
                        <CardActions>
                          <ViewAllLink to={routes.insights.href()} />
                        </CardActions>
                      }
                    >
                      {modePending || mcpUsersPending ? (
                        <SkeletonList />
                      ) : mostUsedTools.length === 0 ? (
                        <EmptyState message="No tool activity recorded" />
                      ) : (
                        <RankedBarList items={mostUsedTools} />
                      )}
                    </DashboardCard>

                    <DashboardCard
                      title="Top Tools by Failure Rate"
                      tooltip="Tools with the highest share of failed MCP calls (HTTP 4xx/5xx) in the selected period. Only tools with at least one failure are shown."
                      action={
                        <CardActions>
                          <ViewAllLink to={routes.insights.href()} />
                        </CardActions>
                      }
                    >
                      {modePending || mcpUsersPending ? (
                        <SkeletonList />
                      ) : topToolsByFailureRate.length === 0 ? (
                        <EmptyState message="No tool failures recorded" />
                      ) : (
                        <RankedBarList items={topToolsByFailureRate} />
                      )}
                    </DashboardCard>
                  </>
                )}
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

// A row "has usage" when any measure is nonzero — used to detect whether the
// project has agent (hook) telemetry at all, since attribute_metrics_summaries
// only admits agent-surface rows.
function hasUsage(m: QueryMeasures): boolean {
  return (
    m.totalTokens > 0 ||
    m.totalCost > 0 ||
    m.totalChats > 0 ||
    m.totalToolCalls > 0
  );
}

// Fetch every page of telemetrySearchUsers for the given filter, following the
// pagination cursor. Used by the MCP-hosting fallback view's external-user
// query only — the hook/agent view reads pre-aggregated data via
// telemetry.query instead.
async function fetchAllUsers(
  client: Parameters<typeof telemetrySearchUsers>[0],
  filter: SearchUsersFilter,
  userType: "internal" | "external",
): Promise<UserSummary[]> {
  const users: UserSummary[] = [];
  let cursor: string | undefined;
  for (;;) {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        searchUsersPayload: {
          cursor,
          filter,
          limit: 1000,
          sort: "desc",
          userType,
        },
      }),
    );
    users.push(...result.users);
    if (!result.nextCursor) break;
    cursor = result.nextCursor;
  }
  return users;
}

// Tool URNs look like `tools:externalmcp:<server>:<tool>`; show the trailing
// tool segment, falling back to the full URN.
function toolLabelFromUrn(urn: string): string {
  const parts = urn.split(":");
  return parts[parts.length - 1] || urn;
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
