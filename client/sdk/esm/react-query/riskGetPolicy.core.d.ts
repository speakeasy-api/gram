import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type RiskGetPolicyQueryData = components.RiskPolicy;
export declare function prefetchRiskGetPolicy(queryClient: QueryClient, client$: GramCore, request: operations.GetRiskPolicyRequest, security?: operations.GetRiskPolicySecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskGetPolicyQuery(client$: GramCore, request: operations.GetRiskPolicyRequest, security?: operations.GetRiskPolicySecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskGetPolicyQueryData>;
};
export declare function queryKeyRiskGetPolicy(parameters: {
    id: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskGetPolicy.core.d.ts.map