import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import type { DeviceIntegrationCoverage } from "@gram/client/models/components/deviceintegrationcoverage.js";
import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";
import { buildDeviceIntegrationConfigQuery } from "@gram/client/react-query/deviceIntegrationConfig.js";
import { useDeviceIntegrationCoverage } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueries } from "@tanstack/react-query";
import { ArrowRight } from "lucide-react";
import { useMemo } from "react";

// The pipeline banner: the spine of the page. It makes the data flow legible
// at a glance — inventory sources on the left PULL the fleet, the org-wide
// coverage in the middle is the shared truth, and evidence destinations on the
// right PUSH it out. The middle is deliberately the emphasized node because it
// is the one thing that belongs to neither side: it is the join of every
// source's inventory against agent heartbeats, and it is exactly what every
// sink republishes. Making it central is what stops a sink page from looking
// like it "owns" devices it merely forwards.
export function CoveragePipeline({
  sources,
  sinks,
}: {
  sources: DeviceIntegrationProvider[];
  sinks: DeviceIntegrationProvider[];
}): JSX.Element {
  const client = useSdkClient();

  // Org-wide coverage: no provider filter. This is the fleet every sink sends.
  const { data: coverage } = useDeviceIntegrationCoverage(
    undefined,
    undefined,
    {
      throwOnError: false,
      staleTime: 30_000,
    },
  );

  // "Connected" here matches the row badge: a config that is enabled (which
  // implies configured). The queries share cache keys with the connection
  // rows, and the enable/disable mutation invalidates all config queries, so
  // toggling a single connection re-derives these counts with no extra wiring.
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

  return (
    <div className="border-border bg-card grid grid-cols-1 items-stretch gap-3 rounded-lg border p-4 md:grid-cols-[1fr_auto_1.4fr_auto_1fr]">
      <PipelineEnd
        label="Inventory sources"
        count={sourceConnected}
        noun="connected"
        verb="pull inventory"
      />
      <FlowArrow />
      <CoverageNode coverage={coverage} />
      <FlowArrow />
      <PipelineEnd
        label="Evidence destinations"
        count={sinkConnected}
        noun="connected"
        verb="push evidence"
        alignEnd
      />
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
      <Type
        variant="small"
        className="text-muted-foreground font-mono text-[10.5px] tracking-wider uppercase"
      >
        {label}
      </Type>
      <Stack
        direction="horizontal"
        align="baseline"
        gap={1.5}
        className={alignEnd ? "md:justify-end" : undefined}
      >
        <Type variant="body" className="text-2xl font-semibold tabular-nums">
          {count}
        </Type>
        <Type muted small>
          {count === 1 ? noun.replace(/s$/, "") : noun}
        </Type>
      </Stack>
      <Type
        variant="small"
        className="text-muted-foreground/70 font-mono text-[10px] tracking-wide uppercase"
      >
        {verb}
      </Type>
    </Stack>
  );
}

// Horizontal on desktop, hidden on the stacked mobile layout where the reading
// order already implies the flow.
function FlowArrow() {
  return (
    <div className="text-muted-foreground/50 hidden items-center justify-center md:flex">
      <ArrowRight className="size-5" aria-hidden />
    </div>
  );
}

function CoverageNode({
  coverage,
}: {
  coverage: DeviceIntegrationCoverage | undefined;
}) {
  const total = coverage?.totalDevices ?? 0;

  if (!coverage || total === 0) {
    return (
      <div className="bg-muted/40 border-border/60 flex flex-col justify-center gap-1 rounded-md border border-dashed p-3">
        <Type
          variant="small"
          className="text-muted-foreground font-mono text-[10.5px] tracking-wider uppercase"
        >
          Agent coverage · your fleet
        </Type>
        <Type muted small>
          No devices yet — connect an inventory source to build your fleet.
        </Type>
      </div>
    );
  }

  const percent = Math.floor((coverage.agentActive / total) * 100);
  const activePct = (coverage.agentActive / total) * 100;
  const stalePct = (coverage.agentStale / total) * 100;

  return (
    <div className="bg-muted/30 flex flex-col justify-center gap-2 rounded-md p-3">
      <Type
        variant="small"
        className="text-muted-foreground font-mono text-[10.5px] tracking-wider uppercase"
      >
        Agent coverage · your fleet
      </Type>
      <Stack direction="horizontal" align="baseline" gap={2}>
        <Type variant="body" className="text-3xl font-semibold tabular-nums">
          {total}
        </Type>
        <Type muted small>
          managed devices ·{" "}
          <span className="font-medium text-emerald-600 tabular-nums dark:text-emerald-400">
            {percent}% covered
          </span>
        </Type>
      </Stack>
      <div
        className="bg-muted flex h-2 overflow-hidden rounded-full"
        role="img"
        aria-label={`${coverage.agentActive} running the agent, ${coverage.agentStale} stale, of ${total} devices`}
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
