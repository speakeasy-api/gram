import { useFilterState, type OptionsById } from "@/components/filters";
import {
  chipLabel,
  isDimensionAtDefault,
  type FilterValue,
} from "@/components/filters/filter-schema";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import type { Action } from "@/components/ui/MoreActions";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useOrgRoutes, useRoutes } from "@/routes";
import { FilterChip } from "@/components/filters/FilterChip";
import { ArrowRight, RefreshCw } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { toast } from "sonner";
import {
  connectionHealthNow,
  useAttempt,
  useAttemptCounts,
  useConnectionAttempts,
} from "./api";
import { useProjectProviders } from "../providers";
import { AttemptDetailSheet } from "./AttemptDetailSheet";
import { AttemptsFooter, AttemptsTable } from "./AttemptsTable";
import {
  ATTEMPT_PARAM,
  LOG_FILTERS,
  LOG_FILTERS_WITHOUT_SERVER,
  resolveWindow,
  RETENTION_MS,
  SEARCH_PARAM,
  startsBeforeRetention,
  toAttemptFilters,
  type LogFilterValues,
} from "./filters";
import {
  formatCount,
  formatSince,
  OUTCOME_LABELS,
  plural,
  STEP_LABELS,
} from "./format";
import {
  FILTER_OUTCOMES,
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

function facetOptions(values: FacetValue[] | undefined) {
  return (values ?? []).map((v) => ({
    value: v.value,
    label: v.label ?? (v.value || "Unlisted app"),
  }));
}

function optionsFrom(facets: AttemptFacets | undefined): OptionsById {
  return {
    outcome: FILTER_OUTCOMES.map((o) => ({
      value: o,
      label: OUTCOME_LABELS[o],
    })),
    step: FILTER_STEPS.map((s) => ({ value: s, label: STEP_LABELS[s] })),
    server: facetOptions(facets?.servers),
    person: facetOptions(facets?.people),
    service: facetOptions(facets?.services),
    app: facetOptions(facets?.apps),
    appVersion: facetOptions(facets?.appVersions),
    message: facetOptions(facets?.reasons),
    network: facetOptions(facets?.networks),
  };
}

/** Matches free text to people, else servers, else gateway members (the API filters by id). */
function searchFilters(
  text: string,
  facets: AttemptFacets | undefined,
): AttemptFilters | "no-match" | null {
  const q = text.trim().toLowerCase();
  if (!q) return null;
  const hits = (values: FacetValue[] | undefined) =>
    (values ?? [])
      .filter((v) => (v.label ?? v.value).toLowerCase().includes(q))
      .map((v) => v.value);
  const people = hits(facets?.people);
  if (people.length > 0) return { people };
  const serverIds = hits(facets?.servers);
  if (serverIds.length > 0) return { serverIds };
  const memberIds = hits(facets?.members);
  if (memberIds.length > 0) return { memberIds };
  return "no-match";
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
  const orgRoutes = useOrgRoutes();
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
  const textFilters = isReference ? null : searchFilters(search, facets);
  const listFilters = useMemo<AttemptFilters>(
    () =>
      textFilters && textFilters !== "no-match"
        ? { ...baseFilters, ...textFilters }
        : baseFilters,
    [baseFilters, textFilters],
  );
  const searchedCounts = useAttemptCounts(scope, window, listFilters, {
    enabled: !beforeRetention && !!textFilters && textFilters !== "no-match",
  });

  const list = useConnectionAttempts(scope, window, listFilters, {
    enabled: !beforeRetention && !isReference && textFilters !== "no-match",
  });
  const reference = useAttempt(isReference ? search.trim() : null);

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
        { replace: key === SEARCH_PARAM },
      );
    },
    [setSearchParams],
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
    if (isReference) {
      const attempt = reference.data?.attempt;
      return attempt ? [attempt] : [];
    }
    if (textFilters === "no-match") return [];
    return list.data?.pages.flatMap((p) => p.attempts ?? []) ?? [];
  }, [isReference, reference.data, list.data, textFilters]);

  const total = isReference
    ? rows.length
    : textFilters === "no-match"
      ? 0
      : textFilters
        ? searchedCounts.data?.totalAttempts
        : counts.data?.totalAttempts;

  const countText = !loggingEnabled
    ? null
    : isReference
      ? reference.isPending
        ? null
        : rows[0]
          ? rows[0].count > 1
            ? `1 match · stands for ${formatCount(rows[0].count)} repeats`
            : "1 match"
          : "No match"
      : total === undefined
        ? null
        : `${(textFilters ? searchedCounts.data : counts.data)?.truncated ? "At least " : ""}${plural(total, "attempt")}`;

  const schema = lockedServer ? LOG_FILTERS_WITHOUT_SERVER : LOG_FILTERS;
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
  };
  const isFetching = isReference
    ? reference.isFetching
    : list.isFetching || counts.isFetching;

  let body: JSX.Element;
  const isError = isReference ? reference.isError : list.isError;
  const isPending = isReference ? reference.isPending : list.isPending;
  if (!loggingEnabled) {
    body = (
      <InlineEmptyState
        icon="file-x"
        heading="Connection Logs needs logging"
        description="Logging is off for this organization, so attempts are not kept. Turning it on starts the log from that moment."
        action={
          <Button asChild>
            <Link to={orgRoutes.logs.href()}>Enable logging</Link>
          </Button>
        }
      />
    );
  } else if (beforeRetention) {
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
              void (isReference ? reference.refetch() : list.refetch())
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
  } else if (isPending && rows.length === 0) {
    body = (
      <SkeletonTable
        label="Loading Connection Logs"
        columns={["120px", "1.2fr", "1fr", "140px", "2fr"]}
      />
    );
  } else if (rows.length === 0) {
    body = isReference ? (
      <InlineEmptyState
        icon="search"
        orientation="horizontal"
        heading="No attempt with this reference"
        description="Reference search covers the last 30 days of this project."
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
              filters.clearAll();
              setParam(SEARCH_PARAM, null);
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
        rows={rows}
        now={now}
        onOpen={(row) => setParam(ATTEMPT_PARAM, row.id)}
        rowActions={rowActions}
        hasMore={hasMore}
        onLoadMore={async () => {
          await list.fetchNextPage();
        }}
        footer={<AttemptsFooter total={total} hasMore={hasMore} />}
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
          {countText ? (
            <Page.Toolbar.Actions>
              <span className="text-muted-foreground text-sm whitespace-nowrap">
                {countText}
              </span>
            </Page.Toolbar.Actions>
          ) : null}
          {lockedServer ? (
            <Page.Toolbar.Actions>
              <Link
                to={`${routes.mcpSessions.provider.connectionLogs.href(providerId)}?server=${encodeURIComponent(lockedServer.id)}`}
                className="inline-flex items-center gap-1 text-sm whitespace-nowrap hover:underline"
              >
                Open in MCP Sessions with this server
                <ArrowRight className="size-3.5" aria-hidden />
              </Link>
            </Page.Toolbar.Actions>
          ) : null}
          <Page.Toolbar.Refresh onRefresh={refresh} isRefreshing={isFetching} />
        </Page.Toolbar.Row>
        {loggingEnabled ? (
          <Page.Toolbar.Row>
            <Page.Toolbar.Filters
              schema={schema}
              extraChips={
                lockedServer ? (
                  <FilterChip
                    label={`Server: ${lockedServer.name}`}
                    ariaLabel={`Locked to ${lockedServer.name}`}
                    active
                  />
                ) : undefined
              }
              values={filters.values}
              optionsById={optionsById}
              onChange={(id, value) =>
                filters.setValue(id as keyof LogFilterValues, value as never)
              }
              onClear={(id) => filters.clearValue(id as keyof LogFilterValues)}
              onClearAll={filters.clearAll}
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

/** Said once wherever the API reads only the viewer's own attempts. */
export function OwnAttemptsNotice(): JSX.Element {
  return (
    <p className="text-muted-foreground text-sm">
      You see only your own connection attempts. People with access to logs see
      everyone&apos;s.
    </p>
  );
}
