import { ChartCard } from "@/components/chart/ChartCard";
import { ACCENT_RED, AXIS, TOOLTIP } from "@/components/chart/palette";
import {
  useIsDarkTheme,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { StatRow } from "@/components/stat-row";
import { Link } from "react-router";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Table } from "@/components/ui/Table";
import { Skeleton } from "@/components/ui/Skeleton";
import type { TunneledMcpServerConnections } from "@gram/client/models/components/tunneledmcpserverconnections.js";
import type { TunneledMcpConnection } from "@gram/client/models/components/tunneledmcpconnection.js";
import type { TunnelMetrics } from "@gram/client/models/components/tunnelmetrics.js";
import type { TunnelDiagnosticStep } from "@gram/client/models/components/tunneldiagnosticstep.js";
import type { TunnelDiagnostics } from "@gram/client/models/components/tunneldiagnostics.js";
import type { TunnelMetricPoint } from "@gram/client/models/components/tunnelmetricpoint.js";
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
import { Line } from "react-chartjs-2";
import { useState } from "react";
import { formatDistanceToNow } from "date-fns";

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
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
  });
  const live = connections?.connections ?? [];
  const unavailable = error || connections?.collectionState === "unavailable";
  const fresh = live.filter(
    (c) =>
      c.diagnostics?.state === "available" &&
      (c.diagnostics.targetState === "reachable" ||
        c.diagnostics.targetState === "unreachable"),
  );
  const failing = fresh.filter(
    (c) => c.diagnostics?.targetState === "unreachable",
  );
  const reachable = fresh.filter(
    (c) => c.diagnostics?.targetState === "reachable",
  );
  let target = "Unknown";
  if (!unavailable && live.length > 0) {
    target = "Not checked";
    if (fresh.length) target = "Checks incomplete";
    if (reachable.length === live.length) target = "Reachable";
    if (failing.length) {
      target = "Some checks failed";
      if (reachable.length) target = "Partially reachable";
      if (failing.length === live.length) target = "Unreachable";
    }
  }
  let targetTone: "warning" | "success" | "destructive" = "warning";
  if (reachable.length > 0 && reachable.length === live.length)
    targetTone = "success";
  if (failing.length) targetTone = "destructive";
  if (unavailable) targetTone = "warning";
  let connectionTone: "warning" | "success" | "neutral" = live.length
    ? "success"
    : "neutral";
  if (unavailable) connectionTone = "warning";
  const points = history.data?.points ?? [];
  const sum = (key: "toolCalls" | "toolsList" | "successes" | "errors") =>
    points.reduce((n, p) => n + Number(p[key] ?? 0), 0);
  const success = sum("successes");
  const historyReady = !history.isError && history.data?.state === "available";
  const hasActivity = points.some(
    (p) =>
      p.toolCalls != null ||
      p.toolsList != null ||
      p.otherRequests != null ||
      p.successes != null ||
      p.errors != null,
  );
  return (
    <section className="mb-8 space-y-6" aria-label="Tunnel health and activity">
      <StatRow
        className="grid grid-cols-1 divide-x-0 divide-y md:grid-cols-3 md:divide-x md:divide-y-0"
        isLoading={loading}
        metrics={[
          {
            label: "Tunnel connections",
            value: unavailable ? "Unavailable" : live.length,
            size: unavailable ? "sm" : "md",
            tone: connectionTone,
            description: unavailable
              ? "Live status could not be loaded"
              : "Connected agents",
          },
          {
            label: "Transport status",
            value: target,
            size: "sm",
            tone: targetTone,
            description: (
              <span className="flex flex-col gap-1">
                Network reachability from each agent
                {!unavailable && failing.length > 0 && (
                  <Link to={logsHref} className="underline underline-offset-4">
                    View tool logs
                  </Link>
                )}
              </span>
            ),
          },
          {
            label: "MCP responses",
            value: historyReady && hasActivity ? success.toLocaleString() : "—",
            tone:
              historyReady && hasActivity && success > 0
                ? "success"
                : "neutral",
            description: "Successful completions in selected range",
          },
        ]}
      />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-display-xs">Activity</h2>
          <p className="text-muted-foreground text-sm">
            Activity across all MCP servers using this tunnel.
          </p>
        </div>
        <SegmentedControl
          value={window}
          onChange={setWindow}
          options={[...windows]}
        />
      </div>
      <ActivityHistory
        history={history.data}
        pending={history.isPending}
        error={history.isError}
      />
      <div className="space-y-3">
        <h2 className="text-display-xs">Agents & target checks</h2>
        <p className="text-muted-foreground text-sm">
          Network checks refresh about every 30 seconds. MCP activity comes from
          normal traffic.
        </p>
        <AgentList
          live={live}
          loading={loading}
          unavailable={unavailable}
          agentSetupHref={agentSetupHref}
        />
      </div>
    </section>
  );
}

function ActivityHistory({
  history,
  pending,
  error,
}: {
  history?: TunnelMetrics;
  pending: boolean;
  error: boolean;
}) {
  if (pending) return <Skeleton className="h-64 w-full" />;
  if (error || history?.state !== "available") {
    const presentation = activityUnavailable(history?.state);
    return <InlineEmptyState icon="chart-no-axes-combined" {...presentation} />;
  }
  return <ActivityCharts history={history} />;
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
          "History refreshes automatically. Live tunnel status is shown below.",
      };
  }
}

function ActivityCharts({ history }: { history: TunnelMetrics }) {
  const points = history.points;
  const sum = (key: "toolCalls" | "toolsList" | "otherRequests" | "errors") =>
    points.reduce((n, p) => n + Number(p[key] ?? 0), 0);
  return (
    <>
      <div className="grid gap-6 lg:grid-cols-2">
        <MetricChart
          title="MCP requests"
          points={points}
          series={[
            { key: "toolCalls", label: "Tool calls" },
            { key: "toolsList", label: "Tools/list" },
            { key: "otherRequests", label: "Other requests" },
            { key: "errors", label: "Errors", error: true },
          ]}
        />
        <MetricChart
          title="Tunnel connections"
          points={points}
          series={[
            { key: "connections", label: "Agents" },
            { key: "consumerSessions", label: "Consumer sessions" },
            { key: "activeRequests", label: "Active requests" },
          ]}
        />
      </div>
      <MetricChart
        title="Completion latency · histogram upper bounds"
        points={points}
        series={[
          { key: "p50Ms", label: "p50 (ms)" },
          { key: "p95Ms", label: "p95 (ms)" },
        ]}
      />
      <p className="text-muted-foreground text-xs">
        The final latency bin means more than 60 seconds.
      </p>
      <div className="flex flex-wrap justify-between gap-3 text-sm">
        <p className="text-muted-foreground">
          {sum("toolCalls").toLocaleString()} tool calls ·{" "}
          {sum("toolsList").toLocaleString()} tools/list ·{" "}
          {sum("otherRequests").toLocaleString()} other requests ·{" "}
          {sum("errors").toLocaleString()} errors ·{" "}
          {points
            .reduce((n, p) => n + (p.connectionsOpened ?? 0), 0)
            .toLocaleString()}{" "}
          connections opened
        </p>
        <p className="text-muted-foreground">
          {history?.lastSampleAt
            ? `Latest sample ${formatDistanceToNow(new Date(history.lastSampleAt), { addSuffix: true })}`
            : "No samples received yet"}
        </p>
      </div>
      <p className="text-muted-foreground text-xs">
        Metrics are best-effort. Only reporting servers contribute to these
        counts.
        {points.some((p) => p.collectionPartial) &&
          " Some aggregates were lost; counts are incomplete."}
      </p>
      <details className="border p-4">
        <summary className="cursor-pointer text-sm font-medium">
          View activity data
        </summary>
        <div className="mt-4 max-h-80 overflow-auto">
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
                header: "Tools/list",
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
                key: "connections",
                header: "Connections",
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
      </details>
      <p className="text-muted-foreground text-sm">
        Missing samples show as gaps. Zero connections are recorded for up to
        five minutes after a disconnect. Requests count attempts; errors count
        completions, which may fall in a later interval.
      </p>
      {history?.clients.length ? (
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-eyebrow">Client families</span>
          {history.clients.map((c) => (
            <Badge key={c.family} variant="neutral">
              <Badge.Text>
                {c.family} · {Number(c.requests).toLocaleString()}
              </Badge.Text>
            </Badge>
          ))}
        </div>
      ) : null}
    </>
  );
}

function AgentList({
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
  if (loading) return <Skeleton className="h-40 w-full" />;
  if (unavailable)
    return (
      <InlineEmptyState
        icon="wifi-off"
        heading="Live status is unavailable"
        description="The latest connection status could not be loaded. Retrying automatically."
      />
    );
  if (!live.length)
    return (
      <InlineEmptyState
        icon="unplug"
        heading="No connected agents"
        description="Start your tunnel agent and check its connection to the gateway."
        action={
          <Link to={agentSetupHref}>
            <Button variant="secondary" size="sm">
              <Button.Text>View agent setup</Button.Text>
            </Button>
          </Link>
        }
      />
    );
  return (
    <>
      {live.map((c) => (
        <AgentDiagnostics key={c.gatewaySessionId} connection={c} />
      ))}
    </>
  );
}

function AgentDiagnostics({
  connection: c,
}: {
  connection: TunneledMcpConnection;
}) {
  return (
    <article className="bg-card space-y-4 border p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="font-medium">{c.serviceVersion}</h3>
          <p className="text-muted-foreground break-all font-mono text-xs">
            Agent {c.agentVersion ?? "unknown"} ·{" "}
            {c.gatewaySessionId.slice(0, 8)}
          </p>
        </div>
        <Badge variant={diagnosticTone(c.diagnostics)}>
          <Badge.Text>{diagnosticLabel(c.diagnostics)}</Badge.Text>
        </Badge>
      </div>
      {c.targetDisplay ? (
        <p className="break-all font-mono text-sm">{c.targetDisplay}</p>
      ) : null}
      {!c.diagnostics || c.diagnostics.state === "unsupported" ? (
        <p className="text-muted-foreground text-sm">
          Target checks are unavailable for this connection. Update the agent
          and gateway to enable diagnostics; forwarding remains supported.
        </p>
      ) : (
        <DiagnosticDetails value={c.diagnostics} />
      )}
      <p className="text-muted-foreground text-xs">
        {c.activeConsumerSessions} consumer sessions · {c.activeSubstreams}{" "}
        active requests · Connection snapshot{" "}
        {formatDistanceToNow(new Date(c.lastHeartbeatAt), {
          addSuffix: true,
        })}
      </p>
    </article>
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

function latencyLabel(value?: number | null) {
  if (value == null) return "—";
  if (value < 0) return ">60 s";
  return `${value} ms`;
}

function stepLabel(step?: TunnelDiagnosticStep) {
  switch (step?.state) {
    case "not_tested":
    case undefined:
      return "Not checked";
    case "pass":
      return `Passed · ${step.durationMs} ms`;
    case "fail":
      return "Failed";
    case "not_applicable":
      return "Not applicable";
    default:
      return "Not checked";
  }
}

function diagnosticLabel(d?: TunnelDiagnostics) {
  if (!d) return "Diagnostics unsupported";
  if (d.state !== "available")
    return (
      (
        {
          unsupported: "Diagnostics unsupported",
          pending: "Waiting for checks",
          disabled: "Checks disabled",
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
function DiagnosticDetails({ value: d }: { value: TunnelDiagnostics }) {
  const steps = [
    ["DNS", d.dns],
    ["TCP", d.tcp],
    ["TLS", d.tls],
  ] as const;
  const failure = steps.find(([, s]) => s?.failure)?.[1]?.failure;
  return (
    <>
      {failure && d.state === "available" && (
        <p className="text-sm">
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
      <details>
        <summary className="cursor-pointer text-sm font-medium">
          Connection details
        </summary>
        <div className="mt-4 space-y-4">
          <dl className="space-y-2">
            {steps.map(([label, step], index) => {
              const blockedBy = steps
                .slice(0, index)
                .find(([, previous]) => previous?.state === "fail");
              const blocked =
                blockedBy && (!step || step.state === "not_tested");
              return (
                <div
                  key={label}
                  className="flex flex-wrap gap-x-4 gap-y-1 text-sm"
                >
                  <dt className="w-8 font-mono">{label}</dt>
                  <dd>
                    {blocked
                      ? `Not checked (${blockedBy[0]} failed)`
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
            </p>
          )}
        </div>
      </details>
    </>
  );
}

function HttpProgress({ value: d }: { value: TunnelDiagnostics }) {
  if (d.state === "disabled") {
    return (
      <p className="text-muted-foreground text-sm">
        HTTP progress is unavailable because diagnostics are disabled.
      </p>
    );
  }
  if (d.state !== "available") {
    return (
      <p className="text-muted-foreground text-sm">
        HTTP progress unavailable. Waiting for a fresh report.
      </p>
    );
  }
  if (!d.httpProgress) {
    return (
      <p className="text-muted-foreground text-sm">
        HTTP progress unavailable for this agent.
      </p>
    );
  }
  if (!Number(d.requestsTotal)) {
    return (
      <p className="text-muted-foreground text-sm">
        HTTP / MCP: Not observed. No traffic has reached this agent.
      </p>
    );
  }
  return (
    <div className="space-y-2">
      <dl className="grid grid-cols-2 gap-4">
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
      <p className="text-muted-foreground text-sm">
        Counts are from the latest report. Open streams may be expected. HTTP
        responses alone do not confirm MCP success; activity charts show
        observed MCP outcomes.
      </p>
    </div>
  );
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
}: {
  title: string;
  points: TunnelMetricPoint[];
  series: { key: SeriesKey; label: string; error?: boolean }[];
}) {
  const colors = useSeriesColors();
  const dark = useIsDarkTheme();
  const hasData = points.some((p) => series.some((s) => p[s.key] != null));
  const options: ChartOptions<"line"> = {
    responsive: true,
    maintainAspectRatio: false,
    animation: false,
    spanGaps: false,
    interaction: { mode: "index", intersect: false },
    plugins: {
      legend: { position: "bottom" },
      tooltip: {
        ...TOOLTIP,
        callbacks: {
          label: (context) =>
            `${context.dataset.label}: ${context.raw === 60001 && title.toLowerCase().includes("latency") ? ">60 s" : context.formattedValue}`,
        },
      },
    },
    scales: {
      x: {
        ticks: { maxTicksLimit: 6, color: AXIS.label },
        grid: { display: false },
      },
      y: {
        beginAtZero: true,
        ticks: { precision: 0, color: AXIS.label },
        grid: { color: dark ? AXIS.gridDark : AXIS.grid },
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
              labels: points.map((p) =>
                new Date(p.time).toLocaleString(undefined, {
                  month: "short",
                  day: "numeric",
                  hour: "2-digit",
                  minute: "2-digit",
                }),
              ),
              datasets: series.map((s, i) => ({
                label: s.label,
                data: points.map((p) =>
                  p[s.key] == null
                    ? null
                    : Number(p[s.key]) < 0
                      ? 60001
                      : Number(p[s.key]),
                ),
                borderColor: s.error ? ACCENT_RED : colors[i],
                backgroundColor: s.error ? ACCENT_RED : colors[i],
                borderWidth: 2,
                pointRadius: 1,
                tension: 0,
              })),
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
