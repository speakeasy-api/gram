import { ChartLegend } from "@/components/ui/ChartLegend";
import { overviewLabels } from "@/components/chart/overviewLabels";
import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject } from "@/contexts/Auth";
import type { ActiveLogFilter } from "@/pages/logs/log-filter-types";
import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import type { GatewayConnectionCaller } from "@gram/client/models/components/gatewayconnectioncaller.js";
import type { GatewayConnectionHotspot } from "@gram/client/models/components/gatewayconnectionhotspot.js";
import type { GatewayConnectionStage } from "@gram/client/models/components/gatewayconnectionstage.js";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildGatewayConnectionHealthQuery } from "@gram/client/react-query/gatewayConnectionHealth.core.js";
import { buildGatewayConnectionHotspotsQuery } from "@gram/client/react-query/gatewayConnectionHotspots.core.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import {
  STAGE_OPTIONS,
  attemptFilters,
  callerOf,
  sameCaller,
  beforeWindowEnd,
  countOf,
  failureRate,
  formatTime,
  grantLabel,
  hotspotKey,
  isUnavailable,
  matchesWindow,
  matchingConnectionFilters,
  outcomeLabel,
  reasonText,
  stageChartData,
  stageTiles,
  toAttempt,
  type Attempt,
  type HealthStage,
} from "./connectionHealth";

const PAGE_SIZE = 20;

/**
 * One hotspot key over the section's window: its own timeline, why it fails,
 * and the latest attempts, with a way to narrow the connections below to it.
 * Everything waits for the keyed health read, which is where the server
 * decides whether this key is visible to the caller.
 */
export function ConnectionHealthSheet({
  gatewayId,
  stage,
  hotspot,
  from,
  to,
  windowPhrase,
  onClose,
  onShowConnections,
}: {
  gatewayId: string;
  stage: HealthStage;
  hotspot: GatewayConnectionHotspot;
  from: Date;
  to: Date;
  windowPhrase: string;
  onClose: () => void;
  onShowConnections: (filters: ActiveLogFilter[]) => void;
}): JSX.Element | null {
  const organization = useOrganization();
  const project = useProject();
  const client = useGramContext();
  const key = hotspotKey(stage, hotspot);
  const caller = key ? callerOf(key) : null;
  const filters = useMemo(
    () => (key ? attemptFilters(gatewayId, key) : []),
    [gatewayId, key],
  );

  const healthPayload = {
    metaMcpServerId: gatewayId,
    from,
    to,
    key: key ?? undefined,
  };
  const healthQuery = buildGatewayConnectionHealthQuery(client, {
    gramProject: project.slug,
    getGatewayConnectionHealthPayload: healthPayload,
  });
  const health = useQuery({
    ...healthQuery,
    queryKey: [...healthQuery.queryKey, healthPayload],
    enabled: key !== null,
    // Cached data from an earlier opening is not this caller's answer now:
    // every opening asks the server again, whatever the stale time.
    refetchOnMount: "always",
    throwOnError: false,
    retry: false,
  });
  // A failed refetch keeps its earlier data, so a refusal must hide it too,
  // and only a read since this opening counts.
  const keyed =
    !health.isError &&
    health.isFetchedAfterMount &&
    matchesWindow(health.data, from, to)
      ? health.data?.stages.find((s) => s.stage === stage)
      : undefined;
  // Raw attempts are read only once the server has just confirmed this key is
  // visible, never while that answer is still pending.
  const rawReady = keyed !== undefined && !health.isFetching;
  const queryKey = [
    organization.id,
    project.id,
    gatewayId,
    filters,
    from.getTime(),
    to.getTime(),
  ];

  // SearchLogs ends inclusively on a millisecond date, so the window end is
  // sent as is and attempts at or after it are dropped here. When that drops
  // rows from a full page, one more page refills the list.
  const recent = useQuery({
    queryKey: ["gateway-connection-attempts", ...queryKey],
    queryFn: async () => {
      let logs: TelemetryLogRecord[] = [];
      let cursor: string | undefined;
      for (let page = 0; page < 2; page++) {
        const result = await unwrapAsync(
          telemetrySearchLogs(client, {
            gramProject: project.slug,
            searchLogsPayload: {
              from,
              to,
              filters,
              sort: "desc",
              limit: PAGE_SIZE,
              cursor,
            },
          }),
        );
        const kept = beforeWindowEnd(result.logs, to);
        logs = [...logs, ...kept];
        cursor = result.nextCursor;
        if (kept.length === result.logs.length || !cursor) break;
      }
      return {
        attempts: logs.slice(0, PAGE_SIZE).map(toAttempt),
        more: logs.length > PAGE_SIZE || cursor !== undefined,
      };
    },
    enabled: rawReady,
    throwOnError: false,
    retry: false,
  });

  // The start bound is exact, so the oldest attempt before the end is the
  // first in this window; it says nothing about earlier windows.
  const first = useQuery({
    queryKey: ["gateway-connection-first-attempt", ...queryKey],
    queryFn: async () => {
      const result = await unwrapAsync(
        telemetrySearchLogs(client, {
          gramProject: project.slug,
          searchLogsPayload: { from, to, filters, sort: "asc", limit: 1 },
        }),
      );
      const log = beforeWindowEnd(result.logs, to)[0];
      return log ? toAttempt(log).at : null;
    },
    enabled: rawReady,
    throwOnError: false,
    retry: false,
  });

  if (!key) return null;
  const stageLabel =
    STAGE_OPTIONS.find((option) => option.value === stage)?.label ?? "";

  let body: ReactNode;
  if (isUnavailable(health.error)) {
    body = (
      <Text small muted className="block px-4">
        This connection is not visible to you. It may belong to someone outside
        this project, or its logs are no longer readable.
      </Text>
    );
  } else if (health.isError) {
    body = (
      <div className="flex items-center justify-between gap-3 px-4">
        <Text small className="text-destructive">
          Couldn&apos;t load this connection.
        </Text>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => void health.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  } else if (!keyed || !health.data) {
    body = <Skeleton className="mx-4 h-64" />;
  } else {
    body = (
      <KeyedDetail
        stage={keyed}
        intervalSeconds={health.data.intervalSeconds}
        hotspot={hotspot}
        from={from}
        to={to}
        first={first}
        recent={recent}
        related={
          stage === "discovery_listing" &&
          (caller ? (
            <CallerFacts
              gatewayId={gatewayId}
              caller={caller}
              from={from}
              to={to}
              windowPhrase={windowPhrase}
              ready={rawReady}
            />
          ) : (
            <section className="space-y-2">
              <h3 className="text-eyebrow">This person and client</h3>
              <Text small muted>
                Sign-ins and provider activity can be matched only to a
                signed-in person or agent using a registered client, so none are
                shown here.
              </Text>
            </section>
          ))
        }
      />
    );
  }

  const matching = matchingConnectionFilters(key);
  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent className="flex w-full flex-col overflow-y-auto sm:max-w-xl">
        <SheetHeader className="border-b pb-4">
          <Text className="text-eyebrow">Failure hot spot</Text>
          <SheetTitle>{hotspot.subject.label}</SheetTitle>
          <SheetDescription>
            {stageLabel} · {windowPhrase}
          </SheetDescription>
          <div className="flex flex-wrap gap-2">
            <Badge variant="neutral" className="max-w-full whitespace-normal">
              <span>Provider: {hotspot.provider.label}</span>
            </Badge>
            <Badge variant="neutral" className="max-w-full whitespace-normal">
              <span>Client: {hotspot.client.label}</span>
            </Badge>
          </div>
        </SheetHeader>
        {body}
        {keyed && (
          <SheetFooter className="sticky bottom-0 mt-auto border-t bg-card py-4">
            <MatchingConnections
              matching={matching}
              onShow={() => onShowConnections(matching.filters)}
            />
          </SheetFooter>
        )}
      </SheetContent>
    </Sheet>
  );
}

// CallerFacts shows, beside a listing hotspot, what the gateway recorded for
// the same person or agent and registered client in the same window: sign-in
// token responses, each provider's sign-in callbacks and refreshes, and the
// member checks inside their listings. They are observations beside the
// listings, never their cause, and the person and client may be several app
// instances.
function CallerFacts({
  gatewayId,
  caller,
  from,
  to,
  windowPhrase,
  ready,
}: {
  gatewayId: string;
  caller: GatewayConnectionCaller;
  from: Date;
  to: Date;
  windowPhrase: string;
  ready: boolean;
}): JSX.Element {
  const project = useProject();
  const client = useGramContext();
  const payload = { metaMcpServerId: gatewayId, from, to, caller };
  const built = buildGatewayConnectionHealthQuery(client, {
    gramProject: project.slug,
    getGatewayConnectionHealthPayload: payload,
  });
  const summary = useQuery({
    ...built,
    queryKey: [...built.queryKey, payload],
    enabled: ready,
    // Enabled only after this opening's key check, so cached data must still
    // be asked for again then, whatever the default stale time.
    staleTime: 0,
    refetchOnMount: "always",
    throwOnError: false,
    retry: false,
  });
  // Counted only from a settled read made since this opening, for this
  // window, that says it counted only this caller: a server that ignores the
  // caller scope answers for the whole gateway instead.
  const answered =
    !summary.isError &&
    summary.isFetchedAfterMount &&
    !summary.isFetching &&
    matchesWindow(summary.data, from, to)
      ? summary.data
      : undefined;
  const settled =
    answered && sameCaller(answered.caller, caller) ? answered : undefined;
  const stageOf = (stage: HealthStage) =>
    settled?.stages.find((s) => s.stage === stage);

  let content: ReactNode;
  if (isUnavailable(summary.error)) {
    content = (
      <Text small muted>
        This person or client isn&apos;t visible to you now, so what the gateway
        recorded for them is not shown.
      </Text>
    );
  } else if (summary.isError) {
    content = (
      <RetryLine
        message="Couldn't load what the gateway recorded for this person and client."
        onRetry={() => void summary.refetch()}
      />
    );
  } else if (answered && !settled) {
    content = (
      <Text small muted>
        This server can&apos;t count one person and client on their own yet, so
        nothing is shown for them.
      </Text>
    );
  } else if (!settled) {
    content = <Skeleton className="h-24 w-full" />;
  } else {
    const tokens = stageOf("inbound_token");
    const issued = countOf(tokens?.outcomes ?? [], "code_issued");
    // The one gap specific to counting a caller; the page lists the rest.
    const authorizeGap = settled.notRecorded.find(
      (gap) => gap.stage === "inbound_authorize",
    );
    content = (
      <>
        <div className="space-y-1">
          <h4 className="text-sm font-medium">Sign-in tokens</h4>
          <Text small className="block">
            {issued === 0
              ? `No sign-in token response is recorded for this person and client ${windowPhrase}.`
              : `The gateway wrote ${issued === 1 ? "1 sign-in token response" : `${issued.toLocaleString()} sign-in token responses`} for this person and client ${windowPhrase}.`}
            {issued > 0 &&
              tokens?.lastCodeIssuedAt &&
              ` The latest sign-in code request started ${formatTime(tokens.lastCodeIssuedAt)}.`}
          </Text>
        </div>
        {(
          ["upstream_callback", "upstream_refresh", "discovery_member"] as const
        ).map((stage) => (
          <ProviderFacts
            key={stage}
            gatewayId={gatewayId}
            caller={caller}
            stage={stage}
            summary={stageOf(stage)}
            from={from}
            to={to}
            windowPhrase={windowPhrase}
          />
        ))}
        {authorizeGap && (
          <Text small muted className="block">
            {authorizeGap.note}
          </Text>
        )}
        <Text small muted className="block">
          These events match this person, client and time window, not one
          listing. They may come from different app instances. Saved provider
          credentials and written token responses do not prove tool discovery
          succeeded.
        </Text>
      </>
    );
  }
  return (
    <section className="space-y-3">
      <h3 className="text-eyebrow">This person and client</h3>
      {content}
    </section>
  );
}

type CallerProviderStage =
  | "upstream_callback"
  | "upstream_refresh"
  | "discovery_member";

const PROVIDER_FACTS: Record<
  CallerProviderStage,
  { title: string; none: string; latest: string }
> = {
  upstream_callback: {
    title: "Provider sign-ins",
    none: "No provider sign-in is recorded for this person and client",
    latest: "latest sign-in",
  },
  upstream_refresh: {
    title: "Provider refreshes",
    none: "No provider refresh caused by a request was recorded for this person and client",
    latest: "latest refresh",
  },
  discovery_member: {
    title: "Member checks by provider",
    none: "No member check is recorded for this person and client",
    latest: "latest check",
  },
};

// ProviderFacts lists one stage's outcomes for the caller, one row per
// provider, successful ones included. Its denominator is its own stage. Member
// checks are grouped by their member's provider too, so members without one
// share a row, and each row names its most frequent reason.
function ProviderFacts({
  gatewayId,
  caller,
  stage,
  summary,
  from,
  to,
  windowPhrase,
}: {
  gatewayId: string;
  caller: GatewayConnectionCaller;
  stage: CallerProviderStage;
  summary: GatewayConnectionStage | undefined;
  from: Date;
  to: Date;
  windowPhrase: string;
}): JSX.Element {
  const project = useProject();
  const client = useGramContext();
  const copy = PROVIDER_FACTS[stage];
  const attempts = summary?.attempts ?? 0;
  const member = stage === "discovery_member";
  // Extra calls during recorded member checks. Recovered counts only checks
  // recorded as listed after a retry, a subset of listed.
  const retries = member ? (summary?.retries ?? 0) : 0;
  const recovered = member ? (summary?.recovered ?? 0) : 0;
  let listedAfter = "none recorded as listed after a retry";
  if (recovered > 0) {
    listedAfter = `${recovered.toLocaleString()} listed after a retry`;
  }
  let retryNote = "";
  if (retries > 0) {
    retryNote = ` · ${retries.toLocaleString()} internal ${retries === 1 ? "retry" : "retries"}, ${listedAfter}`;
  } else if (recovered > 0) {
    retryNote = ` · ${listedAfter}`;
  }
  const payload = {
    metaMcpServerId: gatewayId,
    from,
    to,
    stage,
    caller,
    rankBy: "count" as const,
    minAttempts: 1,
    limit: CALLER_PROVIDER_LIMIT,
  };
  const built = buildGatewayConnectionHotspotsQuery(client, {
    gramProject: project.slug,
    listGatewayConnectionHotspotsPayload: payload,
  });
  // Read only once the caller's summary has settled with attempts here.
  const providers = useQuery({
    ...built,
    queryKey: [...built.queryKey, payload],
    enabled: attempts > 0,
    staleTime: 0,
    refetchOnMount: "always",
    throwOnError: false,
    retry: false,
  });
  const answered =
    !providers.isError &&
    providers.isFetchedAfterMount &&
    !providers.isFetching &&
    matchesWindow(providers.data, from, to) &&
    providers.data?.stage === stage
      ? providers.data
      : undefined;
  const ranked =
    answered && sameCaller(answered.caller, caller) ? answered : undefined;

  let content: ReactNode;
  if (attempts === 0) {
    content = (
      <Text small muted>
        {copy.none} {windowPhrase}. That is no evidence either way.
      </Text>
    );
  } else if (isUnavailable(providers.error)) {
    content = (
      <Text small muted>
        This person or client isn&apos;t visible to you now, so their providers
        are not shown.
      </Text>
    );
  } else if (providers.isError) {
    content = (
      <RetryLine
        message="Couldn't load the providers."
        onRetry={() => void providers.refetch()}
      />
    );
  } else if (answered && !ranked) {
    content = (
      <Text small muted>
        This server can&apos;t list one person and client&apos;s providers yet.
      </Text>
    );
  } else if (!ranked) {
    content = <Skeleton className="h-16 w-full" />;
  } else {
    const listed = ranked.hotspots.reduce((sum, row) => sum + row.attempts, 0);
    const unlisted = attempts - listed;
    content = (
      <>
        <ul className="divide-y">
          {ranked.hotspots.map((row, index) => (
            <li
              // Hidden providers share a label and have no key, so they are
              // told apart by position.
              key={
                row.provider.key === undefined
                  ? `hidden-${index}`
                  : `provider-${row.provider.key}`
              }
              className="flex flex-wrap items-baseline justify-between gap-x-3 py-1.5"
            >
              <Text
                small
                className="min-w-0 truncate"
                title={row.provider.label}
              >
                {row.provider.label}
              </Text>
              <Text small muted className="tabular-nums">
                {outcomeSummary(stage, row.outcomes)}
                {row.lastSeenAt &&
                  ` · ${copy.latest} ${formatTime(row.lastSeenAt)}`}
              </Text>
              {member && row.reasons[0] && (
                <Text small muted className="basis-full">
                  {reasonText(row.reasons[0].reason, stage)}
                </Text>
              )}
            </li>
          ))}
        </ul>
        {member && (
          <Text small muted className="block">
            Each row counts every member server using that provider. Servers
            without a provider share one row. Internal retries happen inside one
            listing, not on a later reconnect.
          </Text>
        )}
        {(ranked.truncated || unlisted > 0) && (
          <Text small muted className="block">
            {unlisted > 0
              ? `${unlisted.toLocaleString()} more ${unlisted === 1 ? "attempt is" : "attempts are"} recorded with providers not listed here.`
              : "More providers are recorded than are listed here."}
          </Text>
        )}
      </>
    );
  }
  return (
    <div className="space-y-1">
      <h4 className="text-sm font-medium">
        {copy.title}
        {attempts > 0 && (
          <span className="text-muted-foreground font-normal">
            {" "}
            · {outcomeSummary(stage, summary?.outcomes ?? [])}
            {retryNote}
          </span>
        )}
      </h4>
      {content}
    </div>
  );
}

// The most providers one caller's stage lists; the server allows no more.
const CALLER_PROVIDER_LIMIT = 25;

function outcomeSummary(
  stage: string,
  outcomes: { outcome: string; count: number }[],
): string {
  return outcomes
    .map(
      (count) =>
        `${outcomeLabel(stage, count.outcome)}: ${count.count.toLocaleString()}`,
    )
    .join(", ");
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
      <Text small muted>
        {message}
      </Text>
      <Button variant="tertiary" size="sm" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}

function KeyedDetail({
  stage,
  intervalSeconds,
  hotspot,
  from,
  to,
  first,
  recent,
  related,
}: {
  stage: GatewayConnectionStage;
  intervalSeconds: number;
  hotspot: GatewayConnectionHotspot;
  from: Date;
  to: Date;
  first: { data?: Date | null; isError: boolean };
  recent: {
    data?: { attempts: Attempt[]; more: boolean };
    isError: boolean;
    refetch: () => unknown;
  };
  related: ReactNode;
}): JSX.Element {
  const chart = stageChartData(stage, intervalSeconds, from, to);
  const failed = countOf(stage.outcomes, "failed");
  const blocked =
    countOf(stage.outcomes, "credential_blocked") +
    countOf(stage.outcomes, "consent_blocked");
  const partial = countOf(stage.outcomes, "partial");
  const total = stage.attempts.toLocaleString();
  const rate =
    stage.attempts > 0
      ? `${failureRate({ attempts: stage.attempts, failed })} of ${total}`
      : `of ${total}`;
  const inbound =
    stage.stage === "inbound_authorize" || stage.stage === "inbound_token";

  return (
    <div className="space-y-6 px-4">
      <StatTileGroup compact>
        {inbound &&
          stageTiles(stage).map((tile) => (
            <StatTile compact key={tile.title} {...tile} format="number" />
          ))}
        {!inbound && (
          <>
            <StatTile
              compact
              title="Attempts"
              value={stage.attempts}
              format="number"
              tone="information"
            />
            <StatTile
              compact
              title="Failed"
              value={failed}
              format="number"
              tone={failed > 0 ? "destructive" : "neutral"}
              subtext={rate}
            />
            {stage.stage === "discovery_listing" && (
              <StatTile
                compact
                title="Partial"
                value={partial}
                format="number"
                tone={partial > 0 ? "warning" : "neutral"}
                subtext={`of ${total}`}
              />
            )}
            {stage.stage === "discovery_member" && (
              <StatTile
                compact
                title="Needs sign-in or consent"
                value={blocked}
                format="number"
                tone={blocked > 0 ? "warning" : "neutral"}
                subtext={`of ${total}`}
              />
            )}
          </>
        )}
      </StatTileGroup>
      <FirstAttempt first={first} />

      <section className="space-y-3 border-y py-4">
        <h3 className="text-eyebrow">Attempts over time</h3>
        <StackedTimeBarChart
          {...chart}
          labels={overviewLabels(chart.labels)}
          compact
          height={100}
        />
        <ChartLegend datasets={chart.datasets} />
      </section>

      {hotspot.reasons.length > 0 && (
        <section className="space-y-2">
          <h3 className="text-eyebrow">Why it fails</h3>
          <ul className="space-y-2">
            {hotspot.reasons.map((reason) => (
              <li
                key={reason.reason}
                className="grid grid-cols-12 items-center gap-3 py-1"
              >
                <Text small className="col-span-7">
                  {reasonText(reason.reason, stage.stage)}
                </Text>
                <span aria-hidden="true" className="bg-muted col-span-4 h-1.5">
                  <span
                    className="bg-destructive block h-full"
                    style={{
                      width: `${(reason.count / Math.max(1, ...hotspot.reasons.map((item) => item.count))) * 100}%`,
                    }}
                  />
                </span>
                <Text
                  small
                  className="text-destructive text-right tabular-nums"
                >
                  {reason.count.toLocaleString()}
                </Text>
              </li>
            ))}
          </ul>
          <Collapsible>
            <CollapsibleTrigger className="group text-muted-foreground flex items-center gap-1 text-sm underline underline-offset-4">
              <ChevronDown
                aria-hidden="true"
                className="size-3.5 transition-transform group-data-[state=open]:rotate-180"
              />
              Technical details
            </CollapsibleTrigger>
            <CollapsibleContent className="pt-2">
              <Text muted small className="block font-mono">
                {hotspot.reasons.map((reason) => reason.reason).join(", ")}
              </Text>
            </CollapsibleContent>
          </Collapsible>
        </section>
      )}

      <RecentAttempts stage={stage.stage} recent={recent} />

      {related}
    </div>
  );
}

function FirstAttempt({
  first,
}: {
  first: { data?: Date | null; isError: boolean };
}): JSX.Element | null {
  if (first.isError || first.data === undefined) return null;
  return (
    <Text small muted className="block">
      {first.data
        ? `First attempt in this window: ${formatTime(first.data)}.`
        : "No attempts in this window."}
    </Text>
  );
}

function RecentAttempts({
  stage,
  recent,
}: {
  stage: string;
  recent: {
    data?: { attempts: Attempt[]; more: boolean };
    isError: boolean;
    refetch: () => unknown;
  };
}): JSX.Element {
  const title = recent.data
    ? `Latest ${recent.data.attempts.length} attempts in this window`
    : "Latest attempts in this window";
  let content: ReactNode;
  if (recent.isError) {
    content = (
      <div className="flex items-center justify-between gap-3">
        <Text small className="text-destructive">
          Couldn&apos;t load recent attempts.
        </Text>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => void recent.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  } else if (!recent.data) {
    content = <Skeleton className="h-24 w-full" />;
  } else if (recent.data.attempts.length === 0) {
    content = (
      <Text small muted>
        No attempts were found in this window.
      </Text>
    );
  } else {
    content = (
      <>
        <ul className="divide-y">
          {recent.data.attempts.map((attempt) => (
            <li key={attempt.id} className="flex gap-3 py-2">
              <Text small muted className="w-28 shrink-0">
                {formatTime(attempt.at)}
              </Text>
              <div className="min-w-0">
                <Text small className="block">
                  {outcomeLabel(stage, attempt.outcome)}
                  {grantLabel(attempt.grantType) &&
                    ` · ${grantLabel(attempt.grantType)}`}
                </Text>
                {attempt.reason && (
                  <Text small muted className="block">
                    {reasonText(attempt.reason, stage)}
                  </Text>
                )}
              </div>
            </li>
          ))}
        </ul>
        {recent.data.more && (
          <Text small muted>
            Older attempts in this window are not shown.
          </Text>
        )}
      </>
    );
  }
  return (
    <section aria-label={title} className="space-y-3 border-t pt-4">
      <h3 className="text-eyebrow">{title}</h3>
      {content}
    </section>
  );
}

function MatchingConnections({
  matching,
  onShow,
}: {
  matching: ReturnType<typeof matchingConnectionFilters>;
  onShow: () => void;
}): JSX.Element {
  if (matching.filters.length === 0) {
    return (
      <Text small muted>
        No matching connections can be shown for this caller.
      </Text>
    );
  }
  const { unmatched } = matching;
  const matched = ["this caller", "provider", "client"].filter(
    (part) => part === "this caller" || !unmatched.includes(part),
  );
  return (
    <div className="space-y-2">
      {unmatched.length > 0 && (
        <Text small muted className="block">
          Narrows by {matched.join(" and ")} only. The {unmatched.join(" and ")}{" "}
          on these attempts {unmatched.length > 1 ? "have" : "has"} no matching
          connection filter, so other connections may be listed.
        </Text>
      )}
      <Button onClick={onShow}>Show matching connections</Button>
    </div>
  );
}
