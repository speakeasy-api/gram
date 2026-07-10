import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskExclusionsResult } from "../models/components/listriskexclusionsresult.js";
import { ListRiskExclusionsRequest, ListRiskExclusionsSecurity } from "../models/operations/listriskexclusions.js";
export type RiskListExclusionsQueryData = ListRiskExclusionsResult;
export declare function prefetchRiskListExclusions(queryClient: QueryClient, client$: GramCore, request?: ListRiskExclusionsRequest | undefined, security?: ListRiskExclusionsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskListExclusionsQuery(client$: GramCore, request?: ListRiskExclusionsRequest | undefined, security?: ListRiskExclusionsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskListExclusionsQueryData>;
};
export declare function queryKeyRiskListExclusions(parameters: {
    riskPolicyId?: string | undefined;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskListExclusions.core.d.ts.map