import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { ChartCard } from "@/components/chart/ChartCard";
import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { IdentityLink } from "@/components/identity-link";
import { useCanViewToolLogs } from "@/components/observe/observeDeepLink";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Icon } from "@/components/ui/Icon";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Skeleton, SkeletonTable } from "@/components/ui/Skeleton";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useProject } from "@/contexts/Auth";
import type { IdentityRef } from "@/lib/identity-urn";
import type { ActiveLogFilter } from "@/pages/logs/log-filter-types";
import type { GatewayConnectionHotspot } from "@gram/client/models/components/gatewayconnectionhotspot.js";
import type { GatewayConnectionStage } from "@gram/client/models/components/gatewayconnectionstage.js";
import type { RankBy } from "@gram/client/models/components/listgatewayconnectionhotspotspayload.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildGatewayConnectionHealthQuery } from "@gram/client/react-query/gatewayConnectionHealth.core.js";
import { buildGatewayConnectionHotspotsQuery } from "@gram/client/react-query/gatewayConnectionHotspots.core.js";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
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

// What a hotspot row counts toward, in the stage's own units.
const FAILING: Record<HealthStage, string> = {
  inbound_authorize: "failed or blocked requests",
  inbound_token: "failed or blocked requests",
  upstream_callback: "failed sign-ins",
  upstream_refresh: "failed refreshes",
  discovery_listing: "failures",
  discovery_member: "failed or blocked checks",
};

// The most ranked combinations asked for; the server allows no more.
const HOTSPOT_LIMIT = 25;

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
    limit: HOTSPOT_LIMIT,
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

  // A failed refetch keeps its earlier data; show the retry state instead.
  const summary =
    !health.isError && matchesWindow(health.data, from, to)
      ? health.data
      : undefined;
  const ranked =
    !hotspots.isError &&
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
          How clients fared signing in to this gateway, connecting providers,
          and listing its tools {phrase}. Each count is what the gateway
          observed, not whether a redirect arrived, sign-in finished, or a token
          was used.
        </Text>
        <Collapsible>
          <CollapsibleTrigger className="group text-muted-foreground flex items-center gap-1 text-sm underline underline-offset-4">
            <ChevronDown
              aria-hidden="true"
              className="size-3.5 transition-transform group-data-[state=open]:rotate-180"
            />
            What isn&apos;t counted
          </CollapsibleTrigger>
          <CollapsibleContent>
            <ul className="mt-2 list-disc space-y-1 pl-5">
              <li>
                <Text small muted>
                  For tool listings, only Direct listings are recorded.
                </Text>
              </li>
              {summary?.notRecorded.map((gap) => (
                <li key={gap.stage}>
                  <Text small muted>
                    {gap.note}
                  </Text>
                </li>
              ))}
              {!summary && (
                <li>
                  <Text small muted>
                    Coverage notes are unavailable.
                  </Text>
                </li>
              )}
            </ul>
          </CollapsibleContent>
        </Collapsible>
      </div>
      <Page.Toolbar>
        <Page.Toolbar.Leading>
          <Select
            value={stageName}
            onValueChange={(value) => setStageName(value as HealthStage)}
          >
            <SelectTrigger aria-label="Connection stage" className="w-48">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {STAGE_OPTIONS.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
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
              capped={(ranked?.hotspots.length ?? 0) >= HOTSPOT_LIMIT}
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
  capped,
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
  // The server returned as many combinations as asked for, so additional
  // combinations may exist beyond them.
  capped: boolean;
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
      render: (row) => {
        // Only a person or agent the reader may see carries an id.
        let identifier: IdentityRef | null = null;
        const { id, kind } = row.subject;
        if (id && (kind === "user" || kind === "agent")) {
          identifier = { urn: id };
        }
        return (
          <Text className="truncate" title={row.subject.label}>
            <IdentityLink identifier={identifier}>
              {row.subject.label}
            </IdentityLink>
          </Text>
        );
      },
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
        <span className="flex min-w-0 items-center gap-2">
          {/* Only a registered client's name can name its vendor; every other
              kind is a description and takes the generic mark. */}
          <AgentProviderIcon
            source={row.client.kind === "registered" ? row.client.label : ""}
            className="text-muted-foreground size-4 shrink-0"
          />
          <Text muted className="truncate" title={row.client.label}>
            {row.client.label}
          </Text>
        </span>
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
    ...(["discovery_member", "inbound_authorize", "inbound_token"].includes(
      stage,
    )
      ? [
          {
            key: "blocked",
            header: (
              <span className="inline-flex items-center gap-1.5">
                Blocked
                <SimpleTooltip
                  tooltip={
                    stage === "discovery_member"
                      ? "Needs sign-in, updated connection settings, or consent. View the row for the reason."
                      : "Refused by an administrator's client or workload policy. View the row for the reason."
                  }
                >
                  <button
                    type="button"
                    aria-label="About Blocked"
                    className="text-muted-foreground hover:text-foreground inline-flex cursor-help items-center"
                  >
                    <Icon name="info" className="size-3" />
                  </button>
                </SimpleTooltip>
              </span>
            ),
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
          {row.reasons[0] ? reasonText(row.reasons[0].reason, stage) : "—"}
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

  const ranking = rankBy === "count" ? "failures" : "failure rate";
  const failing = FAILING[stage];
  if (rows.length === 0) {
    return (
      <WidgetEmptyState
        message={
          capped
            ? `None of the top ${HOTSPOT_LIMIT} combinations ranked by ${ranking} had ${failing} ${phrase}${minAttempts !== "1" ? `, with ${floor} ${stageLabel} each` : ""}.`
            : `Nothing failed ${phrase} for anyone with ${floor} ${stageLabel}.`
        }
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
        {capped
          ? `${rows.length} with ${failing} among the top ${HOTSPOT_LIMIT} combinations ranked by ${ranking}`
          : `${rows.length} with ${failing}, ranked by ${ranking}`}
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
