import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
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
import type { GatewayConnectionHotspot } from "@gram/client/models/components/gatewayconnectionhotspot.js";
import type { GatewayConnectionKey } from "@gram/client/models/components/gatewayconnectionkey.js";
import type { GatewayConnectionStage } from "@gram/client/models/components/gatewayconnectionstage.js";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildGatewayConnectionHealthQuery } from "@gram/client/react-query/gatewayConnectionHealth.core.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import {
  STAGE_OPTIONS,
  attemptFilters,
  canMatchSignIn,
  beforeWindowEnd,
  countOf,
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
          (canMatchSignIn(key) ? (
            <RelatedSignIn
              gatewayId={gatewayId}
              listingKey={key}
              from={from}
              to={to}
              windowPhrase={windowPhrase}
              ready={rawReady}
            />
          ) : (
            <section className="space-y-2">
              <h3 className="text-eyebrow">Sign-in tokens</h3>
              <Text small muted>
                Sign-in token responses can be matched only to a signed-in
                person using a registered client, so none are shown here.
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
        <SheetHeader>
          <SheetTitle>{hotspot.subject.label}</SheetTitle>
          <SheetDescription>
            {[
              stageLabel,
              hotspot.client.label,
              hotspot.provider.label,
              windowPhrase,
            ].join(" · ")}
          </SheetDescription>
        </SheetHeader>
        {body}
        {keyed && (
          <SheetFooter className="mt-auto">
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

// RelatedSignIn shows, beside a listing hotspot, the sign-in tokens the gateway
// wrote for the same person and client in the same window. It is an
// observation next to the listings, never their cause, and the person and
// client may be several app instances.
function RelatedSignIn({
  gatewayId,
  listingKey,
  from,
  to,
  windowPhrase,
  ready,
}: {
  gatewayId: string;
  listingKey: GatewayConnectionKey;
  from: Date;
  to: Date;
  windowPhrase: string;
  ready: boolean;
}): JSX.Element {
  const organization = useOrganization();
  const project = useProject();
  const client = useGramContext();
  const tokenKey: GatewayConnectionKey = {
    ...listingKey,
    stage: "inbound_token",
  };
  const payload = { metaMcpServerId: gatewayId, from, to, key: tokenKey };
  const built = buildGatewayConnectionHealthQuery(client, {
    gramProject: project.slug,
    getGatewayConnectionHealthPayload: payload,
  });
  const tokens = useQuery({
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
  // Counted only from a settled read made since this opening.
  const issued =
    !tokens.isError &&
    tokens.isFetchedAfterMount &&
    !tokens.isFetching &&
    matchesWindow(tokens.data, from, to)
      ? countOf(
          tokens.data?.stages.find((s) => s.stage === "inbound_token")
            ?.outcomes ?? [],
          "code_issued",
        )
      : undefined;
  const filters = [
    ...attemptFilters(gatewayId, tokenKey),
    {
      path: "gram.connection_attempt.outcome",
      operator: "eq" as const,
      values: ["code_issued"],
    },
    {
      path: "gram.connection_attempt.grant_type",
      operator: "eq" as const,
      values: ["authorization_code"],
    },
  ];
  // The latest one only, with the same exclusive end as the attempts list.
  const latest = useQuery({
    queryKey: [
      "gateway-connection-latest-sign-in",
      organization.id,
      project.id,
      gatewayId,
      filters,
      from.getTime(),
      to.getTime(),
    ],
    queryFn: async () => {
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
              limit: 1,
              cursor,
            },
          }),
        );
        const kept = beforeWindowEnd(result.logs, to)[0];
        if (kept) return toAttempt(kept).at;
        cursor = result.nextCursor;
        if (!cursor) break;
      }
      return null;
    },
    enabled: ready && issued !== undefined && issued > 0,
    staleTime: 0,
    refetchOnMount: "always",
    throwOnError: false,
    retry: false,
  });
  // A failed refetch keeps the earlier time, which is then not this answer.
  const latestStarted =
    !latest.isError && latest.isFetchedAfterMount && !latest.isFetching
      ? latest.data
      : undefined;

  let content: ReactNode;
  if (tokens.isError) {
    content = (
      <div className="flex items-center justify-between gap-3">
        <Text small muted>
          Couldn&apos;t load sign-in token responses for this person and client.
        </Text>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => void tokens.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  } else if (issued === undefined) {
    content = <Skeleton className="h-10 w-full" />;
  } else if (issued === 0) {
    content = (
      <Text small muted>
        No sign-in token response is recorded for this person and client{" "}
        {windowPhrase}.
      </Text>
    );
  } else {
    content = (
      <>
        <Text small className="block">
          {issued === 1
            ? `The gateway wrote 1 sign-in token response for this person and client ${windowPhrase}.`
            : `The gateway wrote ${issued.toLocaleString()} sign-in token responses for this person and client ${windowPhrase}.`}
          {latestStarted &&
            ` The latest sign-in code request started ${formatTime(latestStarted)}.`}
        </Text>
        {latest.isError && (
          <div className="flex items-center justify-between gap-3">
            <Text small muted>
              The latest request time couldn&apos;t be loaded.
            </Text>
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => void latest.refetch()}
            >
              Retry
            </Button>
          </div>
        )}
        <Text small muted className="block">
          Whether the app received or used a token is not observed. This person
          and client may be several app instances, and a token response is not
          shown as causing any listing.
        </Text>
      </>
    );
  }
  return (
    <section className="space-y-2">
      <h3 className="text-eyebrow">Sign-in tokens</h3>
      {content}
    </section>
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
  const total = stage.attempts.toLocaleString();
  const rate =
    stage.attempts > 0
      ? `${((failed / stage.attempts) * 100).toFixed(0)}% of ${total}`
      : `of ${total}`;
  const inbound =
    stage.stage === "inbound_authorize" || stage.stage === "inbound_token";

  return (
    <div className="space-y-6 px-4">
      <StatTileGroup className="overflow-x-auto [&>*]:min-w-[11.5rem]">
        {inbound &&
          stageTiles(stage).map((tile) => (
            <StatTile key={tile.title} {...tile} format="number" />
          ))}
        {!inbound && (
          <>
            <StatTile
              title="Attempts"
              value={stage.attempts}
              format="number"
              tone="information"
            />
            <StatTile
              title="Failed"
              value={failed}
              format="number"
              tone={failed > 0 ? "destructive" : "neutral"}
              subtext={rate}
            />
            {stage.stage === "discovery_member" && (
              <StatTile
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

      <section className="space-y-2">
        <h3 className="text-eyebrow">Over time</h3>
        <StackedTimeBarChart {...chart} height={180} />
      </section>

      {hotspot.reasons.length > 0 && (
        <section className="space-y-2">
          <h3 className="text-eyebrow">Why it fails</h3>
          <ul className="space-y-2">
            {hotspot.reasons.map((reason) => (
              <li key={reason.reason} className="flex gap-3">
                <Text small className="w-10 shrink-0 tabular-nums">
                  {reason.count.toLocaleString()}
                </Text>
                <Text small>{reasonText(reason.reason)}</Text>
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

      {related}

      <RecentAttempts stage={stage.stage} recent={recent} />
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
                    {reasonText(attempt.reason)}
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
    <section className="space-y-2">
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
