import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetUserMetricsSummaryResult } from "../models/components/getusermetricssummaryresult.js";
import {
  GetUserMetricsSummaryRequest,
  GetUserMetricsSummarySecurity,
} from "../models/operations/getusermetricssummary.js";
export type GetUserMetricsSummaryQueryData = GetUserMetricsSummaryResult;
export declare function prefetchGetUserMetricsSummary(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetUserMetricsSummaryRequest,
  security?: GetUserMetricsSummarySecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetUserMetricsSummaryQuery(
  client$: GramCore,
  request: GetUserMetricsSummaryRequest,
  security?: GetUserMetricsSummarySecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GetUserMetricsSummaryQueryData>;
};
export declare function queryKeyGetUserMetricsSummary(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getUserMetricsSummary.core.d.ts.map
