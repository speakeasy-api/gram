import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetHooksSummaryResult } from "../models/components/gethookssummaryresult.js";
import {
  GetHooksSummaryRequest,
  GetHooksSummarySecurity,
} from "../models/operations/gethookssummary.js";
export type GetHooksSummaryQueryData = GetHooksSummaryResult;
export declare function prefetchGetHooksSummary(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetHooksSummaryRequest,
  security?: GetHooksSummarySecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetHooksSummaryQuery(
  client$: GramCore,
  request: GetHooksSummaryRequest,
  security?: GetHooksSummarySecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<GetHooksSummaryQueryData>;
};
export declare function queryKeyGetHooksSummary(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getHooksSummary.core.d.ts.map
