import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetMetricsSummaryResult } from "../models/components/getmetricssummaryresult.js";
import { GetProjectMetricsSummaryRequest, GetProjectMetricsSummarySecurity } from "../models/operations/getprojectmetricssummary.js";
export type GetProjectMetricsSummaryQueryData = GetMetricsSummaryResult;
export declare function prefetchGetProjectMetricsSummary(queryClient: QueryClient, client$: GramCore, request: GetProjectMetricsSummaryRequest, security?: GetProjectMetricsSummarySecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildGetProjectMetricsSummaryQuery(client$: GramCore, request: GetProjectMetricsSummaryRequest, security?: GetProjectMetricsSummarySecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<GetProjectMetricsSummaryQueryData>;
};
export declare function queryKeyGetProjectMetricsSummary(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getProjectMetricsSummary.core.d.ts.map