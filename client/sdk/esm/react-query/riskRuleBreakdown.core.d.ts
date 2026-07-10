import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskRuleBreakdownResult } from "../models/components/riskrulebreakdownresult.js";
import { GetRiskRuleBreakdownRequest, GetRiskRuleBreakdownSecurity } from "../models/operations/getriskrulebreakdown.js";
export type RiskRuleBreakdownQueryData = RiskRuleBreakdownResult;
export declare function prefetchRiskRuleBreakdown(queryClient: QueryClient, client$: GramCore, request: GetRiskRuleBreakdownRequest, security?: GetRiskRuleBreakdownSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildRiskRuleBreakdownQuery(client$: GramCore, request: GetRiskRuleBreakdownRequest, security?: GetRiskRuleBreakdownSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<RiskRuleBreakdownQueryData>;
};
export declare function queryKeyRiskRuleBreakdown(parameters: {
    category: string;
    from?: Date | undefined;
    to?: Date | undefined;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=riskRuleBreakdown.core.d.ts.map