import { type GramCore } from "@gram/client/core.js";
import { telemetryQueryTumDetails } from "@gram/client/funcs/telemetryQueryTumDetails";
import { type TumDetailsResult } from "@gram/client/models/components/tumdetailsresult.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { type BillingPeriod, periodStaleTime } from "./billing-cycles";

// Shared query definitions for the TUM billing section, each scoped to one
// period (a billing cycle or a custom range), always org-wide — per-project
// visibility comes from the Project breakdown, not a request filter. Kept in
// a non-component module so the consuming component files satisfy the
// react-refresh "only export components" rule. The generated SDK hooks key
// their cache on gramSession only, so these definitions drive useQuery
// directly with payload-encoding keys; closed periods cache forever (their
// telemetry is immutable) via periodStaleTime.
//
// The request lands on a billing-page-specific endpoint that scopes its
// reads server-side to the tokens-under-management population (observed
// agent traffic; see the server's QueryTumDetails) — the client carries no
// knowledge of what is billed.

export type PeriodScope = {
  client: GramCore;
  // The active organization — part of every cache key. The requests are
  // org-scoped server-side, and closed periods cache forever, so without it
  // an org switch could serve another org's cached data for the same dates.
  orgId: string;
  period: BillingPeriod;
};

export type PeriodQuery<T> = {
  queryKey: string[];
  staleTime: number;
  throwOnError: boolean;
  queryFn: () => Promise<T>;
};

function periodQuery<T>(
  name: string,
  { orgId, period }: PeriodScope,
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
    ],
    staleTime: periodStaleTime(period),
    throwOnError: false,
    queryFn,
  };
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
        },
      }),
    ),
  );
}
