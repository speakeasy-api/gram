import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PeriodUsage } from "../models/components/periodusage.js";
import {
  GetPeriodUsageRequest,
  GetPeriodUsageSecurity,
} from "../models/operations/getperiodusage.js";
export type GetPeriodUsageQueryData = PeriodUsage;
export declare function prefetchGetPeriodUsage(
  queryClient: QueryClient,
  client$: GramCore,
  request?: GetPeriodUsageRequest | undefined,
  security?: GetPeriodUsageSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetPeriodUsageQuery(
  client$: GramCore,
  request?: GetPeriodUsageRequest | undefined,
  security?: GetPeriodUsageSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<GetPeriodUsageQueryData>;
};
export declare function queryKeyGetPeriodUsage(parameters: {
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getPeriodUsage.core.d.ts.map
