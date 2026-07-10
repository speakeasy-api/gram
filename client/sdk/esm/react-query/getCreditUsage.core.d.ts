import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreditUsageResponseBody } from "../models/components/creditusageresponsebody.js";
import {
  CreditUsageRequest,
  CreditUsageSecurity,
} from "../models/operations/creditusage.js";
export type GetCreditUsageQueryData = CreditUsageResponseBody;
export declare function prefetchGetCreditUsage(
  queryClient: QueryClient,
  client$: GramCore,
  request?: CreditUsageRequest | undefined,
  security?: CreditUsageSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetCreditUsageQuery(
  client$: GramCore,
  request?: CreditUsageRequest | undefined,
  security?: CreditUsageSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<GetCreditUsageQueryData>;
};
export declare function queryKeyGetCreditUsage(parameters: {
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getCreditUsage.core.d.ts.map
