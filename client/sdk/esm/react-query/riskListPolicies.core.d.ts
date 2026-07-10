import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskPoliciesResult } from "../models/components/listriskpoliciesresult.js";
import { ListRiskPoliciesRequest, ListRiskPoliciesSecurity } from "../models/operations/listriskpolicies.js";
export type RiskListPoliciesQueryData = ListRiskPoliciesResult;
export declare function prefetchRiskListPolicies(queryClient: QueryClient, client$: GramCore, request?: ListRiskPoliciesRequest | undefined, security?: ListRiskPoliciesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskListPoliciesQuery(client$: GramCore, request?: ListRiskPoliciesRequest | undefined, security?: ListRiskPoliciesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskListPoliciesQueryData>;
};
export declare function queryKeyRiskListPolicies(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskListPolicies.core.d.ts.map