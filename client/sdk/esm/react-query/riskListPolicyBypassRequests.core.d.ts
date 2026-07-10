import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskPolicyBypassRequestsResult } from "../models/components/listriskpolicybypassrequestsresult.js";
import { ListRiskPolicyBypassRequestsRequest, ListRiskPolicyBypassRequestsSecurity, QueryParamStatus } from "../models/operations/listriskpolicybypassrequests.js";
export type RiskListPolicyBypassRequestsQueryData = ListRiskPolicyBypassRequestsResult;
export declare function prefetchRiskListPolicyBypassRequests(queryClient: QueryClient, client$: GramCore, request?: ListRiskPolicyBypassRequestsRequest | undefined, security?: ListRiskPolicyBypassRequestsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskListPolicyBypassRequestsQuery(client$: GramCore, request?: ListRiskPolicyBypassRequestsRequest | undefined, security?: ListRiskPolicyBypassRequestsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskListPolicyBypassRequestsQueryData>;
};
export declare function queryKeyRiskListPolicyBypassRequests(parameters: {
    policyId?: string | undefined;
    status?: QueryParamStatus | undefined;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskListPolicyBypassRequests.core.d.ts.map