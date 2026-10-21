import { useMemo, useState, type JSX } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  agentSeries,
  cacheAgeLabel,
  clientSeries,
  deviceAgentRows,
  fetchFleetHealth,
  fleetHealthQuery,
  inferenceRows,
  latestAgentShare,
  latestShares,
  sessionRows,
  surfaceSeries,
  windowDays,
  type DayCell,
  type FleetHealth as FleetHealthData,
  type OrgRow,
  type Series,
} from "./fleetHealthModel";

type HeightMode = "same" | "sessions" | "machines";

function percent(value: number | undefined): string {
  return value === undefined ? "–" : `${Math.round(value * 100)}%`;
}

function shortDay(day: string): string {
  const date = new Date(`${day}T00:00:00Z`);
  return date.toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    timeZone: "UTC",
  });
}

function Legend({ series }: { series: Series[] }): JSX.Element {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
      {series.map((s) => (
        <li key={s.key} className="flex items-center gap-1.5">
          <span
            aria-hidden
            className="inline-block size-3 rounded-sm"
            style={{ background: s.color }}
          />
          {s.label}
        </li>
      ))}
    </ul>
  );
}

function Bar({
  cell,
  series,
  height,
  title,
}: {
  cell: DayCell | undefined;
  series: Series[];
  height: number;
  title: string;
}): JSX.Element {
  if (!cell) {
    return (
      <div
        role="img"
        aria-label={`${title}: no data`}
        className="h-16 rounded-sm border border-dashed"
        title={`${title}: no data`}
      />
    );
  }
  if (cell.stale) {
    return (
      <div
        role="img"
        aria-label={`${title}: hooks only from sessions started earlier`}
        className="h-16 rounded-sm border border-dashed border-amber-500"
        title={`${title}: hooks only from sessions started earlier`}
      />
    );
  }
  const total = series.reduce(
    (sum, s) => sum + (cell.parts.get(s.key) ?? 0),
    0,
  );
  const tooltip = [
    title,
    ...series
      .filter((s) => (cell.parts.get(s.key) ?? 0) > 0)
      .map((s) => `${s.label}: ${cell.parts.get(s.key)}`),
  ].join("\n");
  return (
    <div
      role="img"
      aria-label={tooltip.replaceAll("\n", ", ")}
      className="bg-muted flex h-16 flex-col justify-end overflow-hidden rounded-sm"
      title={tooltip}
    >
      <div
        className="flex w-full flex-col-reverse transition-[height] motion-reduce:transition-none"
        style={{ height: `${Math.max(height, 0.04) * 100}%` }}
      >
        {series.map((s) => {
          const value = cell.parts.get(s.key) ?? 0;
          if (value === 0 || total === 0) return null;
          return (
            <span
              key={s.key}
              className="block w-full"
              style={{
                height: `${(value / total) * 100}%`,
                background: s.color,
              }}
            />
          );
        })}
      </div>
    </div>
  );
}

function Grid({
  rows,
  days,
  series,
  mode,
  summary,
  emptyText,
}: {
  rows: OrgRow[];
  days: string[];
  series: Series[];
  mode: HeightMode;
  summary: (row: OrgRow) => JSX.Element;
  emptyText: string;
}): JSX.Element {
  if (rows.length === 0) {
    return <p className="text-muted-foreground text-sm">{emptyText}</p>;
  }
  return (
    <div className="overflow-x-auto rounded-md border p-3">
      <div
        className="grid min-w-[760px] items-end gap-[3px] text-xs tabular-nums"
        style={{
          gridTemplateColumns: `minmax(140px, max-content) repeat(${days.length}, minmax(28px, 1fr)) max-content`,
        }}
      >
        <div className="text-muted-foreground pb-1 font-semibold">
          Organization
        </div>
        {days.map((day) => (
          <div
            key={day}
            className="text-muted-foreground pb-1 text-center font-semibold"
          >
            {shortDay(day)}
          </div>
        ))}
        <div className="text-muted-foreground pb-1 pl-2 font-semibold">
          Latest day
        </div>
        {rows.map((row) => {
          const peak = Math.max(
            1,
            ...[...row.days.values()].map((d) =>
              mode === "sessions" ? d.sessions : d.machines,
            ),
          );
          return [
            <div
              key={`${row.id}-name`}
              className="self-center pr-2 font-semibold whitespace-nowrap"
              title={row.id}
            >
              {row.name}
              <small className="text-muted-foreground block font-normal">
                {row.detail}
              </small>
            </div>,
            ...days.map((day) => {
              const cell = row.days.get(day);
              const height =
                mode === "same" || !cell
                  ? 1
                  : (mode === "sessions" ? cell.sessions : cell.machines) /
                    peak;
              return (
                <div key={`${row.id}-${day}`} className="flex flex-col">
                  <Bar
                    cell={cell}
                    series={series}
                    height={height}
                    title={`${row.name}, ${shortDay(day)}`}
                  />
                  <div className="text-muted-foreground mt-0.5 text-center text-[11px]">
                    {cell && !cell.stale ? cell.label : ""}
                  </div>
                </div>
              );
            }),
            <div
              key={`${row.id}-summary`}
              className="self-center pl-2 leading-snug whitespace-nowrap"
            >
              {summary(row)}
            </div>,
          ];
        })}
      </div>
    </div>
  );
}

function HeightToggle({
  mode,
  onChange,
  options,
}: {
  mode: HeightMode;
  onChange: (mode: HeightMode) => void;
  options: { value: HeightMode; label: string }[];
}): JSX.Element {
  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">Bar height</span>
      <Tabs value={mode} onValueChange={(v) => onChange(v as HeightMode)}>
        <TabsList>
          {options.map((o) => (
            <TabsTrigger key={o.value} value={o.value}>
              {o.label}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
    </div>
  );
}

function SessionsSection({ data }: { data: FleetHealthData }): JSX.Element {
  const [mode, setMode] = useState<HeightMode>("same");
  const series = useMemo(() => clientSeries(data.sessions), [data]);
  const rows = useMemo(() => sessionRows(data), [data]);
  const days = useMemo(
    () => windowDays(data.queried_at, data.window_days),
    [data],
  );
  const newest = series[series.length - 1];
  return (
    <section className="space-y-3">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">
          Hook clients by org and day, new sessions only
        </h2>
        <p className="text-muted-foreground text-sm">
          Each day counts only Claude Code sessions that started that day. A
          running session keeps the hooks it started with, so this view drops
          old sessions that would make a fixed machine still look like curl. The
          bar shows the mix of machines on curl (legacy curl hooks) and each
          hooks binary version, blue for old and red for new. The number under
          it is the machines that started a session that day.
        </p>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Legend series={series} />
        <HeightToggle
          mode={mode}
          onChange={setMode}
          options={[
            { value: "same", label: "Same" },
            { value: "sessions", label: "Sessions" },
            { value: "machines", label: "Machines" },
          ]}
        />
      </div>
      <Grid
        rows={rows}
        days={days}
        series={series}
        mode={mode}
        emptyText="No hook sessions in this window."
        summary={(row) => {
          const shares = latestShares(data, row.id, series);
          if (!shares) return <span className="text-muted-foreground">–</span>;
          return (
            <>
              <b>{percent(shares.latestShare)}</b>{" "}
              <span className="text-muted-foreground">
                on {newest?.label ?? "latest"}
              </span>
              <br />
              <span>{percent(shares.curlShare)} curl</span>{" "}
              <span className="text-muted-foreground">
                ({percent(shares.curlShareAll)} all sessions)
              </span>
            </>
          );
        }}
      />
      <p className="text-muted-foreground text-xs">
        A session starts on the UTC day of its first hook. A machine is the
        hook&apos;s device id, or its hostname or email when there is none. The
        last column is each org&apos;s latest day: the share of machines on the
        newest binary version and on curl, then the curl share counting every
        session. A dashed amber cell means hooks arrived that day only from
        sessions started earlier. Binary rows from before hooks recorded their
        version show as &quot;binary, version not recorded&quot;.
      </p>
    </section>
  );
}

function InferenceSection({ data }: { data: FleetHealthData }): JSX.Element {
  const [mode, setMode] = useState<HeightMode>("same");
  const series = useMemo(() => surfaceSeries(data.inference), [data]);
  const rows = useMemo(() => inferenceRows(data), [data]);
  const days = useMemo(
    () => windowDays(data.queried_at, data.window_days),
    [data],
  );
  return (
    <section className="space-y-3">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">
          Inference hook traffic by org and day
        </h2>
        <p className="text-muted-foreground text-sm">
          The bar shows which Claude apps sent conversations through Anthropic
          inference hooks (share of stored messages per app). The number under
          it is the distinct users that day.
        </p>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Legend series={series} />
        <HeightToggle
          mode={mode}
          onChange={setMode}
          options={[
            { value: "same", label: "Same" },
            { value: "sessions", label: "Sessions" },
            { value: "machines", label: "Users" },
          ]}
        />
      </div>
      <Grid
        rows={rows}
        days={days}
        series={series}
        mode={mode}
        emptyText="No inference hook traffic in this window."
        summary={(row) => {
          const latest = [...row.days.keys()].sort().pop();
          const cell = latest ? row.days.get(latest) : undefined;
          if (!cell) return <span className="text-muted-foreground">–</span>;
          return (
            <>
              <b>{cell.label}</b>{" "}
              <span className="text-muted-foreground">users</span>
              <br />
              <span className="text-muted-foreground">
                {row.total.toLocaleString()} messages in window
              </span>
            </>
          );
        }}
      />
      <p className="text-muted-foreground text-xs">
        Source: the session.observed transcript rows that inference hooks write
        for each stored message. Anthropic Compliance API imports are left out.
        Orgs are sorted by messages in the window.
      </p>
    </section>
  );
}

function DeviceAgentsSection({ data }: { data: FleetHealthData }): JSX.Element {
  const [mode, setMode] = useState<HeightMode>("same");
  const series = useMemo(() => agentSeries(data.device_agents), [data]);
  const rows = useMemo(() => deviceAgentRows(data), [data]);
  const days = useMemo(
    () => windowDays(data.queried_at, data.window_days),
    [data],
  );
  return (
    <section className="space-y-3">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">
          Device agent versions by org and day
        </h2>
        <p className="text-muted-foreground text-sm">
          The bar shows the mix of speakeasyd versions, blue for old and red for
          new. The number under it is the machines whose agent polled that day,
          counted under the last version each reported.
        </p>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Legend series={series} />
        <HeightToggle
          mode={mode}
          onChange={setMode}
          options={[
            { value: "same", label: "Same" },
            { value: "machines", label: "Machines" },
          ]}
        />
      </div>
      <Grid
        rows={rows}
        days={days}
        series={series}
        mode={mode}
        emptyText="No device agent polls recorded in this window yet."
        summary={(row) => {
          const latest = latestAgentShare(row, series);
          if (!latest) return <span className="text-muted-foreground">–</span>;
          return (
            <>
              <b>{percent(latest.share)}</b>{" "}
              <span className="text-muted-foreground">on {latest.version}</span>
            </>
          );
        }}
      />
      <p className="text-muted-foreground text-xs">
        Source: the speakeasyd version in each agent&apos;s User-Agent on
        agent.getPlugins, recorded per machine per UTC day in Redis and kept for
        31 days. A machine is its hardware serial, or its environment and
        enrolled email when there is none. Dev builds are left out. Days before
        recording started show no data. Orgs are sorted by machine-days in the
        window.
      </p>
    </section>
  );
}

export function FleetHealth(): JSX.Element {
  const queryClient = useQueryClient();
  const query = useQuery(fleetHealthQuery);
  const refresh = useMutation({
    mutationFn: () => fetchFleetHealth(true),
    onSuccess: (data) =>
      queryClient.setQueryData(fleetHealthQuery.queryKey, data),
  });
  const busy = query.isFetching || refresh.isPending;

  return (
    <div className="space-y-8 p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold">Fleet health</h1>
          <p className="text-muted-foreground text-sm">
            Which client versions every organization runs: hook clients,
            inference hook traffic and device agents. Use it to spot orgs on old
            or legacy clients.
          </p>
        </div>
        <div className="flex items-center gap-3 text-sm">
          {query.data ? (
            <span
              className="text-muted-foreground tabular-nums"
              title={`Read from ClickHouse at ${query.data.queried_at}`}
            >
              {query.data.from_cache ? "Cached, read " : "Read "}
              {cacheAgeLabel(query.data.queried_at, Date.now())}
            </span>
          ) : null}
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            title="Read ClickHouse now instead of the cache"
            onClick={() => refresh.mutate()}
          >
            <RefreshCw
              className={busy ? "animate-spin motion-reduce:animate-none" : ""}
            />
            Refresh
          </Button>
        </div>
      </div>

      {query.isPending ? (
        <div className="space-y-3">
          <Skeleton className="h-6 w-80" />
          <Skeleton className="h-64 w-full" />
        </div>
      ) : null}

      {query.isError || refresh.isError ? (
        <p className="text-destructive text-sm" role="alert">
          {errorMessage(query.error ?? refresh.error)}
        </p>
      ) : null}

      {query.data ? (
        <>
          <SessionsSection data={query.data} />
          <InferenceSection data={query.data} />
          <DeviceAgentsSection data={query.data} />
        </>
      ) : null}
    </div>
  );
}
