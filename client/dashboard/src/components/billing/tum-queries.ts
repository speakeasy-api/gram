import { type GramCore } from "@gram/client/core.js";
import { telemetryQueryRiskTokens } from "@gram/client/funcs/telemetryQueryRiskTokens";
import { telemetryQueryTumDetails } from "@gram/client/funcs/telemetryQueryTumDetails";
import { type QueryRiskTokensResult } from "@gram/client/models/components/queryrisktokensresult.js";
import { type TumDetailsResult } from "@gram/client/models/components/tumdetailsresult.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { type BillingPeriod, periodStaleTime } from "./billing-cycles";

// Shared query definitions for the TUM billing section, each scoped to one
// period (a billing cycle or a custom range) and an optional project. Kept in
// a non-component module so the consuming component files satisfy the
// react-refresh "only export components" rule. The generated SDK hooks key
// their cache on gramSession only, so these definitions drive useQuery
// directly with payload-encoding keys; closed periods cache forever (their
// telemetry is immutable) via periodStaleTime.
//
// Both requests land on billing-page-specific endpoints that scope their
// reads server-side to the billed completion population (see
// billing.ModelUsageSources on the server) — the client carries no knowledge
// of what is billed.

export type PeriodScope = {
  client: GramCore;
  // The active organization — part of every cache key. The requests are
  // org-scoped server-side, and closed periods cache forever, so without it
  // an org switch could serve another org's cached data for the same dates.
  orgId: string;
  period: BillingPeriod;
  // Optional project scope; null spans the whole organization.
  projectId: string | null;
};

export type PeriodQuery<T> = {
  queryKey: string[];
  staleTime: number;
  throwOnError: boolean;
  queryFn: () => Promise<T>;
};

function periodQuery<T>(
  name: string,
  { orgId, period, projectId }: PeriodScope,
  extraKeys: string[],
  queryFn: () => Promise<T>,
): PeriodQuery<T> {
  return {
    queryKey: [
      name,
      orgId,
      period.start.toISOString(),
      period.end.toISOString(),
      ...extraKeys,
      projectId ?? "all",
    ],
    staleTime: periodStaleTime(period),
    throwOnError: false,
    queryFn,
  };
}

// The session-level risky-token series for a period. The chart's risk
// stacking and the details table's risk rows both consume this with the same
// key, so React Query dedupes them into one request.
export function riskPointsQuery(
  scope: PeriodScope,
): PeriodQuery<QueryRiskTokensResult> {
  return periodQuery("tum-risk-tokens", scope, [], () =>
    unwrapAsync(
      telemetryQueryRiskTokens(scope.client, {
        telemetryWindowPayload: {
          from: scope.period.start,
          to: scope.period.end,
          projectId: scope.projectId ?? undefined,
        },
      }),
    ),
  );
}

// Every measure of the usage details table in one request.
export function tumDetailsQuery(
  scope: PeriodScope,
): PeriodQuery<TumDetailsResult> {
  return periodQuery("tum-details", scope, [], () =>
    unwrapAsync(
      telemetryQueryTumDetails(scope.client, {
        telemetryWindowPayload: {
          from: scope.period.start,
          to: scope.period.end,
          projectId: scope.projectId ?? undefined,
        },
      }),
    ),
  );
}
