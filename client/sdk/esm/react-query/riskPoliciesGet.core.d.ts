import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicy } from "../models/components/riskpolicy.js";
import { GetRiskPolicyRequest, GetRiskPolicySecurity } from "../models/operations/getriskpolicy.js";
export type RiskPoliciesGetQueryData = RiskPolicy;
export declare function prefetchRiskPoliciesGet(queryClient: QueryClient, client$: GramCore, request: GetRiskPolicyRequest, security?: GetRiskPolicySecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskPoliciesGetQuery(client$: GramCore, request: GetRiskPolicyRequest, security?: GetRiskPolicySecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskPoliciesGetQueryData>;
};
export declare function queryKeyRiskPoliciesGet(parameters: {
    id: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskPoliciesGet.core.d.ts.map