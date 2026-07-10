import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskCustomDetectionRule } from "../models/components/riskcustomdetectionrule.js";
import { GetCustomDetectionRuleRequest, GetCustomDetectionRuleSecurity } from "../models/operations/getcustomdetectionrule.js";
export type RiskGetCustomDetectionRuleQueryData = RiskCustomDetectionRule;
export declare function prefetchRiskGetCustomDetectionRule(queryClient: QueryClient, client$: GramCore, request: GetCustomDetectionRuleRequest, security?: GetCustomDetectionRuleSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskGetCustomDetectionRuleQuery(client$: GramCore, request: GetCustomDetectionRuleRequest, security?: GetCustomDetectionRuleSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskGetCustomDetectionRuleQueryData>;
};
export declare function queryKeyRiskGetCustomDetectionRule(parameters: {
    id: string;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskGetCustomDetectionRule.core.d.ts.map