import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetToolUsageSummaryResult } from "../models/components/gettoolusagesummaryresult.js";
import { GetToolUsageSummaryRequest, GetToolUsageSummarySecurity } from "../models/operations/gettoolusagesummary.js";
export type GetToolUsageSummaryQueryData = GetToolUsageSummaryResult;
export declare function prefetchGetToolUsageSummary(queryClient: QueryClient, client$: GramCore, request: GetToolUsageSummaryRequest, security?: GetToolUsageSummarySecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildGetToolUsageSummaryQuery(client$: GramCore, request: GetToolUsageSummaryRequest, security?: GetToolUsageSummarySecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<GetToolUsageSummaryQueryData>;
};
export declare function queryKeyGetToolUsageSummary(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getToolUsageSummary.core.d.ts.map