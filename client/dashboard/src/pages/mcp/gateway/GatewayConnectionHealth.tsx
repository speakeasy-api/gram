import { ChartCard } from "@/components/chart/ChartCard";
import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { useCanViewToolLogs } from "@/components/observe/observeDeepLink";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton, SkeletonTable } from "@/components/ui/Skeleton";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useProject } from "@/contexts/Auth";
import type { ActiveLogFilter } from "@/pages/logs/log-filter-types";
import type { GatewayConnectionHotspot } from "@gram/client/models/components/gatewayconnectionhotspot.js";
import type { GatewayConnectionStage } from "@gram/client/models/components/gatewayconnectionstage.js";
import type { RankBy } from "@gram/client/models/components/listgatewayconnectionhotspotspayload.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildGatewayConnectionHealthQuery } from "@gram/client/react-query/gatewayConnectionHealth.core.js";
import { buildGatewayConnectionHotspotsQuery } from "@gram/client/react-query/gatewayConnectionHotspots.core.js";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { ConnectionHealthSheet } from "./ConnectionHealthSheet";
import {
  HEALTH_WINDOWS,
  STAGE_OPTIONS,
  WINDOW_PHRASE,
  failureRate,
  formatTime,
  healthWindow,
  hotspotKey,
  isUnavailable,
  matchesWindow,
  reasonText,
  stageChartData,
  stageTiles,
  type HealthStage,
  type HealthWindow,
} from "./connectionHealth";

const RANK_OPTIONS: { value: RankBy; label: string }[] = [
  { value: "count", label: "Most failures" },
  { value: "rate", label: "Highest failure rate" },
];

const MIN_ATTEMPT_OPTIONS = [
  { value: "1", label: "Any" },
  { value: "5", label: "5+" },
  { value: "20", label: "20+" },
];

const CHART_ID = "gateway-connection-health";

type HotspotRow = GatewayConnectionHotspot & { position: number };

/**
 * Listing and member-check outcomes on one gateway over one shared window,
 * with the callers whose connections fail most. Opening a caller shows that
 * key's own timeline and recent attempts.
 */
export function GatewayConnectionHealth({
  gatewayId,
  onShowConnections,
}: {
  gatewayId: string;
  onShowConnections: (filters: ActiveLogFilter[]) => void;
}): JSX.Element | null {
  const project = useProject();
  const canRead = useCanViewToolLogs();
  const [span, setSpan] = useState<HealthWindow>("7d");
  const [stageName, setStageName] = useState<HealthStage>("discovery_listing");
  const [rankBy, setRankBy] = useState<RankBy>("count");
  const [minAttempts, setMinAttempts] = useState("5");
  const [now, setNow] = useState(() => new Date());
  const [expandedChart, setExpandedChart] = useState<string | null>(null);
  const [selected, setSelected] = useState<GatewayConnectionHotspot | null>(
    null,
  );
  const { from, to } = useMemo(() => healthWindow(span, now), [span, now]);
  const phrase = WINDOW_PHRASE[span];

  // The generated keys leave out the request body, so each payload joins
  // its key. No placeholder data: a result is only shown for the controls it
  // answers.
  const client = useGramContext();
  const options = { enabled: canRead, throwOnError: false, retry: false };
  const healthPayload = { metaMcpServerId: gatewayId, from, to };
  const healthQuery = buildGatewayConnectionHealthQuery(client, {
    gramProject: project.slug,
    getGatewayConnectionHealthPayload: healthPayload,
  });
  const health = useQuery({
    ...healthQuery,
    queryKey: [...healthQuery.queryKey, healthPayload],
    ...options,
  });
  const hotspotsPayload = {
    ...healthPayload,
    stage: stageName,
    rankBy,
    minAttempts: Number(minAttempts),
  };
  const hotspotsQuery = buildGatewayConnectionHotspotsQuery(client, {
    gramProject: project.slug,
    listGatewayConnectionHotspotsPayload: hotspotsPayload,
  });
  const hotspots = useQuery({
    ...hotspotsQuery,
    queryKey: [...hotspotsQuery.queryKey, hotspotsPayload],
    ...options,
  });

  const summary = matchesWindow(health.data, from, to)
    ? health.data
    : undefined;
  const ranked =
    matchesWindow(hotspots.data, from, to) &&
    hotspots.data?.stage === stageName &&
    hotspots.data.rankBy === rankBy &&
    hotspots.data.minAttempts === Number(minAttempts)
      ? hotspots.data
      : undefined;
  const stage = summary?.stages.find((s) => s.stage === stageName);
  const chart = useMemo(
    () =>
      stage && summary
        ? stageChartData(stage, summary.intervalSeconds, from, to)
        : undefined,
    [stage, summary, from, to],
  );
  const rows = useMemo<HotspotRow[]>(
    () =>
      // The ranking can include callers with nothing failing or blocked;
      // they are not where connections fail.
      (ranked?.hotspots ?? [])
        .map((hotspot, position) => ({ ...hotspot, position }))
        .filter((row) => row.failed > 0 || row.blocked > 0),
    [ranked],
  );

  if (!canRead) return null;

  const stageLabel =
    STAGE_OPTIONS.find((option) => option.value === stageName)?.label ?? "";
  const unavailable =
    isUnavailable(health.error) || isUnavailable(hotspots.error);

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-heading-sm">Connection health</h2>
        <Text small muted>
          How clients fared listing this gateway&apos;s tools {phrase}. Sign-in
          and token refresh are not recorded yet, so a successful sign-in is not
          shown. Only Direct tool listings are recorded.
        </Text>
      </div>
      <Page.Toolbar>
        <Page.Toolbar.Leading>
          <SegmentedControl
            value={stageName}
            onChange={setStageName}
            options={STAGE_OPTIONS}
            ariaLabel="Connection stage"
          />
        </Page.Toolbar.Leading>
        <Page.Toolbar.Actions>
          <SegmentedControl
            value={span}
            onChange={setSpan}
            options={HEALTH_WINDOWS}
            ariaLabel="Time window"
          />
        </Page.Toolbar.Actions>
        <Page.Toolbar.Refresh
          onRefresh={() => setNow(new Date())}
          isRefreshing={health.isFetching || hotspots.isFetching}
        />
      </Page.Toolbar>
      {unavailable ? (
        <WidgetEmptyState
          message="Connection health is not available to you for this gateway. It needs logs turned on for the organization and access to this gateway's logs."
          className="h-[200px]"
        />
      ) : (
        <>
          <StageSummary
            stage={stage}
            stageLabel={stageLabel}
            phrase={phrase}
            recorded={summary !== undefined}
            failed={health.isError}
            onRetry={() => void health.refetch()}
          />
          {stage && stage.attempts > 0 && (
            <ChartCard
              title={`${stageLabel} over time`}
              chartId={CHART_ID}
              expandedChart={expandedChart}
              onExpand={setExpandedChart}
              hasData={chart !== undefined}
            >
              {chart && (
                <>
                  <StackedTimeBarChart
                    {...chart}
                    height={expandedChart === CHART_ID ? 420 : 240}
                    expanded={expandedChart === CHART_ID}
                  />
                  <Text muted small>
                    UTC intervals of {intervalLabel(summary!.intervalSeconds)}.
                    {stage.lastSeenAt &&
                      ` Latest attempt ${formatTime(stage.lastSeenAt)}.`}
                  </Text>
                </>
              )}
            </ChartCard>
          )}
          <div className="space-y-3">
            <h3 className="text-eyebrow">Where connections fail</h3>
            <Page.Toolbar>
              <Page.Toolbar.Leading>
                <SegmentedControl
                  value={rankBy}
                  onChange={setRankBy}
                  options={RANK_OPTIONS}
                  ariaLabel="Rank callers by"
                />
              </Page.Toolbar.Leading>
              <Page.Toolbar.Actions>
                <SegmentedControl
                  value={minAttempts}
                  onChange={setMinAttempts}
                  options={MIN_ATTEMPT_OPTIONS}
                  ariaLabel="Minimum attempts"
                />
              </Page.Toolbar.Actions>
            </Page.Toolbar>
            <Hotspots
              rows={ranked ? rows : undefined}
              failed={hotspots.isError}
              onRetry={() => void hotspots.refetch()}
              stage={stageName}
              stageLabel={stageLabel.toLowerCase()}
              rankBy={rankBy}
              minAttempts={minAttempts}
              phrase={phrase}
              onSelect={setSelected}
            />
          </div>
        </>
      )}
      {selected && (
        <ConnectionHealthSheet
          gatewayId={gatewayId}
          stage={stageName}
          hotspot={selected}
          from={from}
          to={to}
          windowPhrase={phrase}
          onClose={() => setSelected(null)}
          onShowConnections={(filters) => {
            setSelected(null);
            onShowConnections(filters);
          }}
        />
      )}
    </div>
  );
}

function RetryLine({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}): JSX.Element {
  return (
    <div className="flex items-center justify-between gap-3">
      <p className="text-destructive text-sm">{message}</p>
      <Button variant="tertiary" size="sm" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}

function StageSummary({
  stage,
  stageLabel,
  phrase,
  recorded,
  failed,
  onRetry,
}: {
  stage: GatewayConnectionStage | undefined;
  stageLabel: string;
  phrase: string;
  recorded: boolean;
  failed: boolean;
  onRetry: () => void;
}): JSX.Element {
  if (failed && !recorded) {
    return (
      <RetryLine message="Couldn't load connection health." onRetry={onRetry} />
    );
  }
  if (!recorded) {
    return (
      <StatTileGroup className="overflow-x-auto [&>*]:min-w-[11.5rem]">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-[116px] flex-1" />
        ))}
      </StatTileGroup>
    );
  }
  if (!stage) {
    return (
      <WidgetEmptyState
        message={`${stageLabel} are not recorded on this server yet.`}
        className="h-[200px]"
      />
    );
  }
  if (stage.attempts === 0) {
    return (
      <WidgetEmptyState
        message={`No ${stageLabel.toLowerCase()} were recorded ${phrase}. Without attempts there is nothing to show about connection health.`}
        className="h-[200px]"
      />
    );
  }
  return (
    <StatTileGroup className="overflow-x-auto [&>*]:min-w-[11.5rem]">
      {stageTiles(stage).map((tile) => (
        <StatTile key={tile.title} format="number" {...tile} />
      ))}
    </StatTileGroup>
  );
}

function Hotspots({
  rows,
  failed,
  onRetry,
  stage,
  stageLabel,
  rankBy,
  minAttempts,
  phrase,
  onSelect,
}: {
  rows: HotspotRow[] | undefined;
  failed: boolean;
  onRetry: () => void;
  stage: HealthStage;
  stageLabel: string;
  rankBy: RankBy;
  minAttempts: string;
  phrase: string;
  onSelect: (hotspot: GatewayConnectionHotspot) => void;
}): JSX.Element {
  if (failed && !rows) {
    return (
      <RetryLine
        message="Couldn't load where connections fail."
        onRetry={onRetry}
      />
    );
  }
  if (!rows) return <SkeletonTable />;

  const floor = minAttempts === "1" ? "any" : `at least ${minAttempts}`;
  const columns: Column<HotspotRow>[] = [
    {
      key: "subject",
      header: "Person or agent",
      width: "1.5fr",
      render: (row) => (
        <Text className="truncate" title={row.subject.label}>
          {row.subject.label}
        </Text>
      ),
    },
    {
      key: "provider",
      header: "Provider",
      width: "1.5fr",
      render: (row) => (
        <Text muted className="truncate" title={row.provider.label}>
          {row.provider.label}
        </Text>
      ),
    },
    {
      key: "client",
      header: "Client",
      width: "1.5fr",
      render: (row) => (
        <Text muted className="truncate" title={row.client.label}>
          {row.client.label}
        </Text>
      ),
    },
    {
      key: "failed",
      header: "Failed",
      width: "96px",
      render: (row) => (
        <Text>
          {row.failed.toLocaleString()} of {row.attempts.toLocaleString()}
        </Text>
      ),
    },
    ...(stage === "discovery_member"
      ? [
          {
            key: "blocked",
            header: "Needs sign-in or consent",
            width: "120px",
            render: (row: HotspotRow) => (
              <Text>{row.blocked.toLocaleString()}</Text>
            ),
          } satisfies Column<HotspotRow>,
        ]
      : []),
    {
      key: "rate",
      header: "Failure rate",
      width: "120px",
      render: (row) => <Text>{failureRate(row)}</Text>,
    },
    {
      key: "reasons",
      header: "Top reason",
      width: "2fr",
      render: (row) => (
        <Text muted small className="line-clamp-2">
          {row.reasons[0] ? reasonText(row.reasons[0].reason) : "—"}
        </Text>
      ),
    },
    {
      key: "action",
      header: "",
      width: "auto",
      render: (row) =>
        hotspotKey(stage, row) ? (
          <Button variant="tertiary" size="sm" onClick={() => onSelect(row)}>
            View
          </Button>
        ) : (
          <SimpleTooltip tooltip="This person, provider, or client is not visible in this project.">
            <Text muted small className="whitespace-nowrap">
              Not visible to you
            </Text>
          </SimpleTooltip>
        ),
    },
  ];

  if (rows.length === 0) {
    return (
      <WidgetEmptyState
        message={`Nothing failed ${phrase} for anyone with ${floor} ${stageLabel}.`}
        className="h-[120px]"
      />
    );
  }

  return (
    <>
      <div className="overflow-x-auto">
        <Table
          className="min-w-[960px]"
          columns={columns}
          data={rows}
          // Hidden rows carry no key and may share labels, so rows are keyed
          // by their place in this response.
          rowKey={(row) => row.position}
        />
      </div>
      <Text muted small>
        {`${rows.length} with ${stage === "discovery_member" ? "failed or blocked checks" : "failures"}, ranked by ${rankBy === "count" ? "failures" : "failure rate"}`}
        {minAttempts !== "1" && `, with ${floor} ${stageLabel} each`} {phrase}.
      </Text>
    </>
  );
}

function intervalLabel(seconds: number): string {
  if (seconds % 3600 === 0) {
    const hours = seconds / 3600;
    return hours === 1 ? "1 hour" : `${hours} hours`;
  }
  return `${Math.round(seconds / 60)} minutes`;
}
