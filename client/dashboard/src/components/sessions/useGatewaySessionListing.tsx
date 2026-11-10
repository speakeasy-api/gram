import { useMemo } from "react";
import { useSearchParams } from "react-router";
import { useGatewaySessionsInfinite } from "@gram/client/react-query/gatewaySessions.js";
import { useGatewaySessionFacets } from "@gram/client/react-query/gatewaySessionFacets.js";
import {
  Sort,
  Status,
} from "@gram/client/models/operations/listgatewaysessions.js";
import type { UserSession } from "@gram/client/models/components/usersession.js";

import {
  defineFilters,
  useFilterState,
  type FilterValue,
} from "@/components/filters";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useSessionDateRange } from "@/hooks/useSessionDateRange";

const GATEWAY_SESSION_FILTERS = defineFilters([
  {
    id: "server",
    label: "Server association",
    kind: "select",
    pinned: true,
    description:
      "An upstream account configured for this member server. This does not indicate historical usage.",
  },
  { id: "provider", label: "Provider", kind: "select" },
  {
    id: "status",
    label: "Status",
    kind: "select",
    pinned: true,
    allLabel: "Active",
  },
  { id: "date", label: "Created", kind: "daterange" },
]);

const STATUS_OPTIONS = [
  { value: "all", label: "All statuses" },
  { value: "active", label: "Active" },
  { value: "expired", label: "Expired" },
  { value: "revoked", label: "Revoked" },
];

const STATUS_TILE_TITLES: Record<Status, string> = {
  [Status.Active]: "Active sessions",
  [Status.Expired]: "Expired sessions",
  [Status.Revoked]: "Revoked sessions",
  [Status.All]: "Sessions",
};

/**
 * Sessions authorized through a gateway, read from the gateway listing rather
 * than the issuer-scoped one the MCP server tab drains. Its filters run on the
 * server ahead of cursor pagination, so pages load on request instead.
 *
 * Without a gateway id the listing is organization-wide and independent of the
 * working project.
 */
export function useGatewaySessionListing(
  gatewayId: string | undefined,
  enabled: boolean,
): {
  sessions: UserSession[];
  query: ReturnType<typeof useGatewaySessionsInfinite>;
  isFiltered: boolean;
  sessionsTileTitle: string;
  toolbar: JSX.Element;
} {
  const [params, setParams] = useSearchParams();
  const { values, setValue, clearValue, clearAll } = useFilterState(
    GATEWAY_SESSION_FILTERS,
  );
  const search = params.get("q") ?? "";
  const status =
    Object.values(Status).find((value) => value === values.status) ??
    Status.Active;
  const { dates, refreshDates } = useSessionDateRange(values.date);
  const query = useGatewaySessionsInfinite(
    {
      gatewayId,
      search: search || undefined,
      status,
      // The list orders itself by recency, so the most recently used sessions
      // are the ones worth loading first.
      sort: Sort.LastUsed,
      serverId: values.server ?? undefined,
      providerId: values.provider ?? undefined,
      createdAfter: dates.from,
      createdBefore: dates.to,
      limit: 50,
    },
    undefined,
    { enabled },
  );
  const facets = useGatewaySessionFacets({ gatewayId }, undefined, {
    enabled,
  });
  const sessions = useMemo(
    () => query.data?.pages.flatMap((page) => page.result.items) ?? [],
    [query.data],
  );
  const isFiltered = Boolean(
    search ||
    values.server ||
    values.provider ||
    (values.status && values.status !== Status.Active) ||
    values.date.preset ||
    values.date.customRange,
  );

  const toolbar = (
    <div className="space-y-3">
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={search}
          onChange={(value) =>
            setParams(
              (previous) => {
                const next = new URLSearchParams(previous);
                if (value) next.set("q", value);
                else next.delete("q");
                return next;
              },
              { replace: true },
            )
          }
          debounceMs={300}
          placeholder="Search identities…"
        />
        <Page.Toolbar.Filters
          schema={GATEWAY_SESSION_FILTERS}
          values={values}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
          optionsById={{
            server: facets.data?.servers ?? [],
            provider: facets.data?.providers ?? [],
            status: STATUS_OPTIONS,
          }}
        />
        <Page.Toolbar.Refresh
          onRefresh={() => {
            if (!refreshDates()) void query.refetch();
          }}
          isRefreshing={query.isFetching}
        />
      </Page.Toolbar>
      {facets.isError && (
        <div className="flex items-center gap-3">
          <Text small>Couldn’t load filter choices.</Text>
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => void facets.refetch()}
          >
            Retry filters
          </Button>
        </div>
      )}
    </div>
  );

  return {
    sessions,
    query,
    isFiltered,
    sessionsTileTitle: STATUS_TILE_TITLES[status],
    toolbar,
  };
}
