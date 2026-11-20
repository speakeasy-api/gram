import { StatTile } from "@/components/chart/stat-tile";
import {
  buildHeatmap,
  computeMovers,
  dailyCost,
  heatmapKey,
} from "./home-widget-data";
import { useInsightsState } from "@/components/insights-context";
import { buildProjectOverviewQuery } from "@/components/project/projectOverviewQuery";
import { Card } from "@/components/ui/Card";
import { Skeleton } from "@/components/ui/Skeleton";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { cn } from "@/lib/utils";
import { Sparkline } from "@/pages/costs/Sparkline";
import { useOrgRoutes, useRoutes } from "@/routes";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { useAccessRequests } from "@gram/client/react-query/accessRequests.js";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRiskSignals } from "@gram/client/react-query/riskSignals.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { formatDistanceToNowStrict } from "date-fns/formatDistanceToNowStrict";
import { ChevronRight } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";

const HOUR_MS = 60 * 60 * 1000;
const DAY_MS = 24 * HOUR_MS;
// A session counts as live when its last message is this recent.
const LIVE_WINDOW_MS = 5 * 60 * 1000;
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

function useLogsEnabled(): { enabled: boolean; pending: boolean } {
  const organization = useOrganization();
  const { data, isPending } = useProductFeatures({
    organizationId: organization.id,
  });
  return { enabled: data?.logsEnabled === true, pending: isPending };
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
    logsOff: !logs.pending && !logs.enabled,
  };
}

/** Widgets around the homepage terminal, in grid order. */
export function HomeWidgets({
  classNames,
}: {
  /** Grid placement for each widget, in order. */
  classNames: [string, string, string, string, string];
}): JSX.Element {
  return (
    <>
      <NeedsAttention className={classNames[0]} />
      <SpendWidget className={classNames[1]} />
      <MoversWidget className={classNames[2]} />
      <LiveSessionsWidget className={classNames[3]} />
      <HeatmapWidget className={classNames[4]} />
    </>
  );
}

// ---------------------------------------------------------------------------
// Needs attention

type AttentionItem = {
  key: string;
  label: string;
  detail: string;
  tone: "destructive" | "warning";
} & ({ prompt: string } | { href: string });

function NeedsAttention({ className }: { className?: string }): JSX.Element {
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  const { hasScope } = useRBAC();
  const { from, to } = useWeekWindow();
  const week = useWeekOverview();
  const isAdmin = hasScope("org:admin");
  const signals = useRiskSignals({ from, to }, undefined, {
    enabled: isAdmin,
    throwOnError: false,
  });
  const accessFlag = useFeatureFlag(FEATURE_FLAGS.accessRequests);
  const requests = useAccessRequests({ status: "pending" }, undefined, {
    enabled: accessFlag.status === "enabled" && isAdmin,
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
            href: routes.watchdog.signal.href(s.ruleId),
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
          href: `${orgRoutes.access.href()}?request=${r.id}`,
        }) satisfies AttentionItem,
    ),
  ].slice(0, 5);

  const pending =
    week.pending || (isAdmin && signals.isPending && !signals.isError);

  return (
    <Card.Dashboard
      title="Needs attention"
      tooltip="High-risk signals, failing tools, and pending access requests from the last 7 days."
      className={cn(WIDGET_CLASS, className)}
      bodyClassName={cn(WIDGET_BODY_CLASS, "p-0")}
    >
      {pending ? (
        <ListSkeleton />
      ) : items.length === 0 ? (
        <p className="text-muted-foreground px-6 py-5 text-sm">
          Nothing needs attention this week.
        </p>
      ) : (
        <ul className="divide-y">
          {items.map((item) => (
            <li key={item.key}>
              <ActionRow
                {...("prompt" in item
                  ? { prompt: item.prompt }
                  : { href: item.href })}
              >
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
                  <span className="block truncate text-sm">{item.label}</span>
                  <span className="text-muted-foreground block truncate text-xs">
                    {item.detail}
                  </span>
                </span>
              </ActionRow>
            </li>
          ))}
        </ul>
      )}
    </Card.Dashboard>
  );
}

// ---------------------------------------------------------------------------
// Spend

function SpendWidget({ className }: { className?: string }): JSX.Element {
  const week = useWeekOverview();
  const { sendPrompt } = useInsightsState();
  const daily = useMemo(
    () => dailyCost(week.data?.timeSeries ?? []),
    [week.data],
  );

  return (
    <Card.Dashboard
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
      ) : (
        <div className="flex flex-col">
          <StatTile
            title="Last 7 days"
            value={week.data?.summary.totalCost ?? 0}
            previousValue={week.data?.comparison.totalCost}
            invertDelta
            format="currency"
            tone="information"
            isLoading={week.pending}
          />
          <div className="px-6 pb-5">
            {week.pending ? (
              <Skeleton className="h-7 w-full" />
            ) : daily.length > 1 ? (
              <button
                type="button"
                // The sparkline draws at a fixed size; scale it to the card.
                className="w-full [&_svg]:h-auto [&_svg]:w-full"
                aria-label="Ask about the spend trend"
                onClick={() =>
                  sendPrompt(
                    "Break down our LLM spend by day for the last 7 days and explain any spikes.",
                  )
                }
              >
                <Sparkline values={daily} width={320} height={48} />
              </button>
            ) : null}
          </div>
        </div>
      )}
    </Card.Dashboard>
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

  return (
    <Card.Dashboard
      title="Biggest movers"
      tooltip="MCP servers whose tool calls changed most in the last 7 days, compared with the 7 days before."
      className={cn(WIDGET_CLASS, className)}
      bodyClassName={cn(WIDGET_BODY_CLASS, "p-0")}
    >
      {!logs.pending && !logs.enabled ? (
        <LogsOff />
      ) : pending ? (
        <ListSkeleton />
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
    </Card.Dashboard>
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
// Live sessions

function LiveSessionsWidget({
  className,
}: {
  className?: string;
}): JSX.Element {
  const routes = useRoutes();
  const chats = useListChats(
    {
      excludeSourceKind: "setup",
      sortBy: "last_message_timestamp",
      sortOrder: "desc",
      limit: 5,
    },
    undefined,
    { refetchInterval: 30_000, throwOnError: false },
  );
  const rows = chats.data?.chats ?? [];
  const now = Date.now();
  const liveCount = rows.filter(
    (c) => now - c.lastMessageTimestamp.getTime() < LIVE_WINDOW_MS,
  ).length;

  return (
    <Card.Dashboard
      title={liveCount > 0 ? `Live now · ${liveCount}` : "Recent sessions"}
      tooltip="The latest agent sessions in this project. Live means a message in the last 5 minutes."
      className={cn(WIDGET_CLASS, className)}
      bodyClassName={cn(WIDGET_BODY_CLASS, "p-0")}
      action={<ViewAllLink to={routes.agentSessions.href()} />}
    >
      {chats.isPending ? (
        <ListSkeleton />
      ) : rows.length === 0 ? (
        <p className="text-muted-foreground px-6 py-5 text-sm">
          No agent sessions yet.
        </p>
      ) : (
        <ul className="divide-y">
          {rows.map((c) => {
            const live =
              now - c.lastMessageTimestamp.getTime() < LIVE_WINDOW_MS;
            const agent =
              c.assistantName ?? c.agentName ?? c.originatingClient ?? "Agent";
            const who = c.accountEmail ?? c.externalUserId;
            const meta = [agent, who, `${c.numMessages} messages`]
              .filter(Boolean)
              .join(" · ");
            return (
              <li key={c.id}>
                <ActionRow
                  prompt={`Summarize the agent session "${c.title || "Untitled"}" (chat ${c.id}): what was asked, which tools ran, and whether anything went wrong.`}
                >
                  <span
                    aria-hidden="true"
                    className={cn(
                      "mt-1.5 size-2 shrink-0 rounded-full",
                      live ? "bg-success-default animate-pulse" : "bg-border",
                    )}
                  />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm">
                      {c.title || "Untitled session"}
                    </span>
                    <span className="text-muted-foreground block truncate text-xs">
                      {meta}
                    </span>
                  </span>
                  <span className="text-muted-foreground shrink-0 text-xs">
                    {live
                      ? "live"
                      : formatDistanceToNowStrict(c.lastMessageTimestamp, {
                          addSuffix: true,
                        })}
                  </span>
                </ActionRow>
              </li>
            );
          })}
        </ul>
      )}
    </Card.Dashboard>
  );
}

// ---------------------------------------------------------------------------
// Heatmap

function HeatmapWidget({ className }: { className?: string }): JSX.Element {
  const week = useWeekOverview();
  const { sendPrompt } = useInsightsState();
  const [hover, setHover] = useState<string | null>(null);

  // Rows are local calendar days (oldest first), columns are local hours.
  const grid = useMemo(
    () => buildHeatmap(week.data?.timeSeries ?? []),
    [week.data],
  );

  return (
    <Card.Dashboard
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
      ) : grid.days.length === 0 ? (
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
                      className="bg-foreground hover:outline-foreground aspect-square hover:outline focus-visible:outline"
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
    </Card.Dashboard>
  );
}

// ---------------------------------------------------------------------------
// Shared bits

/**
 * One row that either asks the terminal (prompt) or opens a page (href).
 * Asking is the default: the terminal is beside every widget.
 */
function ActionRow({
  children,
  prompt,
  href,
}: {
  children: ReactNode;
  prompt?: string;
  href?: string;
}): JSX.Element {
  const { sendPrompt } = useInsightsState();
  const className =
    "group hover:bg-muted/50 flex w-full items-start gap-3 px-6 py-3 text-left";
  const hint = (
    <span className="text-muted-foreground mt-0.5 shrink-0 text-xs opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100">
      {prompt ? "Ask ›" : "Open ›"}
    </span>
  );
  if (href) {
    return (
      <Link to={href} className={cn(className, "no-underline")}>
        {children}
        {hint}
      </Link>
    );
  }
  return (
    <button
      type="button"
      className={className}
      onClick={() => {
        if (prompt) sendPrompt(prompt);
      }}
    >
      {children}
      {hint}
    </button>
  );
}

function AskButton({ prompt }: { prompt: string }): JSX.Element {
  const { sendPrompt } = useInsightsState();
  return (
    <button
      type="button"
      onClick={() => sendPrompt(prompt)}
      className="text-muted-foreground hover:text-foreground text-xs"
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
