import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import type { DeviceIntegrationCoverage } from "@gram/client/models/components/deviceintegrationcoverage.js";
import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";
import { buildDeviceIntegrationConfigQuery } from "@gram/client/react-query/deviceIntegrationConfig.js";
import { useDeviceIntegrationCoverage } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { Stack } from "@/components/ui/Stack";
import { useQueries } from "@tanstack/react-query";
import { ArrowRight } from "lucide-react";
import { useMemo } from "react";

// The pipeline banner: the spine of the page. Coverage is a CONFLUENCE, not a
// single-source pipeline — it is the join of two independent inputs:
//
//   1. the MDM inventory (which devices exist — the denominator), pulled from
//      the inventory sources, and
//   2. the device agent's own heartbeats (which devices are actually running
//      the agent — the numerator), reported by the agent, NOT by any MDM.
//
// Coverage = numerator / denominator. Framing it as "sources → coverage" hid
// the agent entirely, which is doubly wrong on the Device Agent page: the
// agent is the signal the whole feature exists to report, and it is what
// drives the headline percentage. So the banner shows both inputs meeting at
// coverage, and coverage flowing out to the evidence destinations.
export function CoveragePipeline({
  sources,
  sinks,
}: {
  sources: DeviceIntegrationProvider[];
  sinks: DeviceIntegrationProvider[];
}): JSX.Element {
  const client = useSdkClient();

  // Org-wide coverage: no provider filter. This is the joined fleet every sink
  // sends — totalDevices from the MDM side, agentActive from the agent side.
  const { data: coverage } = useDeviceIntegrationCoverage(
    undefined,
    undefined,
    { throwOnError: false, staleTime: 30_000 },
  );

  // "Connected" matches the row badge: a config that is enabled. The queries
  // share cache keys with the connection rows, and the enable/disable mutation
  // invalidates all config queries, so toggling a connection re-derives these
  // counts with no extra wiring.
  const providers = useMemo(() => [...sources, ...sinks], [sources, sinks]);
  const configQueries = useQueries({
    queries: providers.map((provider) => ({
      ...buildDeviceIntegrationConfigQuery(client, { provider: provider.id }),
      staleTime: 30_000,
    })),
  });
  const connectedIds = useMemo(() => {
    const ids = new Set<string>();
    providers.forEach((provider, i) => {
      if (configQueries[i]?.data?.enabled) ids.add(provider.id);
    });
    return ids;
  }, [providers, configQueries]);

  const sourceConnected = sources.filter((p) => connectedIds.has(p.id)).length;
  const sinkConnected = sinks.filter((p) => connectedIds.has(p.id)).length;

  const total = coverage?.totalDevices ?? 0;
  const running = coverage?.agentActive ?? 0;

  // Under user-level matching an active count can mean the assigned user's
  // agent is live on ANOTHER device, not this one — so "running the agent"
  // would overclaim. Only assert the device-local phrasing when the server
  // reports device attestation for every active device; otherwise fall back
  // to the weaker match wording (and default weak while coverage loads).
  const deviceAttested = coverage?.attestation === "device";
  const agentLabel = deviceAttested ? "Running the agent" : "Active agents";
  const agentHint = deviceAttested
    ? "with a live device-agent heartbeat"
    : "assigned user has a live agent";

  return (
    <div className="border-border bg-card border p-4">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-[minmax(0,0.95fr)_auto_minmax(0,1.2fr)_auto_minmax(0,0.75fr)] md:items-stretch">
        {/* Two inputs, stacked. Each row is one operand of the coverage join. */}
        <div className="flex flex-col gap-3">
          <InputNode
            label="Managed devices"
            value={total}
            hint={sourcesHint(sourceConnected, sources.length)}
            foot="pulled from your MDMs"
          />
          <InputNode
            label={agentLabel}
            value={running}
            hint={agentHint}
            foot="reported by device agent"
          />
        </div>

        <MergeArrows />
        <CoverageNode coverage={coverage} total={total} running={running} />
        <SingleArrow />

        <div className="flex flex-col justify-center">
          <PipelineEnd
            label="Evidence destinations"
            count={sinkConnected}
            noun="connected"
            verb="push evidence"
            alignEnd
          />
        </div>
      </div>
    </div>
  );
}

function sourcesHint(connected: number, available: number): string {
  if (available === 0) return "no MDM sources available";
  const noun = available === 1 ? "source" : "sources";
  return `${connected} of ${available} ${noun} connected`;
}

// One operand of the coverage join: a metric with the source of the number
// named beneath it, so the two inputs read as distinct data origins rather
// than one MDM feed.
function InputNode({
  label,
  value,
  hint,
  foot,
}: {
  label: string;
  value: number;
  hint: string;
  foot: string;
}) {
  return (
    <div className="bg-muted/40 flex flex-col gap-1 p-3">
      <Text
        variant="small"
        className="text-muted-foreground font-mono text-[10.5px] tracking-wider uppercase"
      >
        {label}
      </Text>
      <Stack direction="horizontal" align="baseline" gap={1.5}>
        <Text variant="body" className="text-2xl font-semibold tabular-nums">
          {value}
        </Text>
        <Text muted small>
          {hint}
        </Text>
      </Stack>
      <Text
        variant="small"
        className="text-muted-foreground/70 font-mono text-[10px] tracking-wide uppercase"
      >
        {foot}
      </Text>
    </div>
  );
}

// The join result. Emphasized (spectrum-topped) because it is the one figure
// that belongs to neither input alone — running ÷ managed.
function CoverageNode({
  coverage,
  total,
  running,
}: {
  coverage: DeviceIntegrationCoverage | undefined;
  total: number;
  running: number;
}) {
  if (!coverage || total === 0) {
    return (
      <div className="border-border/60 bg-muted/20 flex flex-col justify-center gap-1 border border-dashed p-3">
        <Text
          variant="small"
          className="text-muted-foreground font-mono text-[10.5px] tracking-wider uppercase"
        >
          Agent coverage
        </Text>
        <Text muted small>
          No devices yet — connect an inventory source to build your fleet.
        </Text>
      </div>
    );
  }

  const percent = Math.floor((running / total) * 100);
  const activePct = (running / total) * 100;
  const stalePct = (coverage.agentStale / total) * 100;

  return (
    <div className="border-border relative flex flex-col justify-center gap-2 overflow-hidden border p-3">
      <span
        aria-hidden
        className="absolute inset-x-0 top-0 h-[3px]"
        style={{
          background:
            "linear-gradient(90deg,#e11d48,#f59e0b,#10b981,#3b82f6,#8b5cf6)",
        }}
      />
      <Text
        variant="small"
        className="text-muted-foreground font-mono text-[10.5px] tracking-wider uppercase"
      >
        Agent coverage
      </Text>
      <Stack direction="horizontal" align="baseline" gap={2}>
        <Text
          variant="body"
          className="text-3xl font-semibold tabular-nums text-emerald-600 dark:text-emerald-400"
        >
          {percent}%
        </Text>
        <Text muted small>
          covered · {running} of {total} devices
        </Text>
      </Stack>
      {/* The track needs its own contrast: bg-muted collapses to the card
          color in dark mode, which hides the uncovered remainder and makes the
          filled portion read as far more than its true share. foreground/15
          stays visible on both grounds. */}
      <div
        className="bg-foreground/15 flex h-2 overflow-hidden rounded-full"
        role="img"
        aria-label={`${running} running the agent, ${coverage.agentStale} stale, of ${total} devices`}
      >
        <div
          className="h-full bg-emerald-600 dark:bg-emerald-500"
          style={{ width: `${activePct}%` }}
        />
        <div
          className="h-full bg-amber-500"
          style={{ width: `${stalePct}%` }}
        />
      </div>
    </div>
  );
}

// Two inputs converging into coverage. Each arrow sits beside its input node
// (both columns split their height equally), so the merge reads correctly.
function MergeArrows() {
  return (
    <div className="text-muted-foreground/50 hidden flex-col md:flex">
      <div className="flex flex-1 items-center justify-center">
        <ArrowRight className="size-5" aria-hidden />
      </div>
      <div className="flex flex-1 items-center justify-center">
        <ArrowRight className="size-5" aria-hidden />
      </div>
    </div>
  );
}

function SingleArrow() {
  return (
    <div className="text-muted-foreground/50 hidden items-center justify-center md:flex">
      <ArrowRight className="size-5" aria-hidden />
    </div>
  );
}

function PipelineEnd({
  label,
  count,
  noun,
  verb,
  alignEnd = false,
}: {
  label: string;
  count: number;
  noun: string;
  verb: string;
  alignEnd?: boolean;
}) {
  return (
    <Stack gap={1} className={alignEnd ? "md:text-right" : undefined}>
      <Text
        variant="small"
        className="text-muted-foreground font-mono text-[10.5px] tracking-wider uppercase"
      >
        {label}
      </Text>
      <Stack
        direction="horizontal"
        align="baseline"
        gap={1.5}
        className={alignEnd ? "md:justify-end" : undefined}
      >
        <Text variant="body" className="text-2xl font-semibold tabular-nums">
          {count}
        </Text>
        <Text muted small>
          {count === 1 ? noun.replace(/s$/, "") : noun}
        </Text>
      </Stack>
      <Text
        variant="small"
        className="text-muted-foreground/70 font-mono text-[10px] tracking-wide uppercase"
      >
        {verb}
      </Text>
    </Stack>
  );
}
