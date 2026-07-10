import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskResultsResult } from "../models/components/listriskresultsresult.js";
import { ListRiskResultsRequest, ListRiskResultsSecurity } from "../models/operations/listriskresults.js";
export type RiskListResultsQueryData = ListRiskResultsResult;
export declare function prefetchRiskListResults(queryClient: QueryClient, client$: GramCore, request?: ListRiskResultsRequest | undefined, security?: ListRiskResultsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskListResultsQuery(client$: GramCore, request?: ListRiskResultsRequest | undefined, security?: ListRiskResultsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskListResultsQueryData>;
};
export declare function queryKeyRiskListResults(parameters: {
    policyId?: string | undefined;
    chatId?: string | undefined;
    category?: string | undefined;
    ruleId?: string | undefined;
    userId?: string | undefined;
    uniqueMatch?: boolean | undefined;
    from?: Date | undefined;
    to?: Date | undefined;
    cursor?: string | undefined;
    limit?: number | undefined;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskListResults.core.d.ts.map