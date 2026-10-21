import { ChartCard } from "@/components/chart/ChartCard";
import {
  chartColors,
  EXPANDED_LEGEND,
  SHARED_TOOLTIP,
} from "@/components/chart/chartTheme";
import { ACCENT_RED } from "@/components/chart/palette";
import { RankedBarList } from "@/components/chart/RankedBarList";
import {
  useIsDarkTheme,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { StatRow } from "@/components/stat-row";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import { Table } from "@/components/ui/Table";
import { cn } from "@/lib/utils";
import type { TunnelDiagnostics } from "@gram/client/models/components/tunneldiagnostics.js";
import type { TunnelDiagnosticStep } from "@gram/client/models/components/tunneldiagnosticstep.js";
import type { TunneledMcpConnection } from "@gram/client/models/components/tunneledmcpconnection.js";
import type { TunneledMcpServerConnections } from "@gram/client/models/components/tunneledmcpserverconnections.js";
import type { TunnelMetricPoint } from "@gram/client/models/components/tunnelmetricpoint.js";
import type { TunnelMetrics } from "@gram/client/models/components/tunnelmetrics.js";
import { useGetTunneledMcpServerMetrics } from "@gram/client/react-query/getTunneledMcpServerMetrics.js";
import {
  CategoryScale,
  Chart as ChartJS,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  type ChartOptions,
} from "chart.js";
import { formatDistanceToNow } from "date-fns";
import { ChevronRight } from "lucide-react";
import { useState } from "react";
import { Line } from "react-chartjs-2";
import { Link } from "react-router";
import { liveTunnelConnections, tunnelStatus, type Tone } from "./tunnelStatus";

ChartJS.register(
  CategoryScale,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  Legend,
);

const windows = [
  { value: "hour", label: "1 hour" },
  { value: "day", label: "24 hours" },
  { value: "week", label: "7 days" },
] as const;
type Window = (typeof windows)[number]["value"];

const windowPhrase: Record<Window, string> = {
  hour: "in the last hour",
  day: "in the last 24 hours",
  week: "in the last 7 days",
};

// Longer windows change slowly, so they refresh less often.
const historyRefetchMs: Record<Window, number> = {
  hour: 30_000,
  day: 5 * 60_000,
  week: 15 * 60_000,
};

// The agent reports its slowest latency bin as a negative number; the chart
// pins it just above the 60-second bound so the line stays on the axis.
const OVER_SIXTY_SECONDS = 60_001;

// Lines draw no point markers, so a value with no neighbour on either side
// would be invisible. Only those isolated values get a marker.
const ISOLATED_POINT_RADIUS = 3;
function isolatedPointRadii(values: (number | null)[]): number[] {
  return values.map((value, i) =>
    value != null && values[i - 1] == null && values[i + 1] == null
      ? ISOLATED_POINT_RADIUS
      : 0,
  );
}

export function TunnelObservability({
  id,
  connections,
  loading,
  error,
  logsHref,
  agentSetupHref,
}: {
  id: string;
  connections?: TunneledMcpServerConnections;
  loading: boolean;
  error: boolean;
  logsHref: string;
  agentSetupHref: string;
}): JSX.Element {
  const [window, setWindow] = useState<Window>("day");
  const history = useGetTunneledMcpServerMetrics({ id, window }, undefined, {
    enabled: !!id,
    throwOnError: false,
    refetchInterval: historyRefetchMs[window],
    refetchIntervalInBackground: false,
  });
  const { live, unavailable } = liveTunnelConnections(connections, error);
  return (
    <section
      className="mb-8 space-y-10"
      aria-label="Tunnel health and activity"
    >
      <TunnelStatus
        live={live}
        loading={loading}
        unavailable={unavailable}
        agentSetupHref={agentSetupHref}
      />
      <div className="space-y-6">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="text-display-xs">Activity</h2>
            <p className="text-muted-foreground text-sm">
              Every MCP server on this tunnel, {windowPhrase[window]}
              {history.data?.lastSampleAt
                ? ` · updated ${formatDistanceToNow(new Date(history.data.lastSampleAt), { addSuffix: true })}`
                : ""}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button asChild variant="tertiary" size="sm">
              <Link to={logsHref}>View tool calls</Link>
            </Button>
            <SegmentedControl
              value={window}
              onChange={setWindow}
              options={[...windows]}
            />
          </div>
        </div>
        <ActivityHistory
          history={history.data}
          pending={history.isPending}
          error={history.isError}
          window={window}
        />
      </div>
    </section>
  );
}

const dotClass: Record<Tone, string> = {
  success: "bg-success-default",
  warning: "bg-warning-default",
  destructive: "bg-destructive-default",
  neutral: "bg-muted-foreground",
};

const titleClass: Record<Tone, string> = {
  success: "text-default-success",
  warning: "text-default-warning",
  destructive: "text-default-destructive",
  neutral: "text-foreground",
};

function TunnelStatus({
  live,
  loading,
  unavailable,
  agentSetupHref,
}: {
  live: TunneledMcpConnection[];
  loading: boolean;
  unavailable: boolean;
  agentSetupHref: string;
}) {
  if (loading) return <Skeleton className="h-36 w-full" />;
  const status = tunnelStatus(live, unavailable, { agentSetupHref });
  const sessions = live.reduce((n, c) => n + c.activeConsumerSessions, 0);
  const inFlight = live.reduce((n, c) => n + c.activeSubstreams, 0);
  return (
    <div className="bg-card border">
      <div
        className="flex flex-wrap items-start justify-between gap-4 p-6"
        role="group"
        aria-label="Tunnel status"
      >
        <div className="min-w-0 space-y-2">
          <p className="text-eyebrow">Tunnel status</p>
          <div className="flex items-center gap-3">
            <span
              aria-hidden
              className={cn(
                "size-2 shrink-0 rounded-full",
                dotClass[status.tone],
              )}
            />
            <p
              className={cn(
                "font-display text-[2rem] leading-none font-thin",
                titleClass[status.tone],
              )}
            >
              {status.title}
            </p>
          </div>
          <p className="text-muted-foreground max-w-prose text-sm">
            {status.detail}
          </p>
        </div>
        {status.action && (
          <Button
            asChild
            variant={status.tone === "neutral" ? "primary" : "secondary"}
            size="sm"
          >
            <Link to={status.action.href}>{status.action.label}</Link>
          </Button>
        )}
      </div>
      {live.length > 0 && !unavailable && (
        <>
          <dl className="flex flex-wrap gap-x-8 gap-y-2 border-t px-6 py-3 text-sm">
            <Fact label="Agents" value={live.length} />
            <Fact label="Client sessions" value={sessions} />
            <Fact label="Requests in flight" value={inFlight} />
          </dl>
          <ul className="divide-y border-t">
            {[...live].sort(bySeverity).map((c) => (
              <AgentRow key={c.gatewaySessionId} connection={c} />
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

// Agents that cannot reach the target come first: they are why the status
// above is not green.
const severity = { destructive: 0, warning: 1, success: 2 } as const;
function bySeverity(a: TunneledMcpConnection, b: TunneledMcpConnection) {
  return (
    severity[diagnosticTone(a.diagnostics)] -
    severity[diagnosticTone(b.diagnostics)]
  );
}

function Fact({ label, value }: { label: string; value: number }) {
  return (
    <div className="flex items-baseline gap-2">
      <dt className="text-eyebrow">{label}</dt>
      <dd className="tabular-nums">{value.toLocaleString()}</dd>
    </div>
  );
}

function AgentRow({ connection: c }: { connection: TunneledMcpConnection }) {
  const d = c.diagnostics;
  return (
    <li className="space-y-3 px-6 py-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <p className="text-sm font-medium">
            {c.serviceVersion}
            <span className="text-muted-foreground font-normal">
              {" "}
              · agent {c.agentVersion ?? "unknown"}
            </span>
          </p>
          {c.targetDisplay ? (
            <p
              className="text-muted-foreground truncate font-mono text-xs"
              title={c.targetDisplay}
            >
              {c.targetDisplay}
            </p>
          ) : null}
        </div>
        <div className="flex items-center gap-3">
          <span className="text-muted-foreground text-xs">
            Reported{" "}
            {formatDistanceToNow(new Date(c.lastHeartbeatAt), {
              addSuffix: true,
            })}
          </span>
          <Badge variant={diagnosticTone(d)}>
            <Badge.Text>{diagnosticLabel(d)}</Badge.Text>
          </Badge>
        </div>
      </div>
      {!d || d.state === "unsupported" ? (
        <p className="text-muted-foreground text-sm">
          This agent does not report network checks. Update the agent and
          gateway to see them; forwarding still works.
        </p>
      ) : d.state === "disabled" ? (
        // The agent announced that its checks are turned off.
        <p className="text-muted-foreground text-sm">
          Checks are turned off for this agent, so its reachability is not
          reported.
        </p>
      ) : (
        <DiagnosticDetails value={d} />
      )}
    </li>
  );
}

function diagnosticTone(
  d?: TunnelDiagnostics,
): "warning" | "success" | "destructive" {
  if (d?.state !== "available") return "warning";
  if (d.targetState === "unreachable") return "destructive";
  if (d.targetState === "reachable") return "success";
  return "warning";
}

function diagnosticLabel(d?: TunnelDiagnostics) {
  if (!d) return "Diagnostics unsupported";
  if (d.state !== "available")
    return (
      (
        {
          unsupported: "Diagnostics unsupported",
          pending: "Waiting for checks",
          disabled: "Checks turned off",
          stale: "Checks are stale",
          unavailable: "Checks unavailable",
        } as Record<string, string>
      )[d.state] ?? "Unknown"
    );
  return (
    (
      {
        reachable: "Network reachable",
        unreachable: "Target unreachable",
        pending: "Waiting for checks",
        unknown: "Reachability unknown",
      } as Record<string, string>
    )[d.targetState ?? "unknown"] ?? "Unknown"
  );
}

const failures: Record<string, string> = {
  dns_not_found: "Hostname was not found. Check DNS and the target hostname.",
  dns_timeout: "DNS lookup timed out. Check DNS on the agent host.",
  dns_error: "DNS lookup failed.",
  tcp_refused:
    "Connection refused. Check the target port and whether the server is running.",
  tcp_timeout: "Connection timed out. Check routing and firewall rules.",
  tls_expired: "The target certificate has expired or is not yet valid.",
  tls_untrusted: "The target certificate is not trusted.",
  tls_name_mismatch: "The certificate does not match the target hostname.",
  tls_error: "TLS negotiation failed.",
  proxy:
    "A proxy is configured. Direct network probes are skipped; HTTP reachability is observed from normal traffic.",
  unknown: "The transport failed. Check the local agent logs.",
};

const summaryClass =
  "text-muted-foreground hover:text-foreground flex w-fit cursor-pointer list-none items-center gap-1 text-sm [&::-webkit-details-marker]:hidden";

const stepNames = ["DNS", "TCP", "TLS"] as const;

function stepLabel(step?: TunnelDiagnosticStep) {
  switch (step?.state) {
    case "pass":
      return `Passed · ${step.durationMs} ms`;
    case "fail":
      return "Failed";
    case "not_applicable":
      return "Not applicable";
    case "not_tested":
    case undefined:
    default:
      return "Not checked";
  }
}

function stepTone(step?: TunnelDiagnosticStep, blocked?: boolean): Tone {
  if (blocked) return "neutral";
  if (step?.state === "pass") return "success";
  if (step?.state === "fail") return "destructive";
  return "neutral";
}

function DiagnosticDetails({ value: d }: { value: TunnelDiagnostics }) {
  const steps = [d.dns, d.tcp, d.tls];
  const failure = steps.find((s) => s?.failure)?.failure;
  return (
    <div className="space-y-3">
      {failure && d.state === "available" && (
        <p className="text-default-destructive text-sm">
          {failures[failure] ?? "Transport check unavailable."}
        </p>
      )}
      {d.state !== "available" && d.receivedAt && (
        <p className="text-muted-foreground text-sm">
          Last report{" "}
          {formatDistanceToNow(new Date(d.receivedAt), { addSuffix: true })}.
          These results may no longer reflect the target.
        </p>
      )}
      <details className="group">
        <summary className={summaryClass}>
          <ChevronRight className="size-4 transition-transform group-open:rotate-90" />
          Connection details
        </summary>
        <div className="mt-3 space-y-4 pl-5">
          <dl className="grid max-w-md grid-cols-[3rem_1fr] gap-x-4 gap-y-1.5 text-sm">
            {stepNames.map((label, index) => {
              const step = steps[index];
              const blockedBy = stepNames.find(
                (_, i) => i < index && steps[i]?.state === "fail",
              );
              const blocked =
                !!blockedBy && (!step || step.state === "not_tested");
              return (
                <div key={label} className="contents">
                  <dt className="text-eyebrow self-center">{label}</dt>
                  <dd
                    className={cn(
                      "flex items-center gap-2",
                      stepTone(step, blocked) === "neutral" &&
                        "text-muted-foreground",
                    )}
                  >
                    <span
                      aria-hidden
                      className={cn(
                        "size-1.5 shrink-0 rounded-full",
                        dotClass[stepTone(step, blocked)],
                      )}
                    />
                    {blocked
                      ? `Not checked (${blockedBy} failed)`
                      : stepLabel(step)}
                  </dd>
                </div>
              );
            })}
          </dl>
          <HttpProgress value={d} />
          {Boolean(d.lastHttpStatus) && (
            <p className="text-muted-foreground text-sm">
              Last HTTP response: {d.lastHttpStatus}
              {Number(d.lastHttpResponseAgeMs ?? -1) >= 0 &&
                ` · ${formatDistanceToNow(new Date(Date.now() - Number(d.lastHttpResponseAgeMs)), { addSuffix: true })}`}
              {(d.lastHttpStatus === 401 || d.lastHttpStatus === 403) &&
                ". Target responded; credentials may be needed."}
              {Number(d.lastHttpStatus) >= 500 &&
                ". The MCP server answered with a server error. Check the server's logs."}
            </p>
          )}
        </div>
      </details>
    </div>
  );
}

function HttpProgress({ value: d }: { value: TunnelDiagnostics }) {
  let note: string | null = null;
  if (d.state !== "available") {
    note = "HTTP progress unavailable. Waiting for a fresh report.";
  } else if (!d.httpProgress) {
    note = "HTTP progress unavailable for this agent.";
  } else if (!Number(d.requestsTotal)) {
    note = "HTTP / MCP: Not observed. No traffic has reached this agent.";
  }
  if (note || !d.httpProgress) {
    return <p className="text-muted-foreground text-sm">{note}</p>;
  }
  return (
    <div className="space-y-2">
      <dl className="grid max-w-md grid-cols-2 gap-4">
        <div>
          <dt className="text-eyebrow">Waiting for response headers</dt>
          <dd className="mt-1 text-sm tabular-nums">
            {Number(d.httpProgress.waitingHeaders)}
          </dd>
        </div>
        <div>
          <dt className="text-eyebrow">Responses still open</dt>
          <dd className="mt-1 text-sm tabular-nums">
            {Number(d.httpProgress.openResponses)}
          </dd>
        </div>
      </dl>
      <p className="text-muted-foreground max-w-prose text-sm">
        Counts are from the latest report. Open streams may be expected. HTTP
        responses alone do not confirm MCP success; activity charts show
        observed MCP outcomes.
      </p>
    </div>
  );
}

function ActivityHistory({
  history,
  pending,
  error,
  window,
}: {
  history?: TunnelMetrics;
  pending: boolean;
  error: boolean;
  window: Window;
}) {
  if (pending) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-[7.5rem] w-full" />
        <Skeleton className="h-80 w-full" />
      </div>
    );
  }
  if (error || history?.state !== "available") {
    const presentation = activityUnavailable(history?.state);
    return <InlineEmptyState icon="chart-no-axes-combined" {...presentation} />;
  }
  return <ActivityCharts history={history} window={window} />;
}

function activityUnavailable(state?: string) {
  switch (state) {
    case "too_large":
      return {
        heading: "This time range contains too much activity",
        description:
          "Choose a shorter time range above. Live connection and target checks remain available.",
      };
    case undefined:
    default:
      return {
        heading: "Activity history is unavailable",
        description:
          "History refreshes automatically. Live tunnel status is shown above.",
      };
  }
}

type CountKey =
  | "toolCalls"
  | "toolsList"
  | "otherRequests"
  | "successes"
  | "errors"
  | "rejected"
  | "connectionsOpened";

function total(points: TunnelMetricPoint[], key: CountKey) {
  return points.reduce((n, p) => n + Number(p[key] ?? 0), 0);
}

function percentile(points: TunnelMetricPoint[]) {
  const values = points
    .map((p) => p.p95Ms)
    .filter((v): v is number => v != null);
  if (!values.length) return null;
  if (values.some((v) => v < 0)) return -1;
  return Math.max(...values);
}

function ActivityCharts({
  history,
  window,
}: {
  history: TunnelMetrics;
  window: Window;
}) {
  const points = history.points;
  const hasRequests = points.some(
    (p) =>
      p.toolCalls != null ||
      p.toolsList != null ||
      p.otherRequests != null ||
      p.successes != null ||
      p.errors != null ||
      p.rejected != null,
  );
  const requests =
    total(points, "toolCalls") +
    total(points, "toolsList") +
    total(points, "otherRequests");
  const successes = total(points, "successes");
  const errors = total(points, "errors");
  const completions = successes + errors;
  const errorRate = completions ? errors / completions : 0;
  const rejected = total(points, "rejected");
  const p95 = percentile(points);
  const partial = points.some((p) => p.collectionPartial);
  const clients = [...history.clients].sort((a, b) => b.requests - a.requests);
  return (
    <div className="space-y-6">
      <StatRow
        className="grid grid-cols-2 divide-x-0 divide-y md:grid-cols-4 md:divide-x md:divide-y-0"
        metrics={[
          {
            label: "Requests",
            value: hasRequests ? requests.toLocaleString() : "—",
            size: "sm",
            tone: "neutral",
            description: hasRequests
              ? `${total(points, "toolCalls").toLocaleString()} tool calls`
              : "No requests recorded",
          },
          {
            label: "MCP responses",
            value: hasRequests ? successes.toLocaleString() : "—",
            size: "sm",
            tone: hasRequests && successes > 0 ? "success" : "neutral",
            description: "Completed successfully",
          },
          {
            label: "Errors",
            value: hasRequests ? errors.toLocaleString() : "—",
            size: "sm",
            tone: errors > 0 ? "destructive" : "neutral",
            description: [
              completions
                ? `${(errorRate * 100).toFixed(errorRate < 0.1 ? 1 : 0)}% of completed requests`
                : "No completed requests",
              rejected > 0
                ? `${rejected.toLocaleString()} blocked by Speakeasy`
                : null,
            ]
              .filter(Boolean)
              .join(" · "),
          },
          {
            label: "Slowest p95",
            value: latencyLabel(p95),
            size: "sm",
            tone: "neutral",
            description: "Worst interval's p95 response time",
          },
        ]}
      />
      <div className="grid gap-6 xl:grid-cols-3">
        <div className="xl:col-span-2">
          <MetricChart
            title="MCP requests"
            points={points}
            window={window}
            series={[
              { key: "toolCalls", label: "Tool calls" },
              { key: "toolsList", label: "Tool listings" },
              { key: "otherRequests", label: "Other requests" },
              { key: "errors", label: "Errors", error: true },
            ]}
          />
        </div>
        <ChartCard
          title="Requests by client"
          chartId="tunnel-clients"
          expandable={false}
          hasData={clients.length > 0}
          expandedChart={null}
          onExpand={() => {}}
        >
          <div className="overflow-y-auto pt-1 xl:h-60">
            {clients.length ? (
              <RankedBarList
                items={clients.map((c) => ({
                  key: c.family,
                  label: clientLabel(c.family),
                  value: c.requests,
                  valueLabel: c.requests.toLocaleString(),
                }))}
              />
            ) : (
              <p className="text-muted-foreground text-sm">
                No client has sent a request {windowPhrase[window]}.
              </p>
            )}
          </div>
        </ChartCard>
      </div>
      <div className="grid gap-6 lg:grid-cols-2">
        <MetricChart
          title="Response time"
          points={points}
          window={window}
          latency
          series={[
            { key: "p50Ms", label: "p50" },
            { key: "p95Ms", label: "p95" },
          ]}
        />
        <MetricChart
          title="Tunnel connections"
          points={points}
          window={window}
          series={[
            { key: "connections", label: "Agents" },
            { key: "consumerSessions", label: "Client sessions" },
            { key: "activeRequests", label: "Requests in flight" },
          ]}
        />
      </div>
      {partial && (
        <p className="text-default-warning text-sm">
          Some aggregates were lost in this range, so counts are incomplete.
        </p>
      )}
      <AboutTheseNumbers points={points} />
    </div>
  );
}

function clientLabel(family: string) {
  // Every family the tunnel collector reports.
  const known: Record<string, string> = {
    claude: "Claude",
    codex: "Codex",
    cursor: "Cursor",
    chatgpt: "ChatGPT",
    inspector: "MCP Inspector",
    unknown: "Unknown client",
    other: "Other clients",
  };
  return known[family] ?? family.charAt(0).toUpperCase() + family.slice(1);
}

function AboutTheseNumbers({ points }: { points: TunnelMetricPoint[] }) {
  return (
    <details className="group border-t pt-4">
      <summary className={summaryClass}>
        <ChevronRight className="size-4 transition-transform group-open:rotate-90" />
        About these numbers
      </summary>
      <div className="mt-4 space-y-4">
        <ul className="text-muted-foreground max-w-prose list-disc space-y-1 pl-5 text-sm">
          <li>
            Metrics are best-effort. Only reporting servers contribute to these
            counts, and missing samples show as gaps.
          </li>
          <li>
            Requests count attempts; responses and errors count completions,
            which may fall in a later interval.
          </li>
          <li>
            Requests that Speakeasy blocks before they reach the MCP server, for
            example by policy, are not errors. They are shown as blocked next to
            Errors and in the data table.
          </li>
          <li>
            Zero connections are recorded for up to five minutes after a
            disconnect. {total(points, "connectionsOpened").toLocaleString()}{" "}
            connections opened in this range.
          </li>
          <li>
            Response times are histogram bounds; the slowest bin means more than
            60 seconds.
          </li>
          <li>
            The client is identified by the name it reports when it connects,
            falling back to the HTTP User-Agent. Clients Speakeasy does not
            recognize appear as &ldquo;Other clients&rdquo;.
          </li>
          <li>
            Requests that Speakeasy sends on a user&rsquo;s behalf through this
            tunnel are counted, because they reach the MCP server. Examples are
            tool discovery while connecting a client and calls fanned out
            through a gateway.
          </li>
          <li>
            Checks that Speakeasy runs to validate a connection are not counted.
          </li>
        </ul>
        <div className="max-h-80 overflow-auto border">
          <Table
            data={points}
            rowKey={(p) => p.time.toISOString()}
            columns={[
              {
                key: "time",
                header: "Time",
                render: (p) => p.time.toLocaleString(),
              },
              {
                key: "toolCalls",
                header: "Tool calls",
                render: (p) => p.toolCalls ?? "—",
              },
              {
                key: "toolsList",
                header: "Tool listings",
                render: (p) => p.toolsList ?? "—",
              },
              {
                key: "otherRequests",
                header: "Other requests",
                render: (p) => p.otherRequests ?? "—",
              },
              {
                key: "errors",
                header: "Errors",
                render: (p) => p.errors ?? "—",
              },
              {
                key: "rejected",
                header: "Blocked",
                render: (p) => p.rejected ?? "—",
              },
              {
                key: "connections",
                header: "Agents",
                render: (p) => p.connections?.toFixed(1) ?? "—",
              },
              {
                key: "p95Ms",
                header: "p95",
                render: (p) => latencyLabel(p.p95Ms),
              },
            ]}
          />
        </div>
      </div>
    </details>
  );
}

function latencyLabel(value?: number | null) {
  if (value == null) return "—";
  if (value < 0) return ">60 s";
  if (value >= 1000) return `${(value / 1000).toFixed(1)} s`;
  return `${value} ms`;
}

function axisLabel(time: Date, window: Window) {
  return window === "week"
    ? time.toLocaleString(undefined, { month: "short", day: "numeric" })
    : time.toLocaleTimeString(undefined, {
        hour: "numeric",
        minute: "2-digit",
      });
}

type SeriesKey =
  | "toolCalls"
  | "toolsList"
  | "otherRequests"
  | "errors"
  | "connections"
  | "consumerSessions"
  | "activeRequests"
  | "p50Ms"
  | "p95Ms";

function MetricChart({
  title,
  points,
  series,
  window,
  latency = false,
}: {
  title: string;
  points: TunnelMetricPoint[];
  series: { key: SeriesKey; label: string; error?: boolean }[];
  window: Window;
  latency?: boolean;
}) {
  const colors = useSeriesColors();
  const axis = chartColors(useIsDarkTheme());
  const hasData = points.some((p) => series.some((s) => p[s.key] != null));
  const options: ChartOptions<"line"> = {
    responsive: true,
    maintainAspectRatio: false,
    animation: false,
    spanGaps: false,
    interaction: { mode: "index", intersect: false },
    plugins: {
      legend: EXPANDED_LEGEND,
      tooltip: {
        ...SHARED_TOOLTIP,
        callbacks: {
          title: (items) => {
            const point = points[items[0]?.dataIndex ?? 0];
            return point
              ? point.time.toLocaleString(undefined, {
                  month: "short",
                  day: "numeric",
                  hour: "numeric",
                  minute: "2-digit",
                })
              : "";
          },
          label: (context) => {
            const raw = Number(context.raw);
            const value = latency
              ? raw === OVER_SIXTY_SECONDS
                ? ">60 s"
                : latencyLabel(raw)
              : context.formattedValue;
            return ` ${context.dataset.label}: ${value}`;
          },
        },
      },
    },
    scales: {
      x: {
        ticks: {
          maxTicksLimit: 6,
          maxRotation: 0,
          autoSkip: true,
          color: axis.label,
          font: { size: 11 },
        },
        grid: { display: false },
        border: { display: false },
      },
      y: {
        beginAtZero: true,
        border: { display: false },
        ticks: {
          precision: 0,
          maxTicksLimit: 5,
          color: axis.label,
          font: { size: 11 },
          callback: latency
            ? (value) => latencyLabel(Number(value))
            : undefined,
        },
        grid: { color: axis.gridLine },
      },
    },
  };
  return (
    <ChartCard
      title={title}
      chartId={title}
      expandable={false}
      hasData={hasData}
      expandedChart={null}
      onExpand={() => {}}
    >
      <div className="h-60">
        {hasData ? (
          <Line
            aria-label={title}
            role="img"
            options={options}
            data={{
              labels: points.map((p) => axisLabel(new Date(p.time), window)),
              datasets: series.map((s, i) => {
                const data = points.map((p) => {
                  const value = p[s.key];
                  if (value == null) return null;
                  return Number(value) < 0 ? OVER_SIXTY_SECONDS : Number(value);
                });
                return {
                  label: s.label,
                  data,
                  borderColor: s.error ? ACCENT_RED : colors[i],
                  backgroundColor: s.error ? ACCENT_RED : colors[i],
                  borderWidth: 1.5,
                  pointRadius: isolatedPointRadii(data),
                  pointHoverRadius: 4,
                  pointHitRadius: 8,
                  tension: 0,
                };
              }),
            }}
          />
        ) : (
          <InlineEmptyState
            icon="chart-no-axes-combined"
            heading="No recorded activity"
            description="This chart fills as metrics arrive. No samples is different from zero traffic."
          />
        )}
      </div>
    </ChartCard>
  );
}
