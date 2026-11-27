import { StatTile } from "@/components/chart/stat-tile";
import type { HomeWidgetId } from "./home-widget-data";
import {
  buildHeatmap,
  computeMovers,
  dailyCost,
  heatmapKey,
} from "./home-widget-data";
import { useTrendColors } from "@/components/chart/useSeriesColors";
import { useInsightsState } from "@/components/insights-context";
import { buildProjectOverviewQuery } from "@/components/project/projectOverviewQuery";
import { Card } from "@/components/ui/Card";
import { Skeleton } from "@/components/ui/Skeleton";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { ToolUsageIdentity } from "@/components/tool-usage-identity";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useReadableAgents } from "@/hooks/useReadableAgents";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { cn } from "@/lib/utils";
import { Sparkline } from "@/pages/costs/Sparkline";
import { useRoutes } from "@/routes";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { telemetryGetToolUsageUsers } from "@gram/client/funcs/telemetryGetToolUsageUsers";
import { useAccessRequests } from "@gram/client/react-query/accessRequests.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRiskSignals } from "@gram/client/react-query/riskSignals.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Maximize2, Minimize2, User } from "lucide-react";
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react";
import { Link } from "react-router";

const HOUR_MS = 60 * 60 * 1000;
const DAY_MS = 24 * HOUR_MS;
// Rows in the most active list.
const MOST_ACTIVE_LIMIT = 5;
// Widgets live in fixed-height grid rows on wide screens: the card clips and
// its body scrolls.
const WIDGET_CLASS = "min-h-0 overflow-hidden";
const WIDGET_BODY_CLASS = "min-h-0 flex-1 overflow-y-auto";
const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

/**
 * The last 168 whole hours. Exactly 7 days keeps the overview's buckets
 * hourly (the server switches to 6h buckets past 168h), and an hour-aligned
 * window keeps the query key stable within the hour.
 */
function useWeekWindow(): { from: Date; to: Date; prevFrom: Date } {
  const hour = Math.floor(Date.now() / HOUR_MS) * HOUR_MS;
  return useMemo(
    () => ({
      from: new Date(hour - 7 * DAY_MS),
      to: new Date(hour),
      prevFrom: new Date(hour - 14 * DAY_MS),
    }),
    [hour],
  );
}

function useLogsEnabled(): {
  enabled: boolean;
  pending: boolean;
  error: boolean;
  /** Confirmed off, not just unknown after a failed request. */
  off: boolean;
} {
  const organization = useOrganization();
  const { data, isPending, isError } = useProductFeatures({
    organizationId: organization.id,
  });
  const enabled = data?.logsEnabled === true;
  return {
    enabled,
    pending: isPending,
    error: isError,
    off: !isPending && !isError && !enabled,
  };
}

function useWeekOverview() {
  const client = useGramContext();
  const { projectSlug } = useSlugs();
  const { from, to } = useWeekWindow();
  const logs = useLogsEnabled();
  const query = useQuery({
    queryKey: ["home", "observability-week", projectSlug, to.toISOString()],
    queryFn: () =>
      unwrapAsync(
        telemetryGetObservabilityOverview(client, {
          getObservabilityOverviewPayload: {
            from,
            to,
            includeTimeSeries: true,
          },
        }),
      ),
    enabled: logs.enabled,
    throwOnError: false,
  });
  return {
    data: query.data,
    pending: logs.pending || (logs.enabled && query.isPending),
    error: logs.error || (logs.enabled && query.isError),
    logsOff: logs.off,
  };
}

type ExpandState = {
  expanded: HomeWidgetId | null;
  onToggle: (id: HomeWidgetId) => void;
};
const ExpandContext = createContext<ExpandState | null>(null);

/** Widgets around the homepage terminal, in grid order. */
export function HomeWidgets({
  classNames,
  expanded = null,
  onToggleExpand,
}: {
  /** Grid placement for each widget, in order. */
  classNames: [string, string, string, string, string];
  /** The widget shown large; the others collapse to their title bars. */
  expanded?: HomeWidgetId | null;
  onToggleExpand?: (id: HomeWidgetId) => void;
}): JSX.Element {
  const expand = useMemo(
    () => (onToggleExpand ? { expanded, onToggle: onToggleExpand } : null),
    [expanded, onToggleExpand],
  );
  return (
    <ExpandContext.Provider value={expand}>
      <NeedsAttention className={classNames[0]} />
      <SpendWidget className={classNames[1]} />
      <MoversWidget className={classNames[2]} />
      <MostActiveWidget className={classNames[3]} />
      <HeatmapWidget className={classNames[4]} />
    </ExpandContext.Provider>
  );
}

// ---------------------------------------------------------------------------
// Needs attention

type AttentionItem = {
  key: string;
  label: string;
  detail: string;
  tone: "destructive" | "warning";
  prompt: string;
};

function NeedsAttention({ className }: { className?: string }): JSX.Element {
  const { hasScope } = useRBAC();
  const { from, to } = useWeekWindow();
  const week = useWeekOverview();
  const isAdmin = hasScope("org:admin");
  const signals = useRiskSignals({ from, to }, undefined, {
    enabled: isAdmin,
    throwOnError: false,
  });
  const accessFlag = useFeatureFlag(FEATURE_FLAGS.accessRequests);
  const requestsEnabled = accessFlag.status === "enabled" && isAdmin;
  const requests = useAccessRequests({ status: "pending" }, undefined, {
    enabled: requestsEnabled,
    throwOnError: false,
  });

  const items: AttentionItem[] = [
    ...(signals.data?.signals ?? [])
      .filter((s) => s.severity === "critical" || s.severity === "high")
      .slice(0, 3)
      .map(
        (s) =>
          ({
            key: `risk:${s.ruleId}`,
            label: s.description,
            detail: `${s.findings.toLocaleString()} findings · ${s.severity}`,
            tone: s.severity === "critical" ? "destructive" : "warning",
            prompt: `Explain the "${s.description}" risk signal: what triggered its ${s.findings.toLocaleString()} findings this week, who is affected, and what should we do about it?`,
          }) satisfies AttentionItem,
      ),
    ...(week.data?.topToolsByFailureRate ?? [])
      .filter((t) => t.failureCount > 0)
      .slice(0, 3)
      .map((t) => {
        const tool = toolName(t.gramUrn);
        return {
          key: `tool:${t.gramUrn}`,
          label: `${tool} is failing`,
          detail: `${Math.round(t.failureRate * 100)}% of ${t.callCount.toLocaleString()} calls failed`,
          tone: "destructive",
          prompt: `Why is the ${tool} tool failing this week? Show the most common errors and who is affected.`,
        } satisfies AttentionItem;
      }),
    ...(requests.data?.requests ?? []).slice(0, 2).map(
      (r) =>
        ({
          key: `access:${r.id}`,
          label: `${r.requesterName} is asking for access`,
          detail: r.resourceName ?? r.scope,
          tone: "warning",
          prompt: `${r.requesterName} is asking for access to ${r.resourceName ?? r.scope}. What would this grant them, and should I approve it?`,
        }) satisfies AttentionItem,
    ),
  ].slice(0, 5);

  const pending =
    week.pending ||
    (isAdmin && signals.isPending && !signals.isError) ||
    (requestsEnabled && requests.isPending && !requests.isError);
  // With a source down, an empty list does not mean all is well.
  const error =
    week.error ||
    (isAdmin && signals.isError) ||
    (requestsEnabled && requests.isError);

  return (
    <HomeCard
      id="attention"
      title="Needs attention"
      tooltip="High-risk signals, failing tools, and pending access requests from the last 7 days."
      className={cn(WIDGET_CLASS, className)}
      bodyClassName={cn(WIDGET_BODY_CLASS, "p-0")}
    >
      {pending ? (
        <ListSkeleton />
      ) : items.length === 0 && error ? (
        <LoadError />
      ) : items.length === 0 ? (
        <p className="text-muted-foreground px-6 py-5 text-sm">
          Nothing needs attention this week.
        </p>
      ) : (
        <ul className="divide-y">
          {items.map((item) => (
            <li key={item.key}>
              <ActionRow prompt={item.prompt}>
                <span
                  aria-hidden="true"
                  className={cn(
                    "mt-1.5 size-2 shrink-0 rounded-full",
                    item.tone === "destructive"
                      ? "bg-destructive-default"
                      : "bg-warning-default",
                  )}
                />
                <span className="min-w-0 flex-1">
                  <span
                    className="line-clamp-3 text-sm break-words"
                    title={item.label}
                  >
                    {item.label}
                  </span>
                  <span className="text-muted-foreground mt-0.5 block text-xs">
                    {item.detail}
                  </span>
                </span>
              </ActionRow>
            </li>
          ))}
        </ul>
      )}
    </HomeCard>
  );
}

// ---------------------------------------------------------------------------
// Spend

function SpendWidget({ className }: { className?: string }): JSX.Element {
  const week = useWeekOverview();
  const sendPrompt = useAskTerminal();
  const client = useGramContext();
  const { projectSlug } = useSlugs();
  const { to, prevFrom } = useWeekWindow();
  const trendColors = useTrendColors();
  // The delta compares this week with last week, so the line spans both
  // weeks; a this-week-only line can fall while the total is up 300%.
  const twoWeeks = useQuery({
    queryKey: ["home", "observability-2w", projectSlug, to.toISOString()],
    queryFn: () =>
      unwrapAsync(
        telemetryGetObservabilityOverview(client, {
          getObservabilityOverviewPayload: {
            from: prevFrom,
            to,
            includeTimeSeries: true,
          },
        }),
      ),
    enabled: !week.logsOff && !week.error,
    throwOnError: false,
  });
  const daily = useMemo(
    () => dailyCost(twoWeeks.data?.timeSeries ?? []),
    [twoWeeks.data],
  );
  // Color the line by the same comparison as the delta beside it.
  const curr = week.data?.summary.totalCost ?? 0;
  const prev = week.data?.comparison.totalCost ?? 0;
  const lineColor =
    trendColors[curr > prev ? "up" : curr < prev ? "down" : "flat"];

  return (
    <HomeCard
      id="spend"
      title="Spend"
      tooltip="LLM spend in the last 7 days, compared with the 7 days before."
      className={cn(WIDGET_CLASS, className)}
      bodyClassName={cn(WIDGET_BODY_CLASS, "p-0")}
      action={
        <AskButton prompt="What is driving our LLM spend this week, and how does it compare with last week?" />
      }
    >
      {week.logsOff ? (
        <LogsOff />
      ) : week.error ? (
        <LoadError />
      ) : (
        // On short viewports the Spend row is too low to stack the chart
        // under the number, so put it beside the number instead.
        <div className="flex flex-col [@media(max-height:1050px)]:flex-row [@media(max-height:1050px)]:items-center">
          <StatTile
            title="Last 7 days"
            value={week.data?.summary.totalCost ?? 0}
            previousValue={week.data?.comparison.totalCost}
            invertDelta
            format="currency"
            tone="information"
            isLoading={week.pending}
          />
          <div className="px-6 pb-5 [@media(max-height:1050px)]:min-w-0 [@media(max-height:1050px)]:flex-1 [@media(max-height:1050px)]:pb-0 [@media(max-height:1050px)]:pl-0">
            {week.pending || twoWeeks.isPending ? (
              <Skeleton className="h-7 w-full" />
            ) : daily.length > 1 ? (
              <button
                type="button"
                // The sparkline draws at a fixed size; scale it to the card.
                className="w-full [&_svg]:h-auto [&_svg]:w-full"
                aria-label="Ask about the spend trend"
                onClick={() =>
                  sendPrompt(
                    "Break down our LLM spend by day for the last 14 days and explain what changed between last week and this week.",
                  )
                }
              >
                <Sparkline
                  values={daily}
                  width={320}
                  height={48}
                  color={lineColor}
                />
              </button>
            ) : null}
          </div>
        </div>
      )}
    </HomeCard>
  );
}

// ---------------------------------------------------------------------------
// Biggest movers

function MoversWidget({ className }: { className?: string }): JSX.Element {
  const client = useGramContext();
  const { orgSlug, projectSlug } = useSlugs();
  const { from, to, prevFrom } = useWeekWindow();
  const logs = useLogsEnabled();
  const scope = (rangeFrom: Date, rangeTo: Date) =>
    buildProjectOverviewQuery(client, {
      organization: orgSlug ?? "",
      project: projectSlug ?? "",
      range: { from: rangeFrom.toISOString(), to: rangeTo.toISOString() },
    });
  const enabled = logs.enabled && !!orgSlug && !!projectSlug;
  const thisWeek = useQuery({ ...scope(from, to), enabled });
  const lastWeek = useQuery({ ...scope(prevFrom, from), enabled });

  const movers = useMemo(
    () =>
      computeMovers(
        lastWeek.data?.summary.topServers ?? [],
        thisWeek.data?.summary.topServers ?? [],
      ),
    [thisWeek.data, lastWeek.data],
  );

  const pending =
    logs.pending || (enabled && (thisWeek.isPending || lastWeek.isPending));
  const error =
    logs.error || (enabled && (thisWeek.isError || lastWeek.isError));

  return (
    <HomeCard
      id="movers"
      title="Biggest movers"
      tooltip="MCP servers whose tool calls changed most in the last 7 days, compared with the 7 days before."
      className={cn(WIDGET_CLASS, className)}
      bodyClassName={cn(WIDGET_BODY_CLASS, "p-0")}
    >
      {logs.off ? (
        <LogsOff />
      ) : pending ? (
        <ListSkeleton />
      ) : error ? (
        <LoadError />
      ) : movers.length === 0 ? (
        <p className="text-muted-foreground px-6 py-5 text-sm">
          No change in server usage this week.
        </p>
      ) : (
        <ul className="divide-y">
          {movers.map((m) => (
            <li key={m.name}>
              <ActionRow
                prompt={`Why did tool calls to the ${m.name} server change from ${m.prev} to ${m.curr} this week? Who and what is driving it?`}
              >
                <span className="min-w-0 flex-1 truncate text-sm">
                  {m.name}
                </span>
                <span className="text-muted-foreground font-mono text-xs tabular-nums">
                  {m.curr.toLocaleString()}
                </span>
                <Delta prev={m.prev} curr={m.curr} />
              </ActionRow>
            </li>
          ))}
        </ul>
      )}
    </HomeCard>
  );
}

function Delta({ prev, curr }: { prev: number; curr: number }): JSX.Element {
  const up = curr > prev;
  // Past 10x a percentage stops reading; show the multiple instead.
  const label =
    prev === 0
      ? "new"
      : curr / prev >= 10
        ? `${Math.round(curr / prev)}×`
        : `${Math.abs(Math.round(((curr - prev) / prev) * 100))}%`;
  return (
    <span
      className={cn(
        // Up and down are not good or bad here, so no status color.
        "text-foreground w-16 text-right font-mono text-xs tabular-nums",
      )}
    >
      {up ? "▲" : "▼"} {label}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Most active identities

function MostActiveWidget({ className }: { className?: string }): JSX.Element {
  const client = useGramContext();
  const routes = useRoutes();
  const { projectSlug } = useSlugs();
  const { from, to } = useWeekWindow();
  const logs = useLogsEnabled();
  const users = useQuery({
    queryKey: ["home", "most-active", projectSlug, to.toISOString()],
    queryFn: () =>
      unwrapAsync(
        telemetryGetToolUsageUsers(client, {
          getToolUsageSummaryPayload: { from, to },
        }),
      ),
    enabled: logs.enabled,
    throwOnError: false,
  });

  const top = useMemo(
    () =>
      [...(users.data?.users ?? [])]
        .sort((a, b) => b.eventCount - a.eventCount)
        .slice(0, MOST_ACTIVE_LIMIT),
    [users.data],
  );
  // Agent rows carry only an id; names come from the agents this viewer can
  // read, and an agent they can't read shows unlinked.
  const agents = useReadableAgents(top.some((r) => r.userKind === "agent_id"));
  const agentsById = useMemo(
    () =>
      new Map(
        (agents.isError ? [] : (agents.data ?? [])).map((a) => [a.id, a]),
      ),
    [agents.data, agents.isError],
  );

  const pending = logs.pending || (logs.enabled && users.isPending);
  const error = logs.error || (logs.enabled && users.isError);

  return (
    <HomeCard
      id="active"
      title="Most active"
      tooltip="The people and agents that made the most tool calls in this project in the last 7 days."
      className={cn(WIDGET_CLASS, className)}
      bodyClassName={cn(WIDGET_BODY_CLASS, "p-0")}
      action={<ViewAllLink to={routes.identities.people.href()} />}
    >
      {logs.off ? (
        <LogsOff />
      ) : pending ? (
        <ListSkeleton />
      ) : error ? (
        <LoadError />
      ) : top.length === 0 ? (
        <p className="text-muted-foreground px-6 py-5 text-sm">
          No one has used this project's tools this week.
        </p>
      ) : (
        <ul className="divide-y">
          {top.map((row) => {
            const isAgent = row.userKind === "agent_id";
            const agent = isAgent ? agentsById.get(row.userKey) : undefined;
            // The id lets the assistant look up an agent the viewer can't name.
            const name = isAgent
              ? `the agent ${agent?.name ?? row.userKey}`
              : row.userLabel;
            return (
              <li
                key={`${row.userKind}:${row.userKey}`}
                className="group hover:bg-muted/50 flex items-center gap-3 px-6 py-3"
              >
                {/* An agent's name already carries its own bot badge. */}
                {!isAgent && (
                  <User
                    aria-label="Person"
                    className="text-muted-foreground size-4 shrink-0"
                  />
                )}
                <span className="min-w-0 flex-1 truncate text-sm">
                  <ToolUsageIdentity
                    kind={row.userKind}
                    identityKey={row.userKey}
                    label={row.userLabel}
                    agent={agent}
                  />
                </span>
                <span className="text-muted-foreground shrink-0 font-mono text-xs tabular-nums">
                  {row.eventCount.toLocaleString()} calls
                </span>
                <AskButton
                  className="opacity-0 group-focus-within:opacity-100 group-hover:opacity-100"
                  prompt={`What has ${name} been doing in this project this week? Which tools and MCP servers did they use, and did anything fail?`}
                />
              </li>
            );
          })}
        </ul>
      )}
    </HomeCard>
  );
}

// ---------------------------------------------------------------------------
// Heatmap

function HeatmapWidget({ className }: { className?: string }): JSX.Element {
  const week = useWeekOverview();
  const sendPrompt = useAskTerminal();
  const [hover, setHover] = useState<string | null>(null);

  // Rows are local calendar days (oldest first), columns are local hours.
  const grid = useMemo(
    () => buildHeatmap(week.data?.timeSeries ?? []),
    [week.data],
  );

  return (
    <HomeCard
      id="heatmap"
      title="Tool calls by hour"
      tooltip="Tool calls in each hour of the last 7 days, in your time zone. Click an hour to ask about it."
      className={cn(WIDGET_CLASS, className)}
      bodyClassName={WIDGET_BODY_CLASS}
    >
      {week.logsOff ? (
        <p className="text-muted-foreground text-sm">
          Turn on logs for this organization to see activity.
        </p>
      ) : week.pending ? (
        <Skeleton className="h-40 w-full" />
      ) : week.error ? (
        <p className="text-muted-foreground text-sm">{LOAD_ERROR}</p>
      ) : grid.total === 0 ? (
        <p className="text-muted-foreground text-sm">
          No tool calls in the last 7 days.
        </p>
      ) : (
        <div className="flex flex-col gap-1">
          {grid.days.map((day) => (
            <div key={day.toDateString()} className="flex items-center gap-2">
              <span className="text-muted-foreground w-12 shrink-0 font-mono text-[10px]">
                {DAYS[day.getDay()]} {day.getDate()}
              </span>
              <div className="grid flex-1 grid-cols-24 gap-px">
                {Array.from({ length: 24 }, (_, hour) => {
                  const calls = grid.cells.get(heatmapKey(day, hour)) ?? 0;
                  const label = `${DAYS[day.getDay()]} ${hour}:00 · ${calls.toLocaleString()} calls`;
                  return (
                    <button
                      key={hour}
                      type="button"
                      aria-label={label}
                      onMouseEnter={() => setHover(label)}
                      onMouseLeave={() => setHover(null)}
                      onFocus={() => setHover(label)}
                      onBlur={() => setHover(null)}
                      onClick={() =>
                        sendPrompt(
                          `What happened in this project on ${day.toDateString()} between ${hour}:00 and ${hour + 1}:00? There were ${calls} tool calls.`,
                        )
                      }
                      // Volume is neither good nor bad, so one neutral hue
                      // (the information blue) rather than red or green.
                      className={cn(
                        "hover:outline-foreground aspect-square hover:outline focus-visible:outline",
                        calls === 0
                          ? "bg-foreground"
                          : "bg-[var(--text-default-information)]",
                      )}
                      style={{
                        opacity:
                          calls === 0 ? 0.06 : 0.15 + (calls / grid.max) * 0.85,
                      }}
                    />
                  );
                })}
              </div>
            </div>
          ))}
          <div className="text-muted-foreground mt-1 flex justify-between pl-14 font-mono text-[10px]">
            <span>00</span>
            <span>06</span>
            <span>12</span>
            <span>18</span>
            <span>23</span>
          </div>
          <p className="text-muted-foreground h-4 font-mono text-xs">
            {hover ?? `Peak ${grid.max.toLocaleString()} calls in an hour`}
          </p>
        </div>
      )}
    </HomeCard>
  );
}

// ---------------------------------------------------------------------------
// Shared bits

/** Asks the terminal beside the widgets, continuing its conversation. */
function useAskTerminal(): (prompt: string) => void {
  const { sendPrompt } = useInsightsState();
  return useCallback(
    (prompt: string) => sendPrompt(prompt, { newConversation: false }),
    [sendPrompt],
  );
}

/**
 * A widget card with an expand toggle. While another widget is expanded this
 * one collapses to its title bar; its toggle then switches the focus to it.
 */
function HomeCard({
  id,
  action,
  className,
  bodyClassName,
  headerClassName,
  ...props
}: { id: HomeWidgetId } & ComponentProps<typeof Card.Dashboard>): JSX.Element {
  const expand = useContext(ExpandContext);
  if (!expand) {
    return (
      <Card.Dashboard
        {...props}
        action={action}
        className={className}
        bodyClassName={bodyClassName}
        headerClassName={headerClassName}
      />
    );
  }
  const isExpanded = expand.expanded === id;
  const collapsed = expand.expanded !== null && !isExpanded;
  return (
    <Card.Dashboard
      {...props}
      className={cn(className, collapsed && "h-auto shrink-0")}
      bodyClassName={cn(bodyClassName, collapsed && "hidden")}
      headerClassName={cn(headerClassName, collapsed && "border-b-0 py-3")}
      action={
        <div className="flex items-center gap-4">
          {!collapsed && action}
          <button
            type="button"
            aria-label={
              isExpanded ? `Collapse ${props.title}` : `Expand ${props.title}`
            }
            aria-pressed={isExpanded}
            onClick={() => expand.onToggle(id)}
            className="text-muted-foreground hover:text-foreground inline-flex items-center"
          >
            {isExpanded ? (
              <Minimize2 className="size-3.5" />
            ) : (
              <Maximize2 className="size-3.5" />
            )}
          </button>
        </div>
      }
    />
  );
}

/** One row that sends its question to the terminal beside every widget. */
function ActionRow({
  children,
  prompt,
}: {
  children: ReactNode;
  prompt: string;
}): JSX.Element {
  const sendPrompt = useAskTerminal();
  return (
    <button
      type="button"
      className="group hover:bg-muted/50 flex w-full items-start gap-3 px-6 py-3 text-left"
      onClick={() => sendPrompt(prompt)}
    >
      {children}
      <span className="text-muted-foreground mt-0.5 shrink-0 text-xs opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100">
        Ask ›
      </span>
    </button>
  );
}

function AskButton({
  prompt,
  className,
}: {
  prompt: string;
  className?: string;
}): JSX.Element {
  const sendPrompt = useAskTerminal();
  return (
    <button
      type="button"
      onClick={() => sendPrompt(prompt)}
      className={cn(
        "text-muted-foreground hover:text-foreground shrink-0 text-xs",
        className,
      )}
    >
      Ask ›
    </button>
  );
}

function ViewAllLink({ to }: { to: string }): JSX.Element {
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

function ListSkeleton(): JSX.Element {
  return (
    <div className="space-y-3 px-6 py-5">
      {Array.from({ length: 4 }).map((_, i) => (
        <Skeleton key={i} className="h-8 w-full" />
      ))}
    </div>
  );
}

const LOAD_ERROR = "Couldn't load this data. Try reloading the page.";

function LoadError(): JSX.Element {
  return (
    <p className="text-muted-foreground px-6 py-5 text-sm">{LOAD_ERROR}</p>
  );
}

function LogsOff(): JSX.Element {
  return (
    <p className="text-muted-foreground px-6 py-5 text-sm">
      Turn on logs for this organization to see activity.
    </p>
  );
}

function toolName(urn: string): string {
  return urn.split(":").pop() || urn;
}
