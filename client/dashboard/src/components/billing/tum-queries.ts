import { telemetryQueryRiskTokens } from "@gram/client/funcs/telemetryQueryRiskTokens";
import { type GramCore } from "@gram/client/core.js";
import { type QueryRiskTokensResult } from "@gram/client/models/components/queryrisktokensresult.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { type BillingCycle, cycleStaleTime } from "./billing-cycles";

// Shared query definitions for the TUM billing section. Kept in a
// non-component module so the consuming component files satisfy the
// react-refresh "only export components" rule.

export type RiskPointsQueryInput = {
  client: GramCore;
  cycle: BillingCycle;
  projectId: string | null;
};

// The session-level risky-token series for a cycle. The chart's risk stacking
// and the details table's risk rows both consume this with the same key, so
// React Query dedupes them into one request.
export function riskPointsQuery({
  client,
  cycle,
  projectId,
}: RiskPointsQueryInput): {
  queryKey: string[];
  staleTime: number;
  throwOnError: boolean;
  queryFn: () => Promise<QueryRiskTokensResult>;
} {
  const from = cycle.start;
  const to = cycle.end;
  return {
    queryKey: [
      "tum-risk-tokens",
      from.toISOString(),
      to.toISOString(),
      projectId ?? "all",
    ],
    staleTime: cycleStaleTime(cycle),
    throwOnError: false,
    queryFn: () =>
      unwrapAsync(
        telemetryQueryRiskTokens(client, {
          queryRiskTokensPayload: {
            from,
            to,
            projectId: projectId ?? undefined,
          },
        }),
      ),
  };
}
