import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListCustomDetectionRulesResult } from "../models/components/listcustomdetectionrulesresult.js";
import { ListCustomDetectionRulesRequest, ListCustomDetectionRulesSecurity } from "../models/operations/listcustomdetectionrules.js";
export type RiskListCustomDetectionRulesQueryData = ListCustomDetectionRulesResult;
export declare function prefetchRiskListCustomDetectionRules(queryClient: QueryClient, client$: GramCore, request?: ListCustomDetectionRulesRequest | undefined, security?: ListCustomDetectionRulesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskListCustomDetectionRulesQuery(client$: GramCore, request?: ListCustomDetectionRulesRequest | undefined, security?: ListCustomDetectionRulesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskListCustomDetectionRulesQueryData>;
};
export declare function queryKeyRiskListCustomDetectionRules(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskListCustomDetectionRules.core.d.ts.map