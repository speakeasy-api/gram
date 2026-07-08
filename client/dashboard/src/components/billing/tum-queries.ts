import { type GramCore } from "@gram/client/core.js";
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import { telemetryQueryRiskTokens } from "@gram/client/funcs/telemetryQueryRiskTokens";
import { telemetryQueryTumDetails } from "@gram/client/funcs/telemetryQueryTumDetails";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { type GroupBy } from "@gram/client/models/components/querypayload.js";
import { type QueryResult } from "@gram/client/models/components/queryresult.js";
import { type QueryRiskTokensResult } from "@gram/client/models/components/queryrisktokensresult.js";
import { type TumDetailsResult } from "@gram/client/models/components/tumdetailsresult.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { type BillingCycle, cycleStaleTime } from "./billing-cycles";

// Shared query definitions for the TUM billing section, each scoped to one
// billing cycle and an optional project. Kept in a non-component module so
// the consuming component files satisfy the react-refresh "only export
// components" rule. The generated SDK hooks key their cache on gramSession
// only, so these definitions drive useQuery directly with payload-encoding
// keys; closed cycles cache forever (their telemetry is immutable) via
// cycleStaleTime.

export type CycleScope = {
  client: GramCore;
  cycle: BillingCycle;
  // Optional project scope; null spans the whole organization.
  projectId: string | null;
};

export type CycleQuery<T> = {
  queryKey: string[];
  staleTime: number;
  throwOnError: boolean;
  queryFn: () => Promise<T>;
};

function cycleQuery<T>(
  name: string,
  { cycle, projectId }: CycleScope,
  extraKeys: string[],
  queryFn: () => Promise<T>,
): CycleQuery<T> {
  return {
    queryKey: [
      name,
      cycle.start.toISOString(),
      cycle.end.toISOString(),
      ...extraKeys,
      projectId ?? "all",
    ],
    staleTime: cycleStaleTime(cycle),
    throwOnError: false,
    queryFn,
  };
}

// The session-level risky-token series for a cycle. The chart's risk stacking
// and the details table's risk rows both consume this with the same key, so
// React Query dedupes them into one request.
export function riskPointsQuery(
  scope: CycleScope,
): CycleQuery<QueryRiskTokensResult> {
  return cycleQuery("tum-risk-tokens", scope, [], () =>
    unwrapAsync(
      telemetryQueryRiskTokens(scope.client, {
        queryRiskTokensPayload: {
          from: scope.cycle.start,
          to: scope.cycle.end,
          projectId: scope.projectId ?? undefined,
        },
      }),
    ),
  );
}

// Every measure of the usage details table in one request.
export function tumDetailsQuery(
  scope: CycleScope,
): CycleQuery<TumDetailsResult> {
  return cycleQuery("tum-details", scope, [], () =>
    unwrapAsync(
      telemetryQueryTumDetails(scope.client, {
        // The generator dedupes structurally identical payload schemas, so
        // this request reuses the risk-tokens payload shape/name.
        queryRiskTokensPayload: {
          from: scope.cycle.start,
          to: scope.cycle.end,
          projectId: scope.projectId ?? undefined,
        },
      }),
    ),
  );
}

// The chart's per-group daily timeseries for one breakdown dimension.
export function tumBreakdownQuery(
  scope: CycleScope,
  dimension: Dimension,
): CycleQuery<QueryResult> {
  return cycleQuery("tum-breakdown", scope, [dimension], () =>
    unwrapAsync(
      telemetryQuery(scope.client, {
        queryPayload: {
          from: scope.cycle.start,
          to: scope.cycle.end,
          groupBy: dimension as GroupBy,
          sortBy: "total_tokens",
          topN: 100,
          // Daily buckets; the panel rolls up to weekly/monthly client-side.
          granularitySeconds: 86400,
          filters: scope.projectId
            ? [{ dimension: Dimension.ProjectId, values: [scope.projectId] }]
            : undefined,
        },
      }),
    ),
  );
}
