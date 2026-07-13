import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetObservabilityOverviewResult } from "../models/components/getobservabilityoverviewresult.js";
import {
  GetObservabilityOverviewRequest,
  GetObservabilityOverviewSecurity,
} from "../models/operations/getobservabilityoverview.js";
export type GetObservabilityOverviewQueryData = GetObservabilityOverviewResult;
export declare function prefetchGetObservabilityOverview(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetObservabilityOverviewRequest,
  security?: GetObservabilityOverviewSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetObservabilityOverviewQuery(
  client$: GramCore,
  request: GetObservabilityOverviewRequest,
  security?: GetObservabilityOverviewSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GetObservabilityOverviewQueryData>;
};
export declare function queryKeyGetObservabilityOverview(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getObservabilityOverview.core.d.ts.map
