import { ConnectionsList } from "@/components/connections/ConnectionsList";
import {
  CONNECTION_GROUPING_LABELS,
  type ConnectionGrouping,
} from "@/components/connections/groupConnections";
import {
  LogsFacetRail,
  type FacetGroup,
} from "@/components/observe/LogsFacetRail";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";
import { useRBAC } from "@/hooks/useRBAC";
import { useReadableAgents } from "@/hooks/useReadableAgents";
import { useSessionAgents } from "@/hooks/useSessionAgents";
import {
  LogFilterBar,
  type FilterValueOption,
} from "@/pages/logs/LogFilterBar";
import { type ActiveLogFilter } from "@/pages/logs/log-filter-types";
import { parseFilters, serializeFilters } from "@/pages/logs/log-filter-url";
import type { UserSessionFilterFacet } from "@gram/client/models/components/usersessionfilterfacet.js";
import type { UserSessionFilterFacets } from "@gram/client/models/components/usersessionfilterfacets.js";
import type { Field as FacetFieldParam } from "@gram/client/models/operations/queryusersessionfacets.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildUserSessionFilterFacetsQuery } from "@gram/client/react-query/userSessionFilterFacets.core.js";
import { useUserSessionsInfinite } from "@gram/client/react-query/userSessions.js";
import {
  keepPreviousData,
  useIsFetching,
  useQueries,
} from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { GatewayClientAdmission } from "./GatewayClientAdmission";
import { GatewayConnectionHealth } from "./GatewayConnectionHealth";
import { GatewayConnectionTrend } from "./GatewayConnectionTrend";
import {
  HEALTH_WINDOWS,
  WINDOW_PHRASE,
  healthWindow,
  type HealthWindow,
} from "./connectionHealth";
import {
  CLIENT_REGISTRATION_LABELS,
  CLIENT_VALUE_LABELS,
  IDENTITY_TYPE_LABELS,
  PROVIDER_VALUE_LABELS,
  LAST_USED_OPTIONS,
  SESSION_FILTER_FIELDS,
  SESSION_FILTER_PARAM,
  SESSION_FILTER_SAMPLES,
  describeSessionFilter,
  encodeValues,
  filtersToQuery,
  formatSessionFilterValue,
  includedValues,
  lastUsedLabel,
  parseSessionFilterExpression,
  sessionFilterOperators,
  sessionFilterProblem,
  withinFilterLimits,
  sessionFilterLimitProblem,
  ownLabel,
  withIncludedValues,
  type SessionFacetField,
} from "./sessionFilters";

const MAX_SEARCH_LENGTH = 200;

const GROUPING_LABELS: Record<ConnectionGrouping, string> = {
  ...CONNECTION_GROUPING_LABELS,
  issuer: "Sign-in server",
  client: "Client",
};

const GROUPING_OPTIONS = (
  ["subject", "issuer", "provider", "client"] as const
).map((value) => ({ value, label: GROUPING_LABELS[value] }));

const RAIL_FIELDS: SessionFacetField[] = [
  "identity_type",
  "mcp_server",
  "client",
  "client_registration",
  "provider",
];

const FACET_KEYS: Record<
  SessionFacetField,
  keyof Omit<UserSessionFilterFacets, "unattributedCount">
> = {
  identity: "identity",
  identity_type: "identityType",
  mcp_server: "mcpServer",
  client: "client",
  client_registration: "clientRegistration",
  provider: "provider",
};

type Surface = "rail" | "builder";
type SearchKey = `${Surface}:${SessionFacetField}`;

// Fixed fields whose few values all arrive unsearched; typing filters them by
// their shown label or raw value without a request.
const LOCAL_FIELDS: SessionFacetField[] = [
  "identity_type",
  "client_registration",
];
const LOCAL_LABELS: Partial<Record<SessionFacetField, Record<string, string>>> =
  {
    identity_type: IDENTITY_TYPE_LABELS,
    client_registration: CLIENT_REGISTRATION_LABELS,
  };

const tooLong = (text: string) => Array.from(text).length > MAX_SEARCH_LENGTH;

function fieldLabel(field: SessionFacetField): string {
  return SESSION_FILTER_FIELDS.find((option) => option.key === field)!.label;
}

function writeFilters(next: URLSearchParams, filters: ActiveLogFilter[]) {
  const serialized = serializeFilters(filters);
  if (serialized) next.set(SESSION_FILTER_PARAM, serialized);
  else next.delete(SESSION_FILTER_PARAM);
}

export function GatewayDebugTab({
  gatewayId,
  issuerId,
}: {
  gatewayId: string;
  issuerId: string | null | undefined;
}): JSX.Element {
  const project = useProject();
  const { hasScope } = useRBAC();
  const canRevoke = hasScope("project:write", project.id);
  // One window for the trend and health sections, and the from/to snapshot
  // both read, retaken on refresh.
  const [span, setSpan] = useState<HealthWindow>("7d");
  const [now, setNow] = useState(() => new Date());
  const debugWindow = useMemo(() => healthWindow(span, now), [span, now]);
  // The SDK reads this tab makes for the gateway: their keys name this
  // project and this gateway. Health keys carry the project in the request's
  // parameters and the gateway in the body appended after them.
  const gatewayReads = useIsFetching({
    predicate: ({ queryKey }) => {
      const names = (field: string, value: string) =>
        queryKey.some(
          (part) =>
            typeof part === "object" &&
            part !== null &&
            (part as Record<string, unknown>)[field] === value,
        );
      return (
        queryKey[0] === "@gram/client" &&
        names("gramProject", project.slug) &&
        names("metaMcpServerId", gatewayId)
      );
    },
  });
  const [searchParams, setSearchParams] = useSearchParams();
  const search = searchParams.get("q") ?? "";
  const [searchInput, setSearchInput] = useState("");
  const [grouping, setGrouping] = useState<ConnectionGrouping>("subject");

  // A committed search or filter change is a history entry, so Back restores
  // the previous view. Only correcting a link's own URL replaces it, and a
  // change that leaves the URL as it is does not navigate at all.
  // setSearchParams builds from these same render-time params either way.
  const updateParams = useCallback(
    (update: (next: URLSearchParams) => void, replace = false) => {
      const next = new URLSearchParams(searchParams);
      update(next);
      if (next.toString() === searchParams.toString()) return;
      setSearchParams(next, { replace });
    },
    [searchParams, setSearchParams],
  );
  const setSearch = useCallback(
    (value: string) =>
      updateParams((next) => {
        if (value) next.set("q", value);
        else next.delete("q");
      }),
    [updateParams],
  );
  const linkedChips = useMemo(
    () => parseFilters(searchParams.get(SESSION_FILTER_PARAM)),
    [searchParams],
  );
  const chips = useMemo(
    () =>
      withinFilterLimits(
        linkedChips.filter(
          (chip) =>
            sessionFilterProblem(chip.path, chip.op, chip.value) === null,
        ),
      ),
    [linkedChips],
  );
  const setChips = useCallback(
    (filters: ActiveLogFilter[], replace = false) =>
      updateParams((next) => writeFilters(next, filters), replace),
    [updateParams],
  );
  // Filters from a link that cannot apply are named once and taken out of it.
  const [ignoredChips, setIgnoredChips] = useState<ActiveLogFilter[]>([]);
  useEffect(() => {
    if (chips.length === linkedChips.length) return;
    setIgnoredChips(linkedChips.filter((chip) => !chips.includes(chip)));
    // Replace, so Back skips the rejected link instead of re-correcting it.
    setChips(chips, true);
  }, [chips, linkedChips, setChips]);
  const [selectionProblem, setSelectionProblem] = useState<string | null>(null);
  const editChips = useCallback(
    (filters: ActiveLogFilter[]) => {
      setIgnoredChips([]);
      setSelectionProblem(null);
      setChips(filters);
    },
    [setChips],
  );
  // Showing a hotspot's connections replaces the filters and drops the text
  // search in one history entry, since a search for someone else would hide
  // every one of them. Editing filters by hand keeps the search.
  const showMatching = useCallback(
    (filters: ActiveLogFilter[]) => {
      setIgnoredChips([]);
      setSelectionProblem(null);
      updateParams((next) => {
        next.delete("q");
        writeFilters(next, filters);
      });
    },
    [updateParams],
  );

  const term = search.trim();
  const searchTooLong = tooLong(term);
  const request = {
    gramProject: project.slug,
    metaMcpServerId: gatewayId,
    search: searchTooLong ? undefined : term || undefined,
    ...filtersToQuery(chips),
  };

  const [fieldSearches, setFieldSearches] = useState<
    Partial<Record<SearchKey, string>>
  >({});
  const setFieldSearch = useCallback(
    (surface: Surface, field: SessionFacetField, text: string) =>
      setFieldSearches((prev) => ({ ...prev, [`${surface}:${field}`]: text })),
    [],
  );
  const debouncedSearches = useDebouncedValue(fieldSearches, 200);
  // One request per distinct field and text, whichever surfaces typed it.
  const searches = [
    ...new Set(
      (Object.keys(debouncedSearches) as SearchKey[]).flatMap((key) => {
        const field = key.split(":")[1] as SessionFacetField;
        const text = debouncedSearches[key]!.trim();
        return text === "" || tooLong(text) || LOCAL_FIELDS.includes(field)
          ? []
          : [`${field}\n${text}`];
      }),
    ),
  ];

  const client = useGramContext();
  const facetQueries = useQueries({
    queries: [undefined, ...searches].map((search) => {
      const [field, text] = search?.split("\n") ?? [];
      return {
        ...buildUserSessionFilterFacetsQuery(client, {
          ...request,
          field: field as FacetFieldParam | undefined,
          fieldSearch: text,
        }),
        enabled: !searchTooLong,
        placeholderData: search ? undefined : keepPreviousData,
        throwOnError: false,
        retry: false,
      };
    }),
  });
  const [baseFacets, ...searchedFacets] = facetQueries;
  const facets = baseFacets!.data;
  const facetsFailed = facetQueries.filter((query) => query.isError);

  // A searched field never falls back to unsearched values.
  const fieldState = (
    surface: Surface,
    field: SessionFacetField,
  ): { facet?: UserSessionFilterFacet; notice?: string } => {
    const key: SearchKey = `${surface}:${field}`;
    const text = (fieldSearches[key] ?? "").trim();
    if (text === "") {
      if (facets) return { facet: facets[FACET_KEYS[field]] };
      return {
        notice: baseFacets!.isError ? "Couldn't load values." : "Loading…",
      };
    }
    if (tooLong(text)) {
      return {
        notice: `Search must be ${MAX_SEARCH_LENGTH} characters or fewer.`,
      };
    }
    if (LOCAL_FIELDS.includes(field)) {
      const facet = facets?.[FACET_KEYS[field]];
      const needle = text.toLowerCase();
      return {
        facet: facet && {
          ...facet,
          values: facet.values.filter((v) =>
            [v.value, ownLabel(LOCAL_LABELS[field]!, v.value) ?? ""].some((t) =>
              t.toLowerCase().includes(needle),
            ),
          ),
        },
      };
    }
    const index =
      (debouncedSearches[key] ?? "").trim() === text
        ? searches.indexOf(`${field}\n${text}`)
        : -1;
    const query = index === -1 ? undefined : searchedFacets[index];
    if (query?.isError) return { notice: "Couldn't search values." };
    if (!query?.data) return { notice: "Searching…" };
    return { facet: query.data[FACET_KEYS[field]] };
  };

  const identityValues = [
    ...includedValues(chips, "identity"),
    ...(facets?.identity.values.map((v) => v.value) ?? []),
    ...(fieldState("builder", "identity").facet?.values.map((v) => v.value) ??
      []),
  ];
  const agents = useReadableAgents(
    identityValues.some((value) => value.startsWith("agent:")),
  );
  const agentNames = useMemo(
    () =>
      new Map(
        (agents.isError ? [] : (agents.data ?? [])).map((agent) => [
          `agent:${agent.id}`,
          agent.name,
        ]),
      ),
    [agents.data, agents.isError],
  );

  const labelFor = (field: string, value: string): string => {
    switch (field) {
      case "identity_type":
        return ownLabel(IDENTITY_TYPE_LABELS, value) ?? value;
      case "client_registration":
        return ownLabel(CLIENT_REGISTRATION_LABELS, value) ?? value;
      case "last_used":
        return lastUsedLabel(value);
    }
    if (field === "identity" && agentNames.has(value)) {
      return agentNames.get(value)!;
    }
    if (field === "client" && ownLabel(CLIENT_VALUE_LABELS, value)) {
      return ownLabel(CLIENT_VALUE_LABELS, value)!;
    }
    if (field === "provider" && ownLabel(PROVIDER_VALUE_LABELS, value)) {
      return ownLabel(PROVIDER_VALUE_LABELS, value)!;
    }
    const named = facetQueries
      .flatMap((query) =>
        field in FACET_KEYS && query.data
          ? query.data[FACET_KEYS[field as SessionFacetField]].values
          : [],
      )
      .find((v) => v.value === value && v.displayName);
    return named?.displayName ?? value;
  };

  const [buildingField, setBuildingField] = useState<
    SessionFacetField | "last_used" | null
  >(null);
  let valueOptions: FilterValueOption[] | undefined;
  let valueNotice: string | undefined;
  if (buildingField === "last_used") {
    valueOptions = LAST_USED_OPTIONS.map((value) => ({
      value,
      label: lastUsedLabel(value),
    }));
  } else if (buildingField) {
    const { facet, notice } = fieldState("builder", buildingField);
    valueNotice = notice;
    // A selected value kept only so the rail can remove it is not a match for
    // what was typed, so the builder does not offer it.
    valueOptions = facet?.values
      .filter((v) => v.matchesSearch)
      .map((v) => ({
        value: v.value,
        label: labelFor(buildingField, v.value),
        count: v.count,
      }));
  }

  const railGroups: FacetGroup[] = RAIL_FIELDS.map((field) => {
    const selected = includedValues(chips, field);
    const { facet, notice } = fieldState("rail", field);
    return {
      id: field,
      label: fieldLabel(field),
      totalCount: facet?.distinctCount,
      onSearch: (text: string) => setFieldSearch("rail", field, text),
      searchNotice: notice,
      values: facet
        ? facet.values.map((v) => ({
            value: v.value,
            label: labelFor(field, v.value),
            count: v.count,
            selected: selected.includes(v.value),
            matchesSearch: v.matchesSearch,
          }))
        : selected.map((value) => ({
            value,
            label: labelFor(field, value),
            selected: true,
            matchesSearch: false,
          })),
    };
  });

  const {
    data,
    isPending,
    isError,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    isFetching,
    isPlaceholderData,
    isFetchNextPageError,
    isRefetchError,
    refetch,
  } = useUserSessionsInfinite(request, undefined, {
    enabled: !searchTooLong,
    placeholderData: keepPreviousData,
    throwOnError: false,
  });
  const unresolved = useMemo(
    () => data?.pages.flatMap((page) => page.result.items) ?? [],
    [data],
  );
  const sessions = useSessionAgents(unresolved);
  const actionable = !searchTooLong && sessions.length > 0;
  // One refresh for the tab: a new snapshot for the trend and health
  // sections, and the connections when a search can be sent.
  const refresh = () => {
    setNow(new Date());
    if (searchTooLong) return;
    void refetch();
    facetQueries.forEach((query) => void query.refetch());
  };
  const unattributed = searchTooLong ? 0 : (facets?.unattributedCount ?? 0);

  let body: JSX.Element;
  if (searchTooLong) {
    body = (
      <p className="text-destructive text-sm">
        Search must be {MAX_SEARCH_LENGTH} characters or fewer.
      </p>
    );
  } else if (isPending || (isPlaceholderData && sessions.length === 0)) {
    body = (
      <div className="space-y-2">
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-20 w-full" />
        ))}
      </div>
    );
  } else if (isError && sessions.length === 0) {
    body = (
      <div className="flex items-center justify-between gap-3">
        <p className="text-destructive text-sm">
          Couldn&apos;t load connections.
        </p>
        <Button variant="tertiary" size="sm" onClick={() => void refetch()}>
          Retry
        </Button>
      </div>
    );
  } else if (sessions.length === 0) {
    body = (
      <p className="text-muted-foreground text-sm">
        {term
          ? "No connections match your search"
          : chips.length > 0
            ? "No connections match these filters"
            : "No connections have been recorded for this gateway yet"}
      </p>
    );
  } else {
    body = (
      <ConnectionsList
        sessions={sessions}
        grouping={grouping}
        groupingLabels={GROUPING_LABELS}
        canRevoke={canRevoke}
        onRevoked={refresh}
        killswitchContext={{ capabilityKey: "mcp_tool_calls" }}
        project={project}
      />
    );
  }

  return (
    <Stack gap={4}>
      <Page.Toolbar>
        <Page.Toolbar.Leading>
          <Text small muted>
            Connections and connection health {WINDOW_PHRASE[span]}
          </Text>
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
          onRefresh={refresh}
          isRefreshing={gatewayReads > 0}
        />
      </Page.Toolbar>
      <GatewayConnectionTrend
        gatewayId={gatewayId}
        span={span}
        from={debugWindow.from}
        to={debugWindow.to}
        phrase={WINDOW_PHRASE[span]}
      />
      <div className="grid items-start gap-4 min-[1728px]:grid-cols-4">
        <div className="min-w-0 min-[1728px]:col-span-3">
          <GatewayConnectionHealth
            gatewayId={gatewayId}
            span={span}
            from={debugWindow.from}
            to={debugWindow.to}
            onShowConnections={showMatching}
          />
        </div>
        <GatewayClientAdmission gatewayId={gatewayId} issuerId={issuerId} />
      </div>
      <Text small muted>
        Sessions issued for this gateway, identified by their recorded origin or
        consent selection. Unattributed sessions are not shown.
      </Text>
      <Page.Toolbar>
        <Page.Toolbar.Leading>
          <div className="min-w-0 flex-1">
            <LogFilterBar
              filters={chips}
              onChange={editChips}
              attributeKeys={[]}
              keyOptions={SESSION_FILTER_FIELDS.map((option) => ({
                ...option,
                count:
                  option.key === "last_used"
                    ? undefined
                    : facets?.[FACET_KEYS[option.key]].distinctCount,
              }))}
              operatorsFor={sessionFilterOperators}
              parseExpression={parseSessionFilterExpression}
              valueOptions={valueOptions}
              onValueInputChange={(key, text) => {
                setBuildingField(key as SessionFacetField | "last_used");
                if (key !== "last_used") {
                  setFieldSearch("builder", key as SessionFacetField, text);
                }
              }}
              formatValue={(path, value, op) =>
                formatSessionFilterValue(path, op, value, labelFor)
              }
              validateFilter={(path, op, value, next) =>
                sessionFilterProblem(path, op, value) ??
                sessionFilterLimitProblem(next)
              }
              valueNotice={valueNotice}
              encodeInValue={(value) => encodeValues([value])}
              exactValue={(path) => path === "identity"}
              samples={SESSION_FILTER_SAMPLES}
              placeholder="Search connections or pick a field"
              searchInput={searchInput}
              onSearchInputChange={setSearchInput}
              onSearchSubmit={(value) => {
                setSearch(value);
                setSearchInput("");
              }}
              appliedSearch={search || undefined}
            />
          </div>
        </Page.Toolbar.Leading>
      </Page.Toolbar>
      {ignoredChips.length > 0 && (
        <p className="text-muted-foreground text-sm break-all">
          Removed filters from this link that can&apos;t apply:{" "}
          {ignoredChips
            .map((chip) => describeSessionFilter(chip, labelFor))
            .join("; ")}
        </p>
      )}
      {selectionProblem && (
        <p className="text-muted-foreground text-sm">{selectionProblem}</p>
      )}
      {!searchTooLong && facetsFailed.length > 0 && (
        <div className="flex items-center justify-between gap-3">
          <p className="text-destructive text-sm">
            Couldn&apos;t load filter values.
          </p>
          <Button
            variant="tertiary"
            size="sm"
            onClick={() =>
              facetsFailed.forEach((query) => void query.refetch())
            }
          >
            Retry
          </Button>
        </div>
      )}
      {unattributed > 0 && (
        <Text small muted>
          {unattributed === 1
            ? "1 session on this gateway's sign-in server has no recorded origin."
            : `${unattributed.toLocaleString()} sessions on this gateway's sign-in server have no recorded origin.`}
        </Text>
      )}
      <div className="flex gap-6">
        <div className="hidden w-60 shrink-0 lg:block">
          <LogsFacetRail
            groups={railGroups}
            onToggle={(groupId, value, nextSelected) => {
              const field = groupId as SessionFacetField;
              const current = includedValues(chips, field);
              const next = withIncludedValues(
                chips,
                field,
                nextSelected
                  ? [...current, value]
                  : current.filter((v) => v !== value),
              );
              const problem = sessionFilterLimitProblem(next);
              if (problem) setSelectionProblem(problem);
              else editChips(next);
            }}
            onClearGroup={(groupId) =>
              editChips(
                withIncludedValues(chips, groupId as SessionFacetField, []),
              )
            }
          />
        </div>
        <div className="min-w-0 flex-1 space-y-4">
          <div
            className="flex overflow-x-auto"
            // Keyboard focus scrolls the narrow, scrollable row to the option.
            onFocusCapture={(event) =>
              event.target.scrollIntoView({
                block: "nearest",
                inline: "nearest",
              })
            }
          >
            <SegmentedControl
              className="ml-auto"
              value={grouping}
              onChange={setGrouping}
              options={GROUPING_OPTIONS}
              ariaLabel="Group connections by"
            />
          </div>
          {actionable && (isRefetchError || isFetchNextPageError) && (
            <div className="flex items-center justify-between gap-3">
              <p className="text-destructive text-sm">
                {isFetchNextPageError
                  ? "Couldn’t load more connections."
                  : "Couldn’t refresh connections."}
              </p>
              <Button
                variant="tertiary"
                size="sm"
                onClick={() =>
                  void (isFetchNextPageError ? fetchNextPage() : refetch())
                }
              >
                Retry
              </Button>
            </div>
          )}
          {body}
          {actionable && hasNextPage && !isPlaceholderData && (
            <div className="flex justify-center">
              <Button
                variant="tertiary"
                size="sm"
                disabled={isFetching}
                onClick={() => void fetchNextPage()}
              >
                {isFetchingNextPage ? "Loading…" : "Load more"}
              </Button>
            </div>
          )}
        </div>
      </div>
    </Stack>
  );
}
