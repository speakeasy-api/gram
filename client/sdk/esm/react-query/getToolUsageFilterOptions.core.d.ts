import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetToolUsageFilterOptionsResult } from "../models/components/gettoolusagefilteroptionsresult.js";
import {
  GetToolUsageFilterOptionsRequest,
  GetToolUsageFilterOptionsSecurity,
} from "../models/operations/gettoolusagefilteroptions.js";
export type GetToolUsageFilterOptionsQueryData =
  GetToolUsageFilterOptionsResult;
export declare function prefetchGetToolUsageFilterOptions(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetToolUsageFilterOptionsRequest,
  security?: GetToolUsageFilterOptionsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetToolUsageFilterOptionsQuery(
  client$: GramCore,
  request: GetToolUsageFilterOptionsRequest,
  security?: GetToolUsageFilterOptionsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GetToolUsageFilterOptionsQueryData>;
};
export declare function queryKeyGetToolUsageFilterOptions(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getToolUsageFilterOptions.core.d.ts.map
