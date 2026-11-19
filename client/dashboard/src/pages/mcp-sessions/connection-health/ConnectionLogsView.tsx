import { useFilterState, type OptionsById } from "@/components/filters";
import {
  chipLabel,
  customDateRangeBounds,
  isDimensionAtDefault,
  type FilterValue,
} from "@/components/filters/filter-schema";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import type { Action } from "@/components/ui/MoreActions";
import { useRoutes } from "@/routes";
import { ArrowRight, RefreshCw, X } from "lucide-react";
import { RequireScope } from "@/components/require-scope";
import { useOrganization } from "@/contexts/Auth";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { toast } from "sonner";
import {
  CONNECTION_HEALTH_QUERY_ROOT,
  connectionHealthNow,
  useAttemptBelongs,
  useAttempt,
  useAttemptCounts,
  useConnectionAttempts,
} from "./api";
import { useProjectProviders } from "../providers";
import { AttemptDetailSheet } from "./AttemptDetailSheet";
import {
  AttemptsFooter,
  AttemptsSkeleton,
  AttemptsTable,
} from "./AttemptsTable";
import { LogFilterSheet } from "./LogFilterSheet";
import {
  ATTEMPT_PARAM,
  LOG_FILTERS,
  LOG_FILTERS_WITHOUT_SERVER,
  DEFAULT_LOG_RANGE,
  LOG_PARAMS,
  resolveWindow,
  TIME_RANGE_PRESETS,
  UNLISTED_APP,
  RETENTION_MS,
  SEARCH_PARAM,
  startsBeforeRetention,
  toAttemptFilters,
  type LogFilterValues,
} from "./filters";
import {
  formatDayMonth,
  formatSince,
  OUTCOME_LABELS,
  plural,
  STEP_LABELS,
} from "./format";
import {
  ATTEMPT_OUTCOMES,
  FILTER_STEPS,
  type AttemptFacets,
  type AttemptFilters,
  type AttemptRow,
  type FacetValue,
} from "./types";

const EMPTY_FILTERS: AttemptFilters = {};

const REFERENCE_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const OUTCOME_SENTENCE: Partial<Record<string, string>> = {
  connected: "connected",
  failed: "failed",
  blocked: "was blocked",
  declined: "was declined",
  didnt_finish: "was left unfinished",
};

function facetOptions(values: FacetValue[] | undefined, unnamed?: string) {
  return (values ?? []).map((v) => ({
    value: v.value,
    label: v.label ?? unnamed ?? v.value,
  }));
}

function optionsFrom(facets: AttemptFacets | undefined): OptionsById {
  return {
    outcome: ATTEMPT_OUTCOMES.map((o) => ({
      value: o,
      label: OUTCOME_LABELS[o],
    })),
    step: FILTER_STEPS.map((s) => ({ value: s, label: STEP_LABELS[s] })),
    server: facetOptions(facets?.servers, "Removed server"),
    // Attempts with no known person are found by network instead.
    person: facetOptions(facets?.people.filter((v) => v.value !== "")),
    service: facetOptions(facets?.services),
    app: facetOptions(
      facets?.apps.map((v) =>
        v.value === ""
          ? { ...v, value: UNLISTED_APP, label: v.label ?? "Unlisted app" }
          : v,
      ),
    ),
    appVersion: facetOptions(facets?.appVersions),
    message: facetOptions(facets?.reasons),
    network: facetOptions(facets?.networks),
  };
}

/** Matches free text to people, else servers, else gateway members (the API filters by id). */
type SearchResult = AttemptFilters | "no-match" | "too-broad" | null;

/** The API takes at most this many values per filter. */
const MAX_FILTER_VALUES = 50;

function searchFilters(
  text: string,
  facets: AttemptFacets | undefined,
): SearchResult {
  const q = text.trim().toLowerCase();
  if (!q) return null;
  const hits = (values: FacetValue[] | undefined) =>
    (values ?? [])
      .filter((v) => (v.label ?? v.value).toLowerCase().includes(q))
      .map((v) => v.value);
  const cap = (key: "people" | "serverIds" | "memberIds", values: string[]) =>
    values.length > MAX_FILTER_VALUES ? "too-broad" : { [key]: values };
  const people = hits(facets?.people).filter(Boolean);
  if (people.length > 0) return cap("people", people);
  const serverIds = hits(facets?.servers);
  if (serverIds.length > 0) return cap("serverIds", serverIds);
  const memberIds = hits(facets?.members);
  if (memberIds.length > 0) return cap("memberIds", memberIds);
  return "no-match";
}

/** Search narrows a filter it shares a field with, never widens it. */
function narrow(base: AttemptFilters, text: SearchResult): SearchResult {
  if (!text || typeof text === "string") return text;
  const next: AttemptFilters = {};
  for (const key of ["people", "serverIds", "memberIds"] as const) {
    const hits = text[key];
    if (!hits) continue;
    const set = base[key];
    const kept = set ? hits.filter((v) => set.includes(v)) : hits;
    if (kept.length === 0) return "no-match";
    next[key] = kept;
  }
  return next;
}

/** The current filters, with the server set, for the provider's own view. */
function withServer(params: URLSearchParams, serverId: string): string {
  const next = new URLSearchParams();
  for (const key of LOG_PARAMS) {
    const value = params.get(key);
    if (value !== null) next.set(key, value);
  }
  next.set("server", serverId);
  return next.toString();
}

function rangePhrase(values: LogFilterValues, options: OptionsById): string {
  const dim = LOG_FILTERS[0];
  const label = chipLabel(dim, values.date, options.date);
  return values.date.customRange
    ? "this time range"
    : `the ${label.toLowerCase()}`;
}

/** Connection Logs: what happened, attempt by attempt. */
export function ConnectionLogsView({
  providerId,
  lockedServer,
}: {
  providerId: string;
  /** Locks the view to one server (a server's own tab). */
  lockedServer?: { id: string; name: string };
}): JSX.Element {
  const routes = useRoutes();
  const { providers } = useProjectProviders();
  const filters = useFilterState(LOG_FILTERS);
  const [searchParams, setSearchParams] = useSearchParams();
  const search = searchParams.get(SEARCH_PARAM) ?? "";
  const openReference = searchParams.get(ATTEMPT_PARAM);
  // Presets resolve against a clock that moves only on Refresh.
  const [now, setNow] = useState(connectionHealthNow);
  const window = useMemo(
    () => resolveWindow(filters.values.date, now),
    [filters.values.date, now],
  );
  const beforeRetention = startsBeforeRetention(window, now);
  // A preset these logs don't offer would label a range that is not queried.
  const { preset, customRange } = filters.values.date;
  const unsupportedPreset =
    !customRange &&
    preset !== null &&
    !(TIME_RANGE_PRESETS as readonly string[]).includes(preset);
  const { setValue } = filters;
  useEffect(() => {
    if (unsupportedPreset)
      setValue("date", {
        preset: DEFAULT_LOG_RANGE,
        customRange: null,
        customLabel: null,
      });
  }, [unsupportedPreset, setValue]);
  const scope = useMemo(
    () => ({ providerId, serverId: lockedServer?.id }),
    [providerId, lockedServer?.id],
  );

  const baseFilters = useMemo(() => {
    const parsed = toAttemptFilters(filters.values);
    return lockedServer ? { ...parsed, serverIds: undefined } : parsed;
  }, [filters.values, lockedServer]);
  const isReference = REFERENCE_PATTERN.test(search.trim());

  const counts = useAttemptCounts(scope, window, baseFilters, {
    enabled: !beforeRetention,
  });
  // Labels come from unfiltered counts so a chip keeps its name.
  const optionCounts = useAttemptCounts(scope, window, EMPTY_FILTERS, {
    enabled: !beforeRetention,
  });
  const facets = optionCounts.data?.facets ?? counts.data?.facets;
  // Search matches against the filtered counts, so a narrow filter finds rarer names.
  const searchFacets = counts.data?.facets;
  const textFilters: SearchResult | "pending" =
    isReference || !search.trim()
      ? null
      : searchFacets
        ? narrow(baseFilters, searchFilters(search, searchFacets))
        : "pending";
  const searchBlocked =
    textFilters === "no-match" ||
    textFilters === "too-broad" ||
    textFilters === "pending";
  const listFilters = useMemo<AttemptFilters>(
    () =>
      textFilters && typeof textFilters === "object"
        ? { ...baseFilters, ...textFilters }
        : baseFilters,
    [baseFilters, textFilters],
  );
  const searchedCounts = useAttemptCounts(scope, window, listFilters, {
    enabled:
      !beforeRetention && typeof textFilters === "object" && !!textFilters,
  });

  const list = useConnectionAttempts(scope, window, listFilters, {
    enabled: !beforeRetention && !isReference && !searchBlocked,
  });
  const reference = useAttempt(isReference ? search.trim() : null);
  // A reference is looked up project-wide; keep it only if it belongs here.
  const referenced = reference.data?.attempt;
  const belongs = useAttemptBelongs(referenced, providerId, lockedServer?.id);
  const referenceHere = belongs === "yes" ? referenced : undefined;
  const referenceFailed = reference.isError || belongs === "error";

  const optionsById = useMemo(() => optionsFrom(facets), [facets]);

  const setParam = useCallback(
    (key: string, value: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value) next.set(key, value);
          else next.delete(key);
          return next;
        },
        // Opening an attempt is a step Back undoes; closing one is not.
        { replace: key === SEARCH_PARAM || !value },
      );
    },
    [setSearchParams],
  );

  const resetAll = () =>
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const key of [...LOG_PARAMS, "af"]) next.delete(key);
        return next;
      },
      { replace: true },
    );

  const rowActions = useCallback(
    (row: AttemptRow): Action[] => {
      const actions: Action[] = [
        {
          icon: "panel-right-open",
          label: "View details",
          onClick: () => setParam(ATTEMPT_PARAM, row.id),
        },
        {
          icon: "copy",
          label: "Copy reference",
          onClick: () => {
            void navigator.clipboard
              .writeText(row.id)
              .then(() => toast.success("Reference copied"));
          },
        },
      ];
      if (row.person) {
        const urn = row.person.subjectUrn;
        actions.push({
          icon: "user",
          label: "Show only this person",
          separatorBefore: true,
          onClick: () => filters.setValues({ person: urn }),
        });
      }
      if (row.server && !lockedServer) {
        const id = row.server.id;
        actions.push({
          icon: "server",
          label: "Show only this server",
          separatorBefore: !row.person,
          onClick: () => filters.setValues({ server: id }),
        });
      }
      return actions;
    },
    [filters, lockedServer, setParam],
  );

  const viewerScope =
    (isReference ? reference.data : list.data?.pages[0])?.viewerScope ??
    counts.data?.viewerScope;
  const loggingEnabled =
    (isReference ? reference.data : list.data?.pages[0])?.loggingEnabled ??
    counts.data?.loggingEnabled ??
    true;
  const rows: AttemptRow[] = useMemo(() => {
    if (isReference) return referenceHere ? [referenceHere] : [];
    if (searchBlocked) return [];
    return list.data?.pages.flatMap((p) => p.attempts ?? []) ?? [];
  }, [isReference, referenceHere, list.data, searchBlocked]);

  const total = isReference
    ? rows.length
    : textFilters === "no-match"
      ? 0
      : searchBlocked
        ? undefined
        : textFilters
          ? searchedCounts.data?.totalAttempts
          : counts.data?.totalAttempts;

  const countText = !loggingEnabled
    ? null
    : isReference
      ? reference.isPending || (!!referenced && belongs === "pending")
        ? null
        : rows[0]
          ? rows[0].count > 1
            ? `1 match · stands for ${plural(rows[0].count, "repeat")}`
            : "1 match"
          : "No match"
      : total === undefined
        ? null
        : plural(total, "attempt");

  const schema = lockedServer ? LOG_FILTERS_WITHOUT_SERVER : LOG_FILTERS;
  const organization = useOrganization();
  const { fetchNextPage } = list;
  const loadMore = useCallback(async () => {
    await fetchNextPage();
  }, [fetchNextPage]);
  // A range picked on the calendar has no words of its own; its chip names the days.
  const chipValues = useMemo(() => {
    const { customRange, customLabel } = filters.values.date;
    if (!customRange || customLabel) return filters.values;
    const bounds = customDateRangeBounds(filters.values.date) ?? customRange;
    const end = new Date(bounds.to.getTime() - 1);
    const from = formatDayMonth(customRange.from.toISOString());
    const to = formatDayMonth(end.toISOString());
    return {
      ...filters.values,
      date: {
        ...filters.values.date,
        customLabel: from === to ? from : `${from} – ${to}`,
      },
    };
  }, [filters.values]);
  const filtersActive = schema.some(
    (dim) =>
      dim.kind !== "daterange" &&
      !isDimensionAtDefault(
        dim,
        filters.values[dim.id as keyof LogFilterValues] as FilterValue,
      ),
  );

  const refresh = () => {
    setNow(connectionHealthNow());
    // A reference or a picked range keeps its key, so it refetches instead.
    if (isReference) void reference.refetch();
    else if (filters.values.date.customRange) {
      void counts.refetch();
      void optionCounts.refetch();
      void list.refetch();
      if (typeof textFilters === "object" && textFilters)
        void searchedCounts.refetch();
    }
  };
  const isFetching = isReference
    ? reference.isFetching
    : list.isFetching || counts.isFetching;

  let body: JSX.Element;
  const countsFailed = textFilters === "pending" && counts.isError;
  const isError = isReference ? referenceFailed : list.isError || countsFailed;
  const isPending = isReference
    ? reference.isPending || (!!referenced && belongs === "pending")
    : list.isPending;
  if (!loggingEnabled) {
    body = (
      <InlineEmptyState
        icon="file-x"
        heading="Connection Logs needs logging"
        description={`Logging is off for ${organization.name || "this organization"}, so attempts are not kept. Turning it on starts the log from that moment.`}
        action={<EnableLoggingAction />}
      />
    );
  } else if (beforeRetention && !isReference) {
    const oldest = new Date(now.getTime() - RETENTION_MS).toISOString();
    body = (
      <InlineEmptyState
        icon="calendar"
        orientation="horizontal"
        heading="Connection Logs cover the last 30 days"
        description={`Logs are available from ${formatSince(oldest, now)}.`}
        action={
          <Button
            variant="secondary"
            onClick={() =>
              filters.setValue("date", {
                preset: "30d",
                customRange: null,
                customLabel: null,
              })
            }
          >
            Last 30 days
          </Button>
        }
      />
    );
  } else if (isError) {
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        orientation="horizontal"
        heading="Couldn't load Connection Logs"
        description="Connections are not affected."
        action={
          <Button
            variant="secondary"
            onClick={() =>
              void (isReference
                ? reference.refetch()
                : countsFailed
                  ? counts.refetch()
                  : list.refetch())
            }
          >
            <Button.LeftIcon>
              <RefreshCw className="size-4" />
            </Button.LeftIcon>
            <Button.Text>Try again</Button.Text>
          </Button>
        }
      />
    );
  } else if (
    (textFilters === "pending" || (isPending && !searchBlocked)) &&
    rows.length === 0
  ) {
    body = <AttemptsSkeleton label="Loading Connection Logs" />;
  } else if (textFilters === "too-broad") {
    body = (
      <InlineEmptyState
        icon="search"
        orientation="horizontal"
        heading="Too many matches"
        description={`More than ${MAX_FILTER_VALUES} people or servers match “${search}”. Type more of the name.`}
      />
    );
  } else if (rows.length === 0) {
    body = isReference ? (
      <InlineEmptyState
        icon="search"
        orientation="horizontal"
        heading="No attempt with this reference"
        description={
          lockedServer
            ? "Reference search covers the last 30 days of this server."
            : "Reference search covers the last 30 days of this provider."
        }
      />
    ) : filtersActive || search ? (
      <InlineEmptyState
        icon="search"
        orientation="horizontal"
        heading="No attempts match these filters"
        description={noMatchSentence(filters.values, optionsById, search)}
        action={
          <Button
            variant="secondary"
            onClick={() => {
              resetAll();
            }}
          >
            Reset filters
          </Button>
        }
      />
    ) : (
      <InlineEmptyState
        icon="scroll-text"
        orientation="horizontal"
        heading={`No attempts in ${rangePhrase(filters.values, optionsById)}`}
        description={`Nothing connected or failed in ${rangePhrase(filters.values, optionsById)}.`}
        action={
          filters.values.date.preset !== "30d" ? (
            <Button
              variant="secondary"
              onClick={() =>
                filters.setValue("date", {
                  preset: filters.values.date.preset === "1d" ? "7d" : "1d",
                  customRange: null,
                  customLabel: null,
                })
              }
            >
              {filters.values.date.preset === "1d"
                ? "Last 7 days"
                : "Last 24 hours"}
            </Button>
          ) : undefined
        }
      />
    );
  } else {
    const hasMore = !isReference && !!list.hasNextPage;
    body = (
      <AttemptsTable
        className={
          list.isPlaceholderData && !isReference ? "opacity-60" : undefined
        }
        rows={rows}
        now={now}
        onOpen={(row) => setParam(ATTEMPT_PARAM, row.id)}
        rowActions={rowActions}
        selectedId={openReference}
        hasMore={hasMore}
        onLoadMore={loadMore}
        footer={
          isReference ? undefined : (
            <AttemptsFooter total={total} hasMore={hasMore} />
          )
        }
      />
    );
  }

  return (
    <div className="space-y-4">
      <Page.Toolbar>
        <Page.Toolbar.Row>
          <Page.Toolbar.Search
            value={search}
            onChange={(value) => setParam(SEARCH_PARAM, value || null)}
            debounceMs={250}
            placeholder="Search reference, person or server"
            className="w-full max-w-md"
          />
          {filtersActive && loggingEnabled ? (
            <Page.Toolbar.Leading>
              <Button
                variant="tertiary"
                onClick={resetAll}
                className="text-muted-foreground h-10 gap-1"
              >
                <X className="size-3.5" />
                <span className="text-eyebrow">Reset filters</span>
              </Button>
            </Page.Toolbar.Leading>
          ) : null}
          {countText || lockedServer ? (
            <Page.Toolbar.Actions>
              <span className="text-muted-foreground flex items-center gap-2 text-sm whitespace-nowrap">
                {countText}
                {countText && lockedServer ? <span aria-hidden>·</span> : null}
                {lockedServer ? (
                  <Link
                    to={`${routes.mcpSessions.provider.connectionLogs.href(providerId)}?${withServer(searchParams, lockedServer.id)}`}
                    className="text-foreground inline-flex items-center gap-1 hover:underline"
                  >
                    Open in MCP Sessions with this server
                    <ArrowRight className="size-3.5" aria-hidden />
                  </Link>
                ) : null}
              </span>
            </Page.Toolbar.Actions>
          ) : null}
          <Page.Toolbar.Refresh onRefresh={refresh} isRefreshing={isFetching} />
        </Page.Toolbar.Row>
        {loggingEnabled ? (
          <Page.Toolbar.Row>
            <Page.Toolbar.Filters
              schema={LOG_FILTERS}
              locked={
                lockedServer
                  ? { server: `Server: ${lockedServer.name}` }
                  : undefined
              }
              showSheetCount={false}
              showClearAll={false}
              values={chipValues}
              optionsById={optionsById}
              onChange={(id, value) =>
                filters.setValue(id as keyof LogFilterValues, value as never)
              }
              onClear={(id) => filters.clearValue(id as keyof LogFilterValues)}
              onClearAll={filters.clearAll}
              renderSheet={(sheet) => (
                <LogFilterSheet
                  {...sheet}
                  schema={schema}
                  values={filters.values}
                  optionsById={optionsById}
                  facets={counts.data?.facets}
                  total={
                    beforeRetention ? undefined : counts.data?.totalAttempts
                  }
                  onChange={(id, value) =>
                    filters.setValue(
                      id as keyof LogFilterValues,
                      value as never,
                    )
                  }
                  onReset={resetAll}
                />
              )}
            />
          </Page.Toolbar.Row>
        ) : null}
      </Page.Toolbar>
      {isReference ? (
        <p className="text-muted-foreground -mt-2 text-xs">
          Reference search covers all 30 days.
        </p>
      ) : null}
      {viewerScope === "own" ? <OwnAttemptsNotice /> : null}

      {body}

      <AttemptDetailSheet
        reference={openReference}
        providerId={providerId}
        lockedServerId={lockedServer?.id}
        providerName={providers.find((p) => p.id === providerId)?.slug}
        onClose={() => setParam(ATTEMPT_PARAM, null)}
      />
    </div>
  );
}

function noMatchSentence(
  values: LogFilterValues,
  options: OptionsById,
  search: string,
): string {
  const range = rangePhrase(values, options);
  if (search) return `Nothing matches “${search}” in ${range}.`;
  const others =
    [
      values.person,
      values.service,
      values.app,
      values.appVersion,
      values.network,
    ].some(Boolean) ||
    values.step.length > 0 ||
    values.message.length > 0;
  if (others) return `No attempts match these filters in ${range}.`;
  const verb =
    values.outcome.length === 1 ? OUTCOME_SENTENCE[values.outcome[0]!] : null;
  const server = values.server
    ? options.server?.find((o) => "value" in o && o.value === values.server)
    : undefined;
  const on = server && "label" in server ? ` on ${server.label as string}` : "";
  return verb
    ? `Nothing ${verb}${on} in ${range}.`
    : `No attempt matches${on} in ${range}.`;
}

/** Turns logging on for an organization admin; others are told who can. */
function EnableLoggingAction(): JSX.Element {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  const queryClient = useQueryClient();
  const enable = useFeaturesSetMutation({
    onSuccess: () => {
      if (!isCurrentOrganization()) return;
      void queryClient.invalidateQueries({
        queryKey: [CONNECTION_HEALTH_QUERY_ROOT],
      });
    },
    onError: () => {
      if (isCurrentOrganization())
        toast.error("Couldn't turn on logging. Try again.");
    },
  });
  return (
    <div className="flex flex-col items-center gap-2">
      <RequireScope
        scope="org:admin"
        level="section"
        fallback={
          <p className="text-muted-foreground text-sm">
            An organization admin can turn it on.
          </p>
        }
      >
        <Button
          disabled={enable.isPending}
          onClick={() =>
            enable.mutate({
              request: {
                setProductFeatureRequestBody: {
                  organizationId: organization.id,
                  featureName: FeatureName.Logs,
                  enabled: true,
                },
              },
            })
          }
        >
          {enable.isPending ? "Enabling..." : "Enable logging"}
        </Button>
      </RequireScope>
    </div>
  );
}

/** Said once wherever the API reads only the viewer's own attempts. */
export function OwnAttemptsNotice(): JSX.Element {
  return (
    <p className="text-muted-foreground text-sm">
      You see only your own connection attempts. People with access to logs see
      everyone&apos;s.
    </p>
  );
}
