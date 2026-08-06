import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type RiskGetPolicyStatusQueryData = components.RiskPolicyStatus;
export declare function prefetchRiskGetPolicyStatus(queryClient: QueryClient, client$: GramCore, request: operations.GetRiskPolicyStatusRequest, security?: operations.GetRiskPolicyStatusSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskGetPolicyStatusQuery(client$: GramCore, request: operations.GetRiskPolicyStatusRequest, security?: operations.GetRiskPolicyStatusSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskGetPolicyStatusQueryData>;
};
export declare function queryKeyRiskGetPolicyStatus(parameters: {
    id: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskGetPolicyStatus.core.d.ts.map